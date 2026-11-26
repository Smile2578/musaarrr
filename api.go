package main

import (
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed web
var webFS embed.FS

// Item is the only album shape the browser ever sees.
type Item struct {
	MBID        string         `json:"mbid"`
	Title       string         `json:"title"`
	Artist      string         `json:"artist"`
	ArtistMBID  string         `json:"artistMbid,omitempty"`
	Year        int            `json:"year"`
	Type        string         `json:"type"`
	Secondary   []string       `json:"secondary"`
	Cover       string         `json:"cover"`
	Status      string         `json:"status"`
	Progress    int            `json:"progress"`
	Timeleft    string         `json:"timeleft"`
	PlexURL     string         `json:"plexUrl,omitempty"`
	Note        string         `json:"note,omitempty"`     // in English, for API clients
	NoteCode    string         `json:"noteCode,omitempty"` // the page's key: "note."+code
	NoteParams  map[string]any `json:"noteParams,omitempty"`
	RequestedAt *time.Time     `json:"requestedAt,omitempty"`
	Picked      string         `json:"picked,omitempty"`
	PickedFrom  string         `json:"pickedFrom,omitempty"`
	Names       []string       `json:"names,omitempty"` // the artist's other names, for the Sources tab
	Removable   bool           `json:"removable,omitempty"`
	Listens     int64          `json:"listens,omitempty"` // on ListenBrainz, artist pages only
}

type App struct {
	svc     *Service
	meta    *Metadata
	sources *Sources  // nil: PROWLARR_API_KEY not set
	listens *Listens  // nil: no listen counts
	room    *RoomFile // nil: the room is not kept

	sugMu sync.Mutex
	sug   []suggestion // nil until the first pass is over
	mine  []suggestion // every studio album of the user's artists, from the last pass

	shopMu     sync.Mutex
	shop       *shopStock // nil until the first build
	shopPath   string
	base       []ShopRecord // the starting stock the CI publishes (stockbuild.go), nil without one
	stockPath  string
	stockURL   string    // where it is fetched from: "" to keep the file only
	stockTried time.Time // the last failed fetch: tried again a day later
	nudge      chan struct{}

	obiMu    sync.Mutex
	obi      map[string]string // album MBID -> its Japanese pressing's front, "" for none
	obiPath  string
	coverDir string // the shop's thumbnails, kept: "" without a data folder

	factsMu sync.Mutex
	facts   map[string]coverFact // a cover's art -> its colour and ThumbHash, read as it came in

	mu       sync.Mutex
	notReady string // empty once Lidarr profiles are resolved
	cache    map[string]cached
}

type cached struct {
	at  time.Time
	val any
}

func NewApp(s *Service) *App {
	return &App{svc: s, nudge: make(chan struct{}, 1), notReady: "démarrage", cache: map[string]cached{}, obi: map[string]string{}, facts: map[string]coverFact{}}
}

func (a *App) setReady(why string) {
	a.mu.Lock()
	a.notReady = why
	a.mu.Unlock()
}

func (a *App) ready() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.notReady
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /api/search", a.search)
	mux.HandleFunc("GET /api/artists/{mbid}", a.artist)
	mux.HandleFunc("GET /api/cover/{mbid}", a.cover)
	mux.HandleFunc("GET /api/albums/{mbid}", a.album)
	mux.HandleFunc("GET /api/suggestions", a.suggestions)
	mux.HandleFunc("GET /api/requests", a.list)
	mux.HandleFunc("POST /api/requests", a.create)
	mux.HandleFunc("POST /api/requests/{mbid}/retry", a.retry)
	mux.HandleFunc("DELETE /api/requests/{mbid}", a.remove)
	mux.HandleFunc("GET /api/artists/{mbid}/sources", a.artistSources)
	mux.HandleFunc("GET /api/sources", a.sourceSearch)
	mux.HandleFunc("POST /api/sources/grab", a.sourceGrab)
	mux.HandleFunc("POST /api/sources/files", a.sourceFiles)
	mux.HandleFunc("GET /api/library", a.library)
	mux.HandleFunc("GET /api/library/{key}/cover", a.libraryCover)
	mux.HandleFunc("GET /api/library/{key}/album", a.libraryAlbum)
	mux.HandleFunc("GET /api/room", a.getRoom)
	mux.HandleFunc("PUT /api/room", a.putRoom)
	mux.HandleFunc("GET /api/shop", a.getShop)
	mux.HandleFunc("GET /api/shop/racks", a.getShopRacks)
	mux.HandleFunc("GET /api/shop/{mbid}/cover", a.shopCover)
	mux.Handle("GET /", webFiles())
	return mux
}

