package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Request struct {
	MBID        string    `json:"mbid"`
	Title       string    `json:"title"`
	Artist      string    `json:"artist"`
	Type        string    `json:"type"`
	Secondary   []string  `json:"secondary"`
	Cover       string    `json:"cover"`
	Year        int       `json:"year"`
	AlbumID     int       `json:"albumId"`
	CommandID   int       `json:"commandId"`            // Lidarr AlbumSearch, before Musaarr searched itself
	Cmd         *Command  `json:"cmd,omitempty"`        // outcome of the last search
	SearchedAt  time.Time `json:"searchedAt,omitzero"`  // start of Musaarr's own search
	Source      string    `json:"source,omitempty"`     // the release it grabbed
	DownloadID  string    `json:"downloadId,omitempty"` // of that grab, to follow it when Lidarr cannot
	ImportCmd   int       `json:"importCmd,omitempty"`  // Musaarr's manual import; -1: files left to the user
	RequestedAt time.Time `json:"requestedAt"`
	CheckedAt   time.Time `json:"checkedAt"`
	Status      string    `json:"status"`
	Progress    int       `json:"progress"`
	Timeleft    string    `json:"timeleft"`
	Note        string    `json:"note"`
	PlexKey     string    `json:"plexKey"`
	ImportedAt  time.Time `json:"importedAt,omitzero"`
	ArtistMBID  string    `json:"artistMbid,omitempty"` // for the artist's other names in Plex
	Picked      string    `json:"picked,omitempty"`     // release the user chose in the Sources tab
	PickedFrom  string    `json:"pickedFrom,omitempty"` // its indexer
	PickedAt    time.Time `json:"pickedAt,omitzero"`
}

// ErrStore means requests.json could not be written; the change is kept in memory.
var ErrStore = errors.New("cannot save")

// Store keeps the requests in one JSON file, rewritten atomically on every change.
type Store struct {
	path string
	mu   sync.Mutex
	reqs map[string]Request
}

func OpenStore(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "requests.json"), reqs: map[string]Request{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Request
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("%s unreadable (file left untouched): %w", s.path, err)
	}
	for _, r := range list {
		if r.Status == "in_plex" { // never read again: a note from before v3.1.1 would stay for good
			r.Note = ""
		}
		s.reqs[r.MBID] = r
	}
	return s, nil
}

func (s *Store) Get(mbid string) (Request, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.reqs[mbid]
	return r, ok
}

// All returns the requests, newest first.
func (s *Store) All() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sorted()
}

func (s *Store) sorted() []Request {
	list := make([]Request, 0, len(s.reqs))
	for _, r := range s.reqs {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].RequestedAt.After(list[j].RequestedAt) })
	return list
}

// Put keeps r in memory even when the disk write fails, so a search already sent to
// Lidarr is never forgotten (and never sent again); the next successful Put saves it.
func (s *Store) Put(r Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs[r.MBID] = r
	b, err := json.MarshalIndent(s.sorted(), "", " ")
	if err == nil {
		err = writeFileSync(s.path, b)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStore, err)
	}
	return nil
}

// Delete forgets mbid, in memory even when the disk write fails, like Put.
func (s *Store) Delete(mbid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.reqs, mbid)
	b, err := json.MarshalIndent(s.sorted(), "", " ")
	if err == nil {
		err = writeFileSync(s.path, b)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStore, err)
	}
	return nil
}

// replace swaps every request for list, from a restored backup.
func (s *Store) replace(list []Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.reqs
	s.reqs = map[string]Request{}
	for _, r := range list {
		s.reqs[r.MBID] = r
	}
	b, err := json.MarshalIndent(s.sorted(), "", " ")
	if err == nil {
		err = writeFileSync(s.path, b)
	}
	if err != nil {
		s.reqs = old // unlike Put, nothing here was sent to Lidarr: keep what is on disk
	}
	return err
}

// writeFileSync replaces path atomically and durably: temp file, fsync, rename, fsync of the folder.
func writeFileSync(path string, b []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
