package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Room is how the user decorated the Bibliothèque: the wood, the wall, the shelves in their order
// with an object each, and the albums he set in his display. It is kept on the NAS so the
// iPhone, the iPad and the computer show the same room.
type Room struct {
	Finish  string   `json:"finish,omitempty"`
	Wall    string   `json:"wall,omitempty"`
	Shelves []Shelf  `json:"shelves,omitempty"`
	Vitrine []string `json:"vitrine,omitempty"` // Plex album keys
}

type Shelf struct {
	ID   string `json:"id"`
	Off  bool   `json:"off,omitempty"`
	Deco string `json:"deco,omitempty"`
}

var (
	roomFinishes = []string{"chene", "noyer", "noir", "blanc", "acier"}
	roomWalls    = []string{"uni", "pois", "lattes", "brique"}
	roomShelves  = []string{"deck", "vitrine", "top", "recent", "added", "never", "rediscover", "album", "single", "live", "compilation", "soundtrack", "remix"}
	roomDecos    = []string{"platine", "plante", "lampe", "enceinte", "casque", "cactus"}
)

const vitrineMax = 24

// clean keeps only what this build knows: a room saved by another version, or edited by hand,
// never breaks the page.
func (r Room) clean() Room {
	out := Room{}
	if slices.Contains(roomFinishes, r.Finish) {
		out.Finish = r.Finish
	}
	if slices.Contains(roomWalls, r.Wall) {
		out.Wall = r.Wall
	}
	for _, s := range r.Shelves {
		if !slices.Contains(roomShelves, s.ID) || slices.ContainsFunc(out.Shelves, func(o Shelf) bool { return o.ID == s.ID }) {
			continue
		}
		if !slices.Contains(roomDecos, s.Deco) {
			s.Deco = ""
		}
		out.Shelves = append(out.Shelves, s)
	}
	for _, k := range r.Vitrine {
		if len(out.Vitrine) == vitrineMax {
			break
		}
		if plexKey(k) && !slices.Contains(out.Vitrine, k) {
			out.Vitrine = append(out.Vitrine, k)
		}
	}
	return out
}

func plexKey(k string) bool {
	if k == "" || len(k) > 12 {
		return false
	}
	for _, c := range k {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// RoomFile keeps the room in room.json, beside requests.json.
type RoomFile struct {
	path string
	mu   sync.Mutex
	room Room
}

func OpenRoom(dir string) (*RoomFile, error) {
	f := &RoomFile{path: filepath.Join(dir, "room.json")}
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	var r Room
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s unreadable (file left untouched): %w", f.path, err)
	}
	f.room = r.clean()
	return f, nil
}

func (f *RoomFile) Get() Room {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.room
}

// Put keeps r in memory even when the disk write fails, like the requests.
func (f *RoomFile) Put(r Room) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.room = r
	b, err := json.MarshalIndent(r, "", " ")
	if err == nil {
		err = writeFileSync(f.path, b)
	}
	return err
}

func (a *App) getRoom(w http.ResponseWriter, r *http.Request) {
	if a.room == nil {
		writeJSON(w, 200, Room{})
		return
	}
	writeJSON(w, 200, a.room.Get())
}

func (a *App) putRoom(w http.ResponseWriter, r *http.Request) {
	if a.room == nil {
		fail(w, 503, "deco.off", nil)
		return
	}
	var in Room
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		var big *http.MaxBytesError
		if errors.As(err, &big) {
			fail(w, 413, "deco.tooLong", nil)
			return
		}
		fail(w, 400, "deco.unreadable", nil)
		return
	}
	room := in.clean()
	if err := a.room.Put(room); err != nil {
		fail(w, 500, "deco.notSaved", nil)
		return
	}
	writeJSON(w, 200, room)
}