func (a *App) healthz(w http.ResponseWriter, r *http.Request) {
	if a.ready() != "" { // the reason is in the logs only: it can name hosts or addresses
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

func (a *App) item(al Album) Item {
	it := Item{MBID: al.MBID, Title: al.Title, Artist: al.ArtistName, ArtistMBID: al.ArtistMBID, Year: al.Year, Type: al.Type, Secondary: al.Secondary, Cover: al.Cover}
	if k := a.plexKey(al); k != "" {
		it.Status, it.PlexURL = "in_plex", a.svc.plex.URL(k)
		return it
	}
	if r, ok := a.svc.store.Get(al.MBID); ok {
		it.Status, it.Progress, it.Timeleft = r.Status, r.Progress, r.Timeleft
	}
	return it
}

// plexKey finds an album in Plex under any of its artist's names.
func (a *App) plexKey(al Album) string {
	if p := a.svc.plex; p != nil {
		for _, n := range a.svc.cachedNames(al.ArtistMBID, al.ArtistName) {
			if k := p.Find(n, al.Title); k != "" {
				return k
			}
		}
	}
	return ""
}

func (a *App) view(r Request) Item {
	at := r.RequestedAt
	it := Item{MBID: r.MBID, Title: r.Title, Artist: r.Artist, ArtistMBID: r.ArtistMBID, Year: r.Year, Type: r.Type, Secondary: r.Secondary,
		Cover: r.Cover, Status: r.Status, Progress: r.Progress, Timeleft: r.Timeleft, RequestedAt: &at,
		Picked: r.Picked, PickedFrom: r.PickedFrom, Removable: !onItsWay(r)}
	it.Note, it.NoteCode, it.NoteParams = noteView(r.Note)
	if r.ArtistMBID != "" {
		it.Names = a.svc.cachedNames(r.ArtistMBID, r.Artist)
	}
	if r.PlexKey != "" && a.svc.plex != nil {
		it.PlexURL = a.svc.plex.URL(r.PlexKey)
	}
	if it.Secondary == nil {
		it.Secondary = []string{}
	}
	return it
}

// rank puts studio albums first, then EPs, singles, and everything else.
func rank(al Album) int {
	switch {
	case al.Type == "Album" && len(al.Secondary) == 0:
		return 0
	case al.Type == "EP":
		return 1
	case al.Type == "Single":
		return 2
	}
	return 3
}

// remember caches successful metadata answers for 10 min: they cost a call to Lidarr's metadata server.
func (a *App) remember(key string, fetch func() (any, error)) (any, error) {
	a.mu.Lock()
	c, ok := a.cache[key]
	a.mu.Unlock()
	if ok && time.Since(c.at) < 10*time.Minute {
		return c.val, nil
	}
	v, err := fetch()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if len(a.cache) >= 500 { // shortcut: drop the whole cache when full, an LRU is not worth it for one user
		a.cache = map[string]cached{}
	}
	a.cache[key] = cached{time.Now(), v}
	a.mu.Unlock()
	return v, nil
}

func (a *App) lookup(ctx context.Context, q string) ([]Album, error) {
	v, err := a.remember("album:"+strings.ToLower(q), func() (any, error) {
		albums, err := a.svc.lidarr.Lookup(ctx, q)
		sort.SliceStable(albums, func(i, j int) bool { return rank(albums[i]) < rank(albums[j]) })
		return albums, err
	})
	albums, _ := v.([]Album)
	return albums, err
}

// queryProblem is the error code of what is wrong with a typed search, or "".
func queryProblem(q string) string {
	switch n := utf8.RuneCountInString(q); {
	case n < 2:
		return "query.tooShort"
	case n > 100:
		return "query.tooLong"
	}
	return ""
}

func (a *App) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if code := queryProblem(q); code != "" {
		fail(w, 400, code, nil)
		return
	}
	// The metadata server is sometimes slow: wait longer than a plain Lidarr call.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var artists []Artist
	var aerr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		v, err := a.remember("artist:"+strings.ToLower(q), func() (any, error) { return a.svc.lidarr.ArtistLookup(ctx, q) })
		artists, _ = v.([]Artist)
		aerr = err
	}()
	albums, err := a.lookup(ctx, q)
	<-done
	if err != nil {
		if aerr != nil || len(artists) == 0 {
			fail(w, 502, lidarrDown(err, "lidarr.metadata"), nil)
			return
		}
		log.Printf("lidarr: %v", err)
	} else if aerr != nil {
		log.Printf("lidarr (artists): %v", aerr)
	}
	artists = searchArtists(q, artists, albums)
	items := make([]Item, 0, len(albums))
	for _, al := range albums {
		items = append(items, a.item(al))
	}
	writeJSON(w, 200, map[string]any{"artists": artists, "results": items})
}

