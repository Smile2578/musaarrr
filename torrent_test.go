package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
)

// bencode writes v the way a .torrent is written, for the tests.
func bencode(v any) string {
	switch x := v.(type) {
	case int:
		return fmt.Sprintf("i%de", x)
	case string:
		return fmt.Sprintf("%d:%s", len(x), x)
	case []any:
		var b strings.Builder
		for _, e := range x {
			b.WriteString(bencode(e))
		}
		return "l" + b.String() + "e"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			b.WriteString(bencode(k) + bencode(x[k]))
		}
		return "d" + b.String() + "e"
	}
	panic(fmt.Sprintf("bencode %T", v))
}

func multiTorrent(paths ...string) []byte {
	var files []any
	for i, p := range paths {
		var parts []any
		for _, s := range strings.Split(p, "/") {
			parts = append(parts, s)
		}
		files = append(files, map[string]any{"length": 1000 * (i + 1), "path": parts})
	}
	return []byte(bencode(map[string]any{"announce": "https://tracker.example/abc/announce",
		"info": map[string]any{"name": "Album", "piece length": 16384, "pieces": "xxxxxxxxxxxxxxxxxxxx", "files": files}}))
}

func TestTorrentFilesMulti(t *testing.T) {
	got, err := torrentFiles(multiTorrent("CD1/01 - A.flac", "CD2/01 - B.flac", "cover.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || !slices.Equal(got[1].Path, []string{"CD2", "01 - B.flac"}) || got[2].Size != 3000 {
		t.Fatalf("got %+v", got)
	}
}

func TestTorrentFilesSingle(t *testing.T) {
	b := bencode(map[string]any{"info": map[string]any{"name": "Album.flac", "length": 123456, "pieces": "x"}})
	got, err := torrentFiles([]byte(b))
	if err != nil || len(got) != 1 || got[0].Path[0] != "Album.flac" || got[0].Size != 123456 {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestTorrentFilesV2(t *testing.T) {
	tree := map[string]any{"CD1": map[string]any{"01 A.flac": map[string]any{"": map[string]any{"length": 5, "pieces root": "r"}}}}
	b := bencode(map[string]any{"info": map[string]any{"name": "Album", "meta version": 2, "file tree": tree}})
	got, err := torrentFiles([]byte(b))
	if err != nil || len(got) != 1 || !slices.Equal(got[0].Path, []string{"CD1", "01 A.flac"}) || got[0].Size != 5 {
		t.Fatalf("got %+v %v", got, err)
	}
}

func TestTorrentFilesRefusesGarbage(t *testing.T) {
	for _, b := range []string{"", "<html>login</html>", "d4:infoi3ee", "d4:info", strings.Repeat("l", 100) + strings.Repeat("e", 100), "d4:infod5:filesl" + "i9999999999999999999999e" + "eee"} {
		if _, err := torrentFiles([]byte(b)); err == nil {
			t.Errorf("accepted %.30q", b)
		}
	}
}

func TestTorrentFilesSkipsPadding(t *testing.T) {
	b := []byte(bencode(map[string]any{"info": map[string]any{"name": "Album", "piece length": 16384, "pieces": "xxxxxxxxxxxxxxxxxxxx", "files": []any{
		map[string]any{"length": 1000, "path": []any{"01 A.flac"}},
		map[string]any{"length": 15384, "path": []any{".pad", "15384"}, "attr": "p"},
		map[string]any{"length": 2000, "path": []any{"02 B.flac"}},
	}}}))
	got, err := torrentFiles(b)
	if err != nil || len(got) != 2 || got[1].Path[0] != "02 B.flac" {
		t.Fatalf("got %+v %v", got, err) // BEP 47 padding files are not the release's
	}
}

func TestTorrentFilesEmptyIsRead(t *testing.T) {
	b := []byte(bencode(map[string]any{"info": map[string]any{"name": "Album", "piece length": 16384, "pieces": "xxxxxxxxxxxxxxxxxxxx", "files": []any{}}}))
	got, err := torrentFiles(b)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v %v", got, err) // read, and empty: not nil, so the read is kept
	}
}
