package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"slices"
	"sync"
	"time"
)

// ErrAlreadySent stops a second tap on Télécharger from sending the release twice.
var ErrAlreadySent = errors.New("already sent")

// ErrGrabUnsure: Prowlarr took the grab but did not answer in time; the release may have left.
var ErrGrabUnsure = errors.New("prowlarr did not confirm the grab")

// tokenTTL matches Prowlarr's own cache of a search: past it, a grab would 404 anyway.
const tokenTTL = 30 * time.Minute

// SourceItem is the only release shape the browser sees: no guid, no URL, no indexer id.
type SourceItem struct {
	Token     string    `json:"token"`
	Title     string    `json:"title"`
	Indexer   string    `json:"indexer"`
	Size      int64     `json:"size"`
	Seeders   int       `json:"seeders"`
	Published time.Time `json:"published"`
	Quality   Quality   `json:"quality"`
	Match     bool      `json:"match"`           // its name holds every word searched; trackers also answer loosely
	Hints     []string  `json:"hints,omitempty"` // the edition its name says it is
	// on an album's page: album (this album), other (another record of its artist, named in Other),
	// artist (its artist, no record known); "" for the rest, and on a free search
	Place   string `json:"place,omitempty"`
	Other   string `json:"other,omitempty"`
	Edition *Match `json:"edition,omitempty"` // the album's edition its name points to, named after the pressing
}

type held struct {
	guid, link     string
	indexerID      int
	title, indexer string
	expires        time.Time
	sent           bool
	unsure         bool // timed out mid-grab: never sent again
	files          []TorrentFile
}

// Sources searches Prowlarr and keeps each hit's guid server-side behind an opaque token.
type Sources struct {
	prowlarr *Prowlarr
	mu       sync.Mutex
	held     map[string]*held
}

func NewSources(p *Prowlarr) *Sources {
	return &Sources{prowlarr: p, held: map[string]*held{}}
}

func (s *Sources) Search(ctx context.Context, q string, now time.Time) ([]SourceItem, error) {
	hits, err := s.prowlarr.Search(ctx, q)
	if err != nil {
		return nil, err
	}
	items := make([]SourceItem, 0, len(hits))
	s.mu.Lock()
	defer s.mu.Unlock()
	for tok, h := range s.held {
		if now.After(h.expires) {
			delete(s.held, tok)
		}
	}
	ws := words(q)
	for _, h := range hits {
		if slices.Contains(h.Categories, 3030) { // audiobooks
			continue
		}
		tok := newToken()
		s.held[tok] = &held{guid: h.GUID, link: h.DownloadURL, indexerID: h.IndexerID, title: h.Title, indexer: h.Indexer, expires: now.Add(tokenTTL)}
		items = append(items, SourceItem{Token: tok, Title: h.Title, Indexer: h.Indexer, Size: h.Size,
			Seeders: h.Seeders, Published: h.PublishDate, Quality: ReadQuality(h.Title), Match: sourceMatches(ws, h.Title)})
	}
	slices.SortStableFunc(items, sourceOrder)
	return items, nil
}

// sourceOrder ranks releases: what was searched for, alive, best quality, highest sample rate, tracks before an image + cue, most seeded.
func sourceOrder(a, b SourceItem) int {
	return cmp.Or(
		cmp.Compare(btoi(!a.Match), btoi(!b.Match)),             // what was searched for, before what a tracker guessed
		cmp.Compare(btoi(a.Seeders == 0), btoi(b.Seeders == 0)), // dead releases last
		cmp.Compare(a.Quality.Level(), b.Quality.Level()),
		cmp.Compare(b.Quality.Rate(), a.Quality.Rate()), // 24/192, 24/96, 24/48, then a rate the name does not give
		cmp.Compare(btoi(a.Quality.Layout == "image"), btoi(b.Quality.Layout == "image")),
		cmp.Compare(b.Seeders, a.Seeders),
	)
}

// Grab sends the release behind token to the download client, once.
func (s *Sources) Grab(ctx context.Context, token string, now time.Time) (title, indexer string, err error) {
	s.mu.Lock()
	h, ok := s.held[token]
	switch {
	case !ok || now.After(h.expires):
		s.mu.Unlock()
		return "", "", ErrSourceExpired
	case h.unsure:
		s.mu.Unlock()
		return h.title, h.indexer, ErrGrabUnsure
	case h.sent:
		s.mu.Unlock()
		return h.title, h.indexer, ErrAlreadySent
	}
	h.sent = true // claimed before the call: a double tap waits for nothing and sends nothing
	s.mu.Unlock()
	if err := s.prowlarr.Grab(ctx, h.guid, h.indexerID); err != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !notSent(err) { // Prowlarr went quiet mid-grab: it may still reach qBittorrent
			h.unsure = true
			log.Printf("source maybe sent: %s (%s)", h.title, h.indexer)
			return h.title, h.indexer, ErrGrabUnsure
		}
		h.sent = false // nothing left: the user can try again
		return "", "", err
	}
	log.Printf("source sent: %s (%s)", h.title, h.indexer)
	return h.title, h.indexer, nil
}

// Files lists what the release behind token holds, from its .torrent, read once through Prowlarr.
func (s *Sources) Files(ctx context.Context, token string, now time.Time) ([]TorrentFile, error) {
	s.mu.Lock()
	h, ok := s.held[token]
	if !ok || now.After(h.expires) {
		s.mu.Unlock()
		return nil, ErrSourceExpired
	}
	files, link := h.files, h.link
	s.mu.Unlock()
	if files != nil {
		return files, nil
	}
	b, err := s.prowlarr.Fetch(ctx, link)
	if err != nil {
		return nil, err
	}
	if files, err = torrentFiles(b); err != nil {
		return nil, err
	}
	s.mu.Lock()
	h.files = files
	s.mu.Unlock()
	log.Printf("contents read: %s (%s)", h.title, h.indexer)
	return files, nil
}

// notSent tells a grab that surely did not happen (Prowlarr answered, or was never reached)
// from one that timed out after Prowlarr had it.
func notSent(err error) bool {
	var se *StatusError
	var op *net.OpError
	return errors.Is(err, ErrSourceExpired) || errors.Is(err, ErrSourceRefused) || errors.Is(err, ErrProwlarrKey) ||
		errors.As(err, &se) || (errors.As(err, &op) && op.Op == "dial")
}

// sourceMatches is true when the title holds every word searched, as a whole word or as a run of
// its words written together: « bon iver » is in « Bon.Iver.For.Emma », not in « Boney M - Rivers »;
// « acdc » is in « AC/DC ». The words are pick.go's: folded, « & » read as « and ».
func sourceMatches(ws []string, title string) bool {
	t := words(title)
	for _, w := range ws {
		if !holdsWord(t, w) {
			return false
		}
	}
	return true
}

func holdsWord(t []string, w string) bool {
	for i := range t {
		run := ""
		for _, x := range t[i:] {
			if run += x; len(run) >= len(w) {
				break
			}
		}
		if run == w {
			return true
		}
	}
	return false
}

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b) // never fails (crypto/rand panics instead since Go 1.24)
	return hex.EncodeToString(b)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