// searchArtists orders the artists of a search: first the artist the query is, or the artist of an
// album found that the query names ("Middle of Nowhere – Kacey Musgraves" leads to Kacey Musgraves,
// whom Lidarr's artist search misses); then Lidarr's guesses in its order, those whose name holds the
// query and more words last ("kanye west": Ye before "Kanye West Tribute Band").
func searchArtists(q string, found []Artist, albums []Album) []Artist {
	qw := words(q)
	within := func(long, short []string) bool { // short appears in long, whole words in a row
		for i := range long {
			if len(short) > 0 && startsWith(long[i:], short) {
				return true
			}
		}
		return false
	}
	byID := map[string]Artist{}
	for _, ar := range found {
		byID[ar.MBID] = ar
	}
	out, seen := []Artist{}, map[string]bool{}
	add := func(ar Artist) {
		if ar.MBID != "" && !seen[ar.MBID] {
			seen[ar.MBID] = true
			out = append(out, ar)
		}
	}
	for _, al := range albums {
		if within(qw, words(al.ArtistName)) {
			ar, ok := byID[al.ArtistMBID] // Lidarr's has the portrait
			if !ok {
				ar = Artist{MBID: al.ArtistMBID, Name: al.ArtistName}
			}
			add(ar)
		}
	}
	for _, ar := range found {
		if slices.Equal(words(ar.Name), qw) {
			add(ar)
		}
	}
	for _, ar := range found {
		if !within(words(ar.Name), qw) {
			add(ar)
		}
	}
	for _, ar := range found {
		add(ar)
	}
	return out[:min(len(out), 6)]
}

type artistPage struct {
	name   string
	albums []Album
}

// artist lists one artist's albums; their covers load lazily through /api/cover.
func (a *App) artist(w http.ResponseWriter, r *http.Request) {
	id, ok := validMBID(r.PathValue("mbid"))
	if !ok {
		fail(w, 400, "artist.invalid", nil)
		return
	}
	listens := make(chan map[string]int64, 1)
	go func() { listens <- a.albumListens(r.Context(), id) }()
	p, err := a.discography(r.Context(), id)
	if err != nil {
		fail(w, 502, lidarrDown(err, "lidarr.metadata"), nil)
		return
	}
	names := a.svc.artistNames(r.Context(), id, p.name) // also tells Plex's names for it
	counts := <-listens
	items := make([]Item, 0, len(p.albums))
	for _, al := range p.albums {
		it := a.item(al)
		it.Cover = "/api/cover/" + al.MBID
		it.Listens = counts[al.MBID]
		items = append(items, it)
	}
	writeJSON(w, 200, map[string]any{"artist": Artist{MBID: id, Name: p.name}, "results": items, "names": names})
}

// listensWait caps what the artist page waits for ListenBrainz: the albums matter more than their listens.
var listensWait = 4 * time.Second

func (a *App) albumListens(ctx context.Context, id string) map[string]int64 {
	if a.listens == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, listensWait)
	defer cancel()
	v, err := a.remember("listens:"+id, func() (any, error) { return a.listens.TopAlbums(ctx, id) })
	if err != nil {
		log.Printf("artist %s listens: %v", id, err)
	}
	counts, _ := v.(map[string]int64)
	return counts
}

func (a *App) discography(ctx context.Context, id string) (artistPage, error) {
	v, err := a.remember("discography:"+id, func() (any, error) {
		name, albums, err := a.meta.ArtistAlbums(ctx, id)
		return artistPage{name, albums}, err
	})
	p, _ := v.(artistPage)
	return p, err
}

