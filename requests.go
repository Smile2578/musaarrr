package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

// ErrBusy: a source is already on its way to Plex, so retiring the request would not stop it.
var ErrBusy = errors.New("download already started")

type Service struct {
	lidarr   *Lidarr
	plex     *Plex     // nil: Plex disabled
	meta     *Metadata // the artists' other names; nil: Lidarr's name only
	store    *Store
	prof     Profiles
	root     string
	mu       sync.Mutex     // one Create/Retry/Refresh at a time: no double add, no lost update
	searches sync.WaitGroup // searches running in the background

	namesMu sync.Mutex
	names   map[string][]string // artist MBID -> its MusicBrainz name and aliases
}

// Create requests one album; it is idempotent per MBID. created is false when it already existed.
func (s *Service) Create(ctx context.Context, mbid string) (r Request, created bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.store.Get(mbid); ok {
		return r, false, nil
	}
	// Adding a new artist makes Lidarr fetch its metadata, which can be slow.
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	found, err := s.lidarr.Lookup(ctx, "lidarr:"+mbid)
	if err != nil {
		return r, false, err
	}
	if len(found) == 0 {
		return r, false, ErrNotFound
	}
	a := found[0]
	id := a.ID
	newArtist := false
	if id == 0 {
		known, kerr := s.lidarr.ArtistKnown(ctx, a.ArtistMBID)
		if kerr != nil {
			return r, false, kerr
		}
		id, err = s.lidarr.AddAlbum(ctx, a, s.prof.QualityID, s.prof.MetadataID, s.root)
		newArtist = !known && err == nil
		if errors.Is(err, ErrAlreadyAdded) {
			err = s.monitorExisting(ctx, mbid, &id)
		}
	} else {
		err = s.lidarr.MonitorAlbum(ctx, id)
	}
	if err != nil {
		return r, false, err
	}
	r = Request{MBID: mbid, Title: a.Title, Artist: a.ArtistName, Type: a.Type, Secondary: a.Secondary,
		Cover: a.Cover, Year: a.Year, AlbumID: id, RequestedAt: time.Now(), Status: "requested", ArtistMBID: a.ArtistMBID}
	if got, err := s.lidarr.AlbumByMBID(ctx, mbid); err == nil && got != nil {
		// Lidarr 3.1.6 creates the artist unmonitored whatever the payload says (seen on a real
		// instance): the album would then miss Wanted, RSS and 24-bit upgrades. Only artists we
		// just created are fixed; monitorNewItems "none" keeps their other albums unmonitored.
		if newArtist && got.ArtistID != 0 && !got.ArtistMonitored {
			if err := s.lidarr.MonitorArtist(ctx, got.ArtistID); err != nil {
				log.Printf("monitoring artist %s: %v", got.ArtistName, err)
			} else {
				got.ArtistMonitored = true
			}
		}
		r.AlbumID = got.ID
		r.Note = s.artistNote(got)
	}
	return r, true, s.store.Put(r)
}

func (s *Service) monitorExisting(ctx context.Context, mbid string, id *int) error {
	got, err := s.lidarr.AlbumByMBID(ctx, mbid)
	if err != nil {
		return err
	}
	if got == nil {
		return ErrNotFound
	}
	*id = got.ID
	return s.lidarr.MonitorAlbum(ctx, got.ID)
}

// A request's note is a code the page words in the reader's language (key "note."+code), or
// "profile:<name>" for the artist's quality profile. noteView reads it.
const (
	unmonitoredNote = "unmonitored" // Lidarr's RSS only grabs for artists it follows: a later source is missed
	profileNote     = "profile"
	notInPlexNote   = "notInPlex"
	removedNote     = "removed"
	notReadyNote    = "notReady"
)

// artistNote warns when an existing artist will not get the album with the chosen profile; the app never edits artists.
func (s *Service) artistNote(a *Album) string {
	switch {
	case a.ArtistID == 0: // artist not embedded in the answer: nothing to say
		return ""
	case a.ArtistQualityProfileID != 0 && a.ArtistQualityProfileID != s.prof.QualityID:
		return profileNote + ":" + s.prof.QualityNames[a.ArtistQualityProfileID]
	case !a.ArtistMonitored:
		return unmonitoredNote
	}
	return ""
}

