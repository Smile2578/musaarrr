package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newRoomApp(t *testing.T) (*App, string, func(method, body string) (int, Room, string)) {
	_, a, h := newTestApp(t)
	dir := t.TempDir()
	rf, err := OpenRoom(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.room = rf
	send := func(method, body string) (int, Room, string) {
		w := call(h, method, "/api/room", body)
		var got Room
		json.Unmarshal(w.Body.Bytes(), &got)
		return w.Code, got, errCode(w)
	}
	return a, dir, send
}

func TestRoomRoundTrip(t *testing.T) {
	_, dir, send := newRoomApp(t)
	if code, got, _ := send("GET", ""); code != 200 || got.Finish != "" || len(got.Shelves) != 0 {
		t.Fatalf("empty room: %d %+v", code, got)
	}
	body := `{"finish":"noyer","wall":"brique","shelves":[{"id":"top","deco":"plante"},{"id":"deck","off":true}],"vitrine":["123","456"]}`
	code, got, _ := send("PUT", body)
	if code != 200 || got.Finish != "noyer" || got.Wall != "brique" || len(got.Shelves) != 2 || got.Shelves[0].Deco != "plante" || !got.Shelves[1].Off || len(got.Vitrine) != 2 {
		t.Fatalf("put: %d %+v", code, got)
	}
	again, err := OpenRoom(dir) // the room survives a restart
	if err != nil {
		t.Fatal(err)
	}
	if r := again.Get(); r.Finish != "noyer" || len(r.Shelves) != 2 || r.Vitrine[1] != "456" {
		t.Errorf("reopened %+v", r)
	}
}

func TestRoomCleansWhatItDoesNotKnow(t *testing.T) {
	_, _, send := newRoomApp(t)
	body := `{"finish":"marbre","wall":"<b>","shelves":[{"id":"top","deco":"fusée"},{"id":"top"},{"id":"hall"},{"id":"live","deco":"lampe"}],
	  "vitrine":["123","123","abc","9999999999999","456"]}`
	code, got, _ := send("PUT", body)
	if code != 200 || got.Finish != "" || got.Wall != "" {
		t.Fatalf("%d %+v", code, got)
	}
	if len(got.Shelves) != 2 || got.Shelves[0].ID != "top" || got.Shelves[0].Deco != "" || got.Shelves[1].ID != "live" || got.Shelves[1].Deco != "lampe" {
		t.Errorf("shelves %+v", got.Shelves)
	}
	if strings.Join(got.Vitrine, ",") != "123,456" {
		t.Errorf("vitrine %v", got.Vitrine)
	}
	many := make([]string, 40)
	for i := range many {
		many[i] = `"` + strings.Repeat("1", i%12+1) + string(rune('0'+i/12)) + `"`
	}
	_, got, _ = send("PUT", `{"vitrine":[`+strings.Join(many, ",")+`]}`)
	if len(got.Vitrine) != 24 {
		t.Errorf("vitrine kept %d", len(got.Vitrine))
	}
}

func TestRoomRejects(t *testing.T) {
	a, dir, send := newRoomApp(t)
	if code, _, msg := send("PUT", `{"finish":`); code != 400 || msg != "deco.unreadable" {
		t.Errorf("bad json: %d %q", code, msg)
	}
	if code, _, _ := send("PUT", `{"finish":"`+strings.Repeat("a", 20000)+`"}`); code != 413 {
		t.Errorf("too big: %d", code)
	}
	send("PUT", `{"finish":"chene"}`)
	if err := os.Mkdir(filepath.Join(dir, "room.json.tmp"), 0o700); err != nil { // the write cannot happen, even as root
		t.Fatal(err)
	}
	code, _, msg := send("PUT", `{"finish":"acier"}`)
	if code != 500 || msg != "deco.notSaved" {
		t.Errorf("write failure: %d %q", code, msg)
	}
	if a.room.Get().Finish != "acier" {
		t.Errorf("not kept in memory: %+v", a.room.Get())
	}
}