// album is the album page: the album, its editions, and the version the user owns in Plex.
func (a *App) album(w http.ResponseWriter, r *http.Request) {
	id, ok := validMBID(r.PathValue("mbid"))
	if !ok {
		fail(w, 400, "album.invalid", nil)
		return
	}
	v, err := a.remember("release:"+id, func() (any, error) { return a.meta.Album(r.Context(), id) })
	if err != nil {
		fail(w, 502, lidarrDown(err, "lidarr.metadata"), nil)
		return
	}
	d := v.(AlbumDetail)
	listens := make(chan map[string]int64, 1)
	go func() { listens <- a.albumListens(r.Context(), d.ArtistMBID) }()
	al := Album{MBID: id, Title: d.Title, Type: d.Type, Secondary: d.Secondary, Year: d.Year, ArtistMBID: d.ArtistMBID, ArtistName: d.Artist}
	it := a.item(al)
	it.Cover = "/api/cover/" + id
	var owned any // null when Plex lacks it
	if k := a.plexKey(al); k != "" {
		if o, err := a.svc.plex.Version(r.Context(), k); err != nil {
			log.Printf("plex, album %s: %v", k, err)
		} else {
			titles, discs := make([]string, len(o.Tracks)), map[int]bool{}
			for i, t := range o.Tracks {
				titles[i], discs[t.Disc] = t.Title, true
			}
			owned = struct {
				Owned
				Edition *Match `json:"edition"`
			}{o, matchEdition(d.Editions, titles, len(discs))}
		}
	}
	it.Listens = (<-listens)[id]
	writeJSON(w, 200, map[string]any{"album": it, "editions": d.Editions, "owned": owned})
}