// Retry searches again for a request, unless its search is still running.
func (s *Service) Retry(ctx context.Context, mbid string) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.store.Get(mbid)
	if !ok {
		return r, ErrNotFound
	}
	if ownSearchRunning(r) {
		return r, nil
	}
	a, err := s.lidarr.AlbumByMBID(ctx, mbid)
	if err != nil {
		return r, err
	}
	if a == nil {
		return r, ErrNotFound
	}
	if !r.PickedAt.IsZero() { // the user gives up on their pick: Lidarr looks for the album again
		if err := s.lidarr.MonitorAlbum(ctx, a.ID); err != nil {
			return r, err
		}
		r.Picked, r.PickedFrom, r.PickedAt = "", "", time.Time{}
	}
	now := time.Now()
	r.AlbumID, r.CommandID = a.ID, 0
	s.startSearch(&r, *a, now)
	r.Status, r.CheckedAt = "searching", now
	return r, s.store.Put(r)
}

// Remove forgets a request and has Lidarr stop looking for the album.
func (s *Service) Remove(ctx context.Context, mbid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.store.Get(mbid)
	if !ok {
		return ErrNotFound
	}
	if onItsWay(r) {
		return ErrBusy
	}
	if r.Status != "in_plex" { // in Plex, Lidarr may still upgrade the album: leave it be
		a, err := s.lidarr.AlbumByMBID(ctx, mbid)
		if err != nil {
			return err
		}
		if a != nil && a.Monitored {
			if r.Status == "searching" { // Lidarr may hold a grab back (delay profile)
				queue, err := s.lidarr.Queue(ctx)
				if err != nil {
					return err
				}
				if slices.ContainsFunc(queue, func(q QueueItem) bool { return q.AlbumID == a.ID }) {
					return ErrBusy
				}
			}
			if err := s.lidarr.UnmonitorAlbum(ctx, a.ID); err != nil {
				return err
			}
		}
	}
	return s.store.Delete(mbid)
}

func onItsWay(r Request) bool {
	switch r.Status {
	case "downloading", "importing", "imported", "picked":
		return true
	}
	return r.Status == "searching" && r.Source != "" // grabbed, not in the queue yet
}

// searchTimeout bounds one interactive search: Lidarr asks every indexer and waits for all of them.
const searchTimeout = 3 * time.Minute

func ownSearchRunning(r Request) bool {
	return r.CommandID == 0 && r.Cmd != nil && r.Cmd.Status == "started"
}

// startSearch runs Musaarr's search for r in the background, outside s.mu: it takes seconds and
// must not hold up new requests. Its outcome is stored in r.Cmd, shaped like Lidarr's AlbumSearch.
func (s *Service) startSearch(r *Request, a Album, now time.Time) {
	r.SearchedAt, r.Cmd, r.Source, r.DownloadID, r.ImportCmd = now, &Command{Status: "started"}, "", "", 0
	mbid, names, allowed := r.MBID, []string{a.ArtistName, r.Artist}, s.prof.Allowed
	s.searches.Add(1)
	go func() {
		defer s.searches.Done()
		ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
		defer cancel()
		c, source := s.search(ctx, a, s.artistNames(ctx, a.ArtistMBID, names...), allowed)
		s.mu.Lock()
		defer s.mu.Unlock()
		cur, ok := s.store.Get(mbid)
		if !ok || !cur.SearchedAt.Equal(now) { // searched again meanwhile
			return
		}
		cur.Cmd, cur.Source = &c, source
		if err := s.store.Put(cur); err != nil {
			log.Printf("request %s: %v", mbid, err)
		}
	}()
}

// search asks Lidarr for every source of the album and grabs the one pick chooses.
func (s *Service) search(ctx context.Context, a Album, names []string, allowed map[int]bool) (Command, string) {
	rels, err := s.lidarr.Releases(ctx, a.ID)
	if err == nil {
		rel, forced, ok := pick(rels, names, a.Title, allowed)
		if !ok {
			return Command{Status: "completed", Message: fmt.Sprintf("%d sources, 0 reports downloaded.", len(rels)), Ended: time.Now()}, ""
		}
		// shortcut: a Remove between this check and the grab still lets it through (milliseconds).
		if _, kept := s.store.Get(a.MBID); !kept {
			return Command{Status: "cancelled", Ended: time.Now()}, ""
		}
		if err = s.lidarr.Grab(ctx, rel, a.ID, a.ArtistID); err == nil {
			if forced {
				log.Printf("%s: a source Lidarr could not read, picked by Musaarr: %s", a.Title, rel.Title)
			}
			return Command{Status: "completed", Message: "1 reports downloaded.", Ended: time.Now()}, rel.Title
		}
	}
	log.Printf("search for %s: %v", a.Title, err)
	return Command{Status: "failed", Message: err.Error(), Ended: time.Now()}, ""
}

// followUnreadable finds the download of a source Lidarr could not read, and imports it into
// the album when Lidarr stops at "Unable to parse download". It returns that download's queue items.
func (s *Service) followUnreadable(ctx context.Context, r *Request, a *Album, queue []QueueItem) []QueueItem {
	if r.DownloadID == "" {
		id, err := s.lidarr.GrabbedDownloadID(ctx, a.ID)
		if err != nil {
			log.Printf("history of %s: %v", a.Title, err)
		}
		r.DownloadID = id
	}
	var mine []QueueItem
	for _, q := range queue {
		if r.DownloadID != "" && q.AlbumID == 0 && strings.EqualFold(q.DownloadID, r.DownloadID) {
			mine = append(mine, q)
		}
	}
	if len(mine) > 0 && mine[0].State == "importBlocked" && r.ImportCmd == 0 {
		r.ImportCmd = s.manualImport(ctx, a, r.DownloadID)
	}
	return mine
}

// manualImport imports a download only when every audio file maps to a distinct track of the album.
func (s *Service) manualImport(ctx context.Context, a *Album, downloadID string) int {
	items, err := s.lidarr.ImportItems(ctx, downloadID, a.ArtistID)
	if err != nil {
		log.Printf("manual import of %s: %v", a.Title, err)
		return 0 // tried again next pass
	}
	var files []ImportItem
	seen := map[int]bool{}
	for _, it := range items {
		if it.AdditionalFile {
			continue
		}
		ok := it.Album.ID == a.ID && len(it.Tracks) > 0 && len(it.Rejections) == 0
		for _, t := range it.Tracks {
			ok = ok && !seen[t.ID]
			seen[t.ID] = true
		}
		if !ok {
			log.Printf("manual import of %s left to the user: %s does not match the album", a.Title, path.Base(it.Path))
			return -1
		}
		files = append(files, it)
	}
	if len(files) == 0 {
		log.Printf("manual import of %s left to the user: no audio file", a.Title)
		return -1
	}
	id, err := s.lidarr.ManualImport(ctx, files, a.ArtistID, a.ID, downloadID)
	if err != nil {
		log.Printf("manual import of %s: %v", a.Title, err)
		return 0
	}
	log.Printf("%s: manual import of %d files started", a.Title, len(files))
	return id
}