// cover redirects to an album's cover, found through Lidarr's lookup (the artist list carries none).
func (a *App) cover(w http.ResponseWriter, r *http.Request) {
	id, ok := validMBID(r.PathValue("mbid"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	albums, err := a.lookup(r.Context(), "lidarr:"+id)
	if err != nil || len(albums) == 0 || albums[0].Cover == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.Redirect(w, r, albums[0].Cover, http.StatusFound)
}

// lidarrDown is the error code of a Lidarr failure: too slow, unreachable, or answered (Lidarr said no).
func lidarrDown(err error, answered string) string {
	log.Printf("lidarr: %v", err)
	if errors.Is(err, context.DeadlineExceeded) {
		return "lidarr.slow"
	}
	var le *LidarrError
	if errors.As(err, &le) {
		return answered
	}
	return "lidarr.unreachable"
}

func (a *App) list(w http.ResponseWriter, r *http.Request) {
	reqs := a.svc.store.All()
	items := make([]Item, 0, len(reqs))
	for _, rq := range reqs {
		items = append(items, a.view(rq))
	}
	writeJSON(w, 200, map[string]any{"requests": items})
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validMBID(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	return s, uuidRe.MatchString(s)
}

func (a *App) create(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "badFormat", nil)
		return
	}
	var in struct {
		MBID string `json:"mbid"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		fail(w, 400, "album.invalid", nil)
		return
	}
	id, ok := validMBID(in.MBID)
	if !ok {
		fail(w, 400, "album.invalid", nil)
		return
	}
	if a.ready() != "" {
		fail(w, 503, "starting", nil)
		return
	}
	// WithoutCancel: if the phone drops the connection, the add still finishes and is saved.
	req, created, err := a.svc.Create(context.WithoutCancel(r.Context()), id)
	var le *LidarrError
	switch {
	case errors.Is(err, ErrStore):
		fail(w, 500, "request.notSaved", nil)
		return
	case errors.Is(err, ErrNotFound):
		fail(w, 404, "album.notInLidarr", nil)
		return
	case errors.As(err, &le):
		fail(w, 502, "request.refused", map[string]any{"detail": truncate(le.Message, 200)})
		return
	case err != nil:
		fail(w, 502, lidarrDown(err, "lidarr.refused"), nil)
		return
	}
	select {
	case a.nudge <- struct{}{}:
	default:
	}
	code := 200
	if created {
		code = 201
	}
	writeJSON(w, code, a.view(req))
}

func (a *App) retry(w http.ResponseWriter, r *http.Request) {
	id, ok := validMBID(r.PathValue("mbid"))
	if !ok {
		fail(w, 400, "album.invalid", nil)
		return
	}
	req, err := a.svc.Retry(context.WithoutCancel(r.Context()), id)
	switch {
	case errors.Is(err, ErrStore):
		fail(w, 500, "request.notSaved", nil)
		return
	case errors.Is(err, ErrNotFound):
		fail(w, 404, "request.notFound", nil)
		return
	case err != nil:
		fail(w, 502, lidarrDown(err, "retry.refused"), nil)
		return
	}
	select {
	case a.nudge <- struct{}{}:
	default:
	}
	writeJSON(w, 200, a.view(req))
}

func (a *App) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := validMBID(r.PathValue("mbid"))
	if !ok {
		fail(w, 400, "album.invalid", nil)
		return
	}
	switch err := a.svc.Remove(context.WithoutCancel(r.Context()), id); {
	case errors.Is(err, ErrStore):
		fail(w, 500, "remove.notSaved", nil)
	case errors.Is(err, ErrNotFound):
		fail(w, 404, "request.notFound", nil)
	case errors.Is(err, ErrBusy):
		fail(w, 409, "remove.busy", nil)
	case err != nil:
		fail(w, 502, lidarrDown(err, "remove.refused"), nil)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// sourcesTimeout stays under the server's 90 s WriteTimeout: Prowlarr waits for its slowest tracker.
var sourcesTimeout = 75 * time.Second

var tokenRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// leadingTags are what trackers put before the artist: « (Jazz, Fusion) [CD] », « [DVDA][QU] ».
var leadingTags = regexp.MustCompile(`^\s*(?:(?:\([^)]*\)|\[[^\]]*\])\s*)*`)

func (a *App) sourceSearch(w http.ResponseWriter, r *http.Request) {
	if a.sources == nil {
		fail(w, 503, "sources.off", nil)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if code := queryProblem(q); code != "" {
		fail(w, 400, code, nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), sourcesTimeout)
	defer cancel()
	year, _ := strconv.Atoi(r.URL.Query().Get("year")) // the album's, from its page: another year is a later pressing
	var shelf chan *albumShelf
	if album, ok := validMBID(r.URL.Query().Get("album")); ok {
		shelf = make(chan *albumShelf, 1)
		go func() { shelf <- a.shelfFor(ctx, album) }() // beside the trackers, which may take the whole minute
	}
	items, err := a.sources.Search(ctx, q, time.Now())
	if r.Context().Err() != nil { // the browser gave up (a new search replaced this one): nobody reads the answer
		return
	}
	if err != nil {
		status, code := sourceErr(err)
		fail(w, status, code, nil)
		return
	}
	if shelf != nil {
		if sh := <-shelf; sh != nil {
			sh.file(items)
			if year == 0 { // the page did not know the album's year: later ones in a name are still pressings
				year = sh.album.Year
			}
		}
	}
	for i := range items {
		items[i].Hints = editionHints(items[i].Title, year)
	}
	writeJSON(w, 200, map[string]any{"results": items})
}

// albumShelf is what an album's page needs to file the releases found from it: the album, the artist's
// other records and the names the artist goes by.
type albumShelf struct {
	id     string
	album  AlbumDetail
	names  [][]string
	keys   []albumKey
	titles map[string]string
}

// shelfFor reads the album and its artist's discography; nil when the album can't be read, and the
// list then stays as the trackers rank it.
func (a *App) shelfFor(ctx context.Context, id string) *albumShelf {
	if a.meta == nil {
		return nil
	}
	v, err := a.remember("release:"+id, func() (any, error) { return a.meta.Album(ctx, id) })
	if err != nil {
		log.Printf("album %s sources: %v", id, err)
		return nil
	}
	d := v.(AlbumDetail)
	albums := []Album{{MBID: id, Title: d.Title}}
	if p, err := a.discography(ctx, d.ArtistMBID); err == nil {
		albums = append(albums, p.albums...)
	}
	sh := &albumShelf{id: id, album: d, keys: albumKeys(albums), titles: map[string]string{}}
	for _, al := range albums {
		sh.titles[al.MBID] = al.Title
	}
	for _, n := range a.svc.artistNames(ctx, d.ArtistMBID, d.Artist) {
		sh.names = append(sh.names, words(n))
	}
	return sh
}

// file sorts the releases into the album itself, with the edition its name points to, then the
// artist's other records, then the rest; each group by quality.
func (sh *albumShelf) file(items []SourceItem) {
	for i := range items {
		// the artist leads a release of theirs, after the genre and source tags: « The Bitches Brew Band -
		// The Miles Davis Bitches Brew Album » is someone else's
		lead := words(leadingTags.ReplaceAllString(items[i].Title, ""))
		if !slices.ContainsFunc(sh.names, func(n []string) bool { return len(n) > 0 && startsWith(lead, n) }) {
			continue
		}
		switch mbid, named := albumFor(items[i].Title, sh.names, sh.keys); {
		case mbid == sh.id && !slices.Contains(items[i].Quality.Flags, "discography"):
			items[i].Place, items[i].Edition = "album", guessEdition(sh.album.Editions, items[i].Title, sh.album.Year)
		case mbid != "" && mbid != sh.id:
			items[i].Place, items[i].Other = "other", sh.titles[mbid]
		case named:
			items[i].Place = "artist"
		}
	}
	group := map[string]int{"album": 0, "other": 1, "artist": 1, "": 2}
	slices.SortStableFunc(items, func(x, y SourceItem) int {
		return cmp.Or(cmp.Compare(group[x.Place], group[y.Place]), sourceOrder(x, y))
	})
}

var audioExts = []string{".flac", ".wav", ".aiff", ".aif", ".ape", ".wv", ".alac", ".m4a", ".dsf", ".dff", ".mp3", ".ogg", ".opus", ".aac"}

type sourceFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Audio bool   `json:"audio"`
}

// sourceFiles reads what a release holds from its .torrent, and which edition of the album it is.
// Prowlarr logs the read as a grab: the page only asks on a tap.
func (a *App) sourceFiles(w http.ResponseWriter, r *http.Request) {
	if a.sources == nil {
		fail(w, 503, "sources.off", nil)
		return
	}
	var in struct {
		Token string `json:"token"`
		Album string `json:"album"` // optional: the album page it was opened from
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || !tokenRe.MatchString(in.Token) {
		fail(w, 400, "source.invalid", nil)
		return
	}
	files, err := a.sources.Files(r.Context(), in.Token, time.Now())
	switch {
	case errors.Is(err, ErrMagnet):
		fail(w, 422, "files.magnet", nil)
		return
	case errors.Is(err, ErrNZB):
		fail(w, 422, "files.usenet", nil)
		return
	case errors.Is(err, ErrNoTorrent):
		fail(w, 422, "files.hidden", nil)
		return
	case errors.Is(err, errTorrent):
		fail(w, 422, "files.unreadable", nil)
		return
	case err != nil:
		status, code := sourceErr(err)
		fail(w, status, code, nil)
		return
	}
	out := make([]sourceFile, len(files))
	var names []string
	dirs, cues := map[string]bool{}, 0
	for i, f := range files {
		name := f.Path[len(f.Path)-1]
		ext := strings.ToLower(filepath.Ext(name))
		out[i] = sourceFile{Name: name, Size: f.Size, Audio: slices.Contains(audioExts, ext)}
		if ext == ".cue" {
			cues++
		}
		if out[i].Audio {
			names = append(names, fileTitle(strings.TrimSuffix(name, filepath.Ext(name))))
			dirs[strings.Join(f.Path[:len(f.Path)-1], "/")] = true
		}
	}
	discs := max(len(dirs), min(len(names), 1))
	image := cues > 0 && len(names) <= max(cues, discs) // one file per disc beside its .cue
	tracks := len(names)
	var edition *Match
	if image {
		tracks = 0 // the .cue holds the tracks, and it is not read
	} else if id, ok := validMBID(in.Album); ok {
		if v, err := a.remember("release:"+id, func() (any, error) { return a.meta.Album(r.Context(), id) }); err == nil {
			edition = matchEdition(v.(AlbumDetail).Editions, names, discs)
		}
	}
	writeJSON(w, 200, map[string]any{"files": out, "tracks": tracks, "discs": discs, "image": image, "edition": edition})
}

// artistSources searches the trackers once for an artist, under its name or another one (q),
// and files each release under the album it is; the artist's discographies and the releases
// of no album go to others, and releases of other artists are left out.
func (a *App) artistSources(w http.ResponseWriter, r *http.Request) {
	if a.sources == nil {
		fail(w, 503, "sources.off", nil)
		return
	}
	id, ok := validMBID(r.PathValue("mbid"))
	if !ok {
		fail(w, 400, "artist.invalid", nil)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if code := queryProblem(q); q != "" && code != "" {
		fail(w, 400, code, nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), sourcesTimeout)
	defer cancel()
	p, err := a.discography(ctx, id)
	if err != nil {
		fail(w, 502, lidarrDown(err, "lidarr.metadata"), nil)
		return
	}
	if q == "" {
		q = p.name
	}
	names := a.svc.artistNames(ctx, id, p.name)
	items, err := a.sources.Search(ctx, q, time.Now())
	if r.Context().Err() != nil { // the user left the artist: nobody reads the answer
		return
	}
	if err != nil {
		status, code := sourceErr(err)
		fail(w, status, code, nil)
		return
	}
	ns := make([][]string, 0, len(names))
	for _, n := range names {
		ns = append(ns, words(n))
	}
	keys := albumKeys(p.albums)
	albums, others := map[string][]SourceItem{}, []SourceItem{}
	for _, it := range items {
		switch mbid, named := albumFor(it.Title, ns, keys); {
		case !named:
		case mbid == "" || slices.Contains(it.Quality.Flags, "discography"):
			others = append(others, it)
		default:
			albums[mbid] = append(albums[mbid], it)
		}
	}
	writeJSON(w, 200, map[string]any{"query": q, "albums": albums, "others": others, "names": names})
}

func (a *App) sourceGrab(w http.ResponseWriter, r *http.Request) {
	if a.sources == nil {
		fail(w, 503, "sources.off", nil)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, "badFormat", nil)
		return
	}
	var in struct {
		Token string `json:"token"`
		MBID  string `json:"mbid"` // optional: the request this source is for
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || !tokenRe.MatchString(in.Token) {
		fail(w, 400, "source.invalid", nil)
		return
	}
	id, linked := validMBID(in.MBID)
	switch {
	case in.MBID != "" && !linked:
		fail(w, 400, "album.invalid", nil)
		return
	case linked: // checked first: a grab cannot be taken back
		req, ok := a.svc.store.Get(id)
		if !ok {
			fail(w, 404, "request.notFound", nil)
			return
		}
		if !waitsForSource(req) {
			fail(w, 409, "grab.notWaiting", nil)
			return
		}
	}
	// WithoutCancel: a phone that drops the connection must not leave the grab half done.
	// The deadline is the search's: an indexer behind FlareSolverr can take longer than 30 s.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), sourcesTimeout)
	defer cancel()
	title, indexer, err := a.sources.Grab(ctx, in.Token, time.Now())
	if errors.Is(err, ErrGrabUnsure) && linked { // the release may be on its way: Lidarr must stop looking
		if _, perr := a.svc.Picked(ctx, id, title, indexer, time.Now()); perr != nil {
			log.Printf("request %s: %v", id, perr)
		}
	}
	if err != nil {
		status, code := sourceErr(err)
		fail(w, status, code, nil)
		return
	}
	if !linked {
		writeJSON(w, 200, map[string]any{"title": title})
		return
	}
	req, err := a.svc.Picked(ctx, id, title, indexer, time.Now())
	if err != nil {
		log.Printf("request %s: %v", id, err)
		fail(w, 500, "grab.notSaved", nil)
		return
	}
	select {
	case a.nudge <- struct{}{}:
	default:
	}
	writeJSON(w, 200, map[string]any{"title": title, "request": a.view(req)})
}

// waitsForSource is where « Choisir une source » shows: never while Musaarr's own search runs.
func waitsForSource(r Request) bool {
	stuck := r.Status == "waiting" || r.Status == "failed" || (r.Status == "check" && !r.PickedAt.IsZero())
	return stuck && !ownSearchRunning(r)
}

// sourceErr turns a Prowlarr failure into a status and an error code, without technical detail.
func sourceErr(err error) (int, string) {
	switch {
	case errors.Is(err, ErrSourceExpired):
		return 410, "source.expired"
	case errors.Is(err, ErrGrabUnsure):
		return 504, "grab.unsure"
	case errors.Is(err, ErrAlreadySent):
		return 409, "grab.alreadySent"
	case errors.Is(err, ErrSourceRefused):
		return 409, "grab.refused"
	case errors.Is(err, ErrProwlarrKey):
		return 502, "prowlarr.key"
	case errors.Is(err, context.DeadlineExceeded):
		return 504, "sources.slow"
	}
	log.Printf("prowlarr: %v", err)
	return 502, "prowlarr.unreachable"
}

/* ---------- Bibliothèque ---------- */

var plexKeyRe = regexp.MustCompile(`^[0-9]{1,12}$`)

// libAlbum is the Plex album a library route names, if Plex holds it.
func (a *App) libAlbum(r *http.Request) (LibAlbum, bool) {
	key := r.PathValue("key")
	if a.svc.plex == nil || !plexKeyRe.MatchString(key) {
		return LibAlbum{}, false
	}
	return a.svc.plex.Album(key)
}

// library is every album Plex holds, encoded at each reload; the tab revalidates it by ETag.
func (a *App) library(w http.ResponseWriter, r *http.Request) {
	if a.svc.plex == nil {
		fail(w, 503, "plex.off", nil)
		return
	}
	lib, ok := a.svc.plex.Library()
	if !ok {
		fail(w, 503, "plex.down", nil)
		return
	}
	serveEncoded(w, r, lib)
}

// serveEncoded sends a pre-encoded answer, gzipped when the browser takes it, revalidated by ETag.
func serveEncoded(w http.ResponseWriter, r *http.Request, lib Lib) {
	h := w.Header()
	body, tag := lib.Body, lib.Tag
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		body, tag = lib.Gz, strings.TrimSuffix(lib.Tag, `"`)+`-gz"`
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Vary", "Accept-Encoding")
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		h.Del("Content-Encoding")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	w.Write(body)
}

// coverTypes are the images a cover may be: never SVG, which could carry a script.
var coverTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}

// libraryCover relays a square thumbnail of a Plex album's artwork: the Plex token stays here.
func (a *App) libraryCover(w http.ResponseWriter, r *http.Request) {
	al, ok := a.libAlbum(r)
	if !ok || al.thumb == "" {
		http.NotFound(w, r)
		return
	}
	resp, err := a.svc.plex.Photo(r.Context(), al.thumb, 400)
	if err != nil {
		log.Printf("plex, cover %s: %v", al.Key, err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	ct, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
	if !coverTypes[strings.TrimSpace(ct)] {
		log.Printf("plex, cover %s: type %q", al.Key, ct)
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	io.Copy(w, io.LimitReader(resp.Body, 8<<20))
}

type albumRef struct {
	MBID       string `json:"mbid"`
	ArtistMBID string `json:"artistMbid"`
}

// libraryAlbum finds the MusicBrainz album behind a Plex album, so the tab can open its page.
func (a *App) libraryAlbum(w http.ResponseWriter, r *http.Request) {
	al, ok := a.libAlbum(r)
	if !ok {
		fail(w, 404, "plex.gone", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	v, err := a.remember("plexalbum:"+al.Key, func() (any, error) { return a.resolveLibrary(ctx, al) })
	if err != nil {
		fail(w, 502, lidarrDown(err, "lidarr.metadata"), nil)
		return
	}
	if ref := v.(albumRef); ref.MBID != "" {
		writeJSON(w, 200, ref)
		return
	}
	fail(w, 404, "album.notFound", nil)
}

// resolveLibrary tries the MusicBrainz id Plex holds, then Lidarr's lookup on "artist title": the
// same artist and title first, then the same title under another name (Plex files Ye under Kanye West).
// Found nothing is an answer too, remembered like the others.
func (a *App) resolveLibrary(ctx context.Context, al LibAlbum) (albumRef, error) {
	if id, ok := validMBID(al.mbid); ok && a.meta != nil {
		v, err := a.remember("release:"+id, func() (any, error) { return a.meta.Album(ctx, id) })
		if d, _ := v.(AlbumDetail); err == nil && d.ArtistMBID != "" {
			return albumRef{id, d.ArtistMBID}, nil
		}
	}
	found, err := a.lookup(ctx, al.Artist+" "+al.Title)
	if err != nil {
		return albumRef{}, err
	}
	want := normKey(al.Artist, al.Title)
	_, title, _ := strings.Cut(want, "|")
	var same *Album
	for i, c := range found {
		if _, ok := validMBID(c.ArtistMBID); !ok { // the album page needs its artist
			continue
		}
		k := normKey(c.ArtistName, c.Title)
		if k == want {
			return albumRef{c.MBID, c.ArtistMBID}, nil
		}
		if _, t, _ := strings.Cut(k, "|"); t == title && same == nil {
			same = &found[i]
		}
	}
	if same != nil {
		return albumRef{same.MBID, same.ArtistMBID}, nil
	}
	return albumRef{}, nil
}