// artistNames adds the artist's MusicBrainz aliases to the names given: trackers and Plex tags
// often still say "Kanye West" for the artist MusicBrainz now calls "Ye".
func (s *Service) artistNames(ctx context.Context, artistMBID string, known ...string) []string {
	var names []string
	add := func(n string) {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	for _, n := range known {
		add(n)
	}
	if s.meta == nil || artistMBID == "" {
		return names
	}
	s.namesMu.Lock()
	aliases, ok := s.names[artistMBID]
	s.namesMu.Unlock()
	if !ok {
		var err error
		if aliases, err = s.meta.Names(ctx, artistMBID); err != nil {
			log.Printf("other names of artist %s: %v", artistMBID, err)
			return names // not cached: asked again next time
		}
		s.namesMu.Lock()
		if s.names == nil {
			s.names = map[string][]string{}
		}
		s.names[artistMBID] = aliases
		s.namesMu.Unlock()
	}
	for _, n := range aliases {
		add(n)
	}
	return names
}

// pickedWait is how long a source the user chose may take to reach Plex before they are told to look.
const pickedWait = 24 * time.Hour

// Picked records the release the user sent from the Sources tab for a request. Its files go where
// Prowlarr's download client puts them, never into Lidarr's library, so Lidarr stops looking for the album.
func (s *Service) Picked(ctx context.Context, mbid, title, indexer string, now time.Time) (Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.store.Get(mbid)
	if !ok {
		return r, ErrNotFound
	}
	r.Picked, r.PickedFrom, r.PickedAt = title, indexer, now
	r.Status, r.Note, r.Progress, r.Timeleft, r.CheckedAt = "picked", "", 0, "", now
	if err := s.lidarr.UnmonitorAlbum(ctx, r.AlbumID); err != nil { // the grab is done: keep the pick, refresh retries
		log.Printf("%s: Lidarr still monitors it: %v", r.Title, err)
	}
	return r, s.store.Put(r)
}

// refreshPicked follows a chosen source to Plex, outside Lidarr's queue and imports.
func (s *Service) refreshPicked(ctx context.Context, r *Request, a *Album, now time.Time) error {
	names := []string{r.Artist}
	if a != nil {
		r.AlbumID = a.ID
		names = append(names, a.ArtistName)
		if a.Monitored {
			if err := s.lidarr.UnmonitorAlbum(ctx, a.ID); err != nil {
				return err
			}
		}
	}
	r.Progress, r.Timeleft = 0, ""
	switch r.PlexKey = s.findInPlex(ctx, r.ArtistMBID, r.Title, names...); {
	case r.PlexKey != "":
		r.Status, r.Note = "in_plex", ""
	case now.Sub(r.PickedAt) >= pickedWait:
		r.Status, r.Note = "check", notInPlexNote
	default:
		r.Status, r.Note = "picked", ""
	}
	return nil
}

// cachedNames is artistNames without a network call: in a list, one call per artist would stall the page.
func (s *Service) cachedNames(artistMBID string, known ...string) []string {
	s.namesMu.Lock()
	defer s.namesMu.Unlock()
	return append(slices.Clone(known), s.names[artistMBID]...)
}

// findInPlex looks for the album under every name of its artist; "" when Plex is off or lacks it.
func (s *Service) findInPlex(ctx context.Context, artistMBID, title string, names ...string) string {
	if s.plex == nil {
		return ""
	}
	for _, n := range s.artistNames(ctx, artistMBID, names...) {
		if k := s.plex.Find(n, title); k != "" {
			return k
		}
	}
	return ""
}

// plexWait is how long an imported album is checked against Plex every minute.
const plexWait = 30 * time.Minute

// notReadyAfter is how long a request may wait for Lidarr to monitor the album and list its releases.
const notReadyAfter = 10 * time.Minute

func due(r Request, now time.Time) bool {
	switch r.Status {
	case "in_plex":
		return false
	case "imported":
		if recentImport(r, now) {
			return now.Sub(r.CheckedAt) >= time.Minute
		}
		return now.Sub(r.CheckedAt) >= 15*time.Minute
	case "waiting", "check", "failed":
		return now.Sub(r.CheckedAt) >= 15*time.Minute
	}
	return true
}

// active statuses are re-read on every pass, whatever CheckedAt says.
func active(status string) bool {
	return status == "requested" || status == "searching" || status == "downloading" || status == "importing" || status == "picked"
}

func recentImport(r Request, now time.Time) bool {
	return r.Status == "imported" && now.Sub(r.ImportedAt) < plexWait
}

func terminal(c *Command) bool {
	return c != nil && (c.Gone || c.Status == "completed" || c.Status == "failed" ||
		c.Status == "aborted" || c.Status == "cancelled" || c.Status == "orphaned")
}

// Refresh re-reads Lidarr for every request that is due and stores its new status.
func (s *Service) Refresh(ctx context.Context, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var todo []Request
	for _, r := range s.store.All() {
		if due(r, now) {
			todo = append(todo, r)
		}
	}
	if len(todo) == 0 {
		return
	}
	queue, err := s.lidarr.Queue(ctx)
	if err != nil {
		log.Printf("Lidarr queue: %v", err)
		return
	}
	for _, r := range todo {
		prev := r
		if err := s.refreshOne(ctx, &r, queue, now); err != nil {
			log.Printf("request %s: %v", r.MBID, err)
			if r.CommandID == prev.CommandID && r.SearchedAt.Equal(prev.SearchedAt) {
				continue
			}
			// a search went out before the error: save its id so it is not sent again
		} else if active(r.Status) && sameButChecked(prev, r) {
			continue // nothing new: spare the disk
		}
		if err := s.store.Put(r); err != nil {
			log.Printf("request %s: %v", r.MBID, err)
		}
	}
}

func sameButChecked(a, b Request) bool {
	a.CheckedAt = b.CheckedAt
	return reflect.DeepEqual(a, b)
}

func (s *Service) refreshOne(ctx context.Context, r *Request, queue []QueueItem, now time.Time) error {
	a, err := s.lidarr.AlbumByMBID(ctx, r.MBID)
	if err != nil {
		return err
	}
	r.CheckedAt = now
	if a != nil && a.ArtistMBID != "" {
		r.ArtistMBID = a.ArtistMBID
	}
	if !r.PickedAt.IsZero() {
		return s.refreshPicked(ctx, r, a, now)
	}
	if a == nil {
		if r.PlexKey = s.findInPlex(ctx, r.ArtistMBID, r.Title, r.Artist); r.PlexKey != "" {
			r.Status, r.Note = "in_plex", ""
			return nil
		}
		r.Status, r.Note = "check", removedNote
		return nil
	}
	r.AlbumID, r.Note = a.ID, ""
	// Any source counts, the user's own downloads too; only Lidarr's half-done import is waited for.
	if r.PlexKey == "" && (a.Percent == 0 || a.Percent >= 100) {
		r.PlexKey = s.findInPlex(ctx, a.ArtistMBID, r.Title, r.Artist, a.ArtistName)
	}
	if r.PlexKey != "" && a.Percent < 100 {
		if a.Monitored { // Lidarr does not have these files: it would fetch a second copy
			if err := s.lidarr.UnmonitorAlbum(ctx, a.ID); err != nil {
				r.PlexKey = "" // found again next pass
				return err
			}
			log.Printf("%s: already in Plex, Lidarr stops searching for it", r.Title)
		}
		r.Status, r.Progress, r.Timeleft = "in_plex", 0, ""
		return nil
	}
	switch {
	case r.CommandID == 0 && r.SearchedAt.IsZero() && a.Monitored && a.Releases > 0:
		s.startSearch(r, *a, now)
	case ownSearchRunning(*r) && now.Sub(r.SearchedAt) > searchTimeout+time.Minute: // Musaarr restarted mid-search
		r.Cmd = &Command{Status: "completed", Message: "Recherche interrompue, 0 reports downloaded.", Ended: now}
	}
	if r.CommandID > 0 && !terminal(r.Cmd) { // searched by Lidarr's AlbumSearch before Musaarr searched itself
		c, err := s.lidarr.Command(ctx, r.CommandID)
		if err != nil {
			return err
		}
		r.Cmd = &c
	}
	var mine []QueueItem
	for _, q := range queue {
		if q.AlbumID == a.ID {
			mine = append(mine, q)
		}
	}
	if len(mine) == 0 && r.Source != "" && a.Percent < 100 {
		mine = s.followUnreadable(ctx, r, a, queue)
	}
	r.Status, r.Progress, r.Timeleft = computeStatus(Signals{Cmd: r.Cmd, Queue: mine, Percent: a.Percent, InPlex: r.PlexKey != "", Now: now})
	if r.Status == "check" && r.ImportCmd > 0 {
		if c, err := s.lidarr.Command(ctx, r.ImportCmd); err == nil && !terminal(&c) {
			r.Status = "importing"
		}
	}
	switch {
	case r.Status == "imported" && r.ImportedAt.IsZero():
		r.ImportedAt = now
	case r.Status == "requested" && now.Sub(r.RequestedAt) >= notReadyAfter:
		r.Status, r.Note = "check", notReadyNote
	}
	if r.Note == "" && (r.Status == "requested" || r.Status == "searching" || r.Status == "waiting") {
		r.Note = s.artistNote(a) // the artist's settings only matter until a source is found
	}
	return nil
}
