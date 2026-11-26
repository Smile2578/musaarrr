package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newLibraryApp is an app whose Plex holds libraryJSON.
func newLibraryApp(t *testing.T) (*fake, *fake, *App, http.Handler) {
	f, a, h := newTestApp(t)
	pf, p := newTestPlex(t)
	pf.reply("GET /library/sections/3/all", 200, libraryJSON)
	if err := p.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	a.svc.plex = p
	return f, pf, a, h
}

func TestLibrary(t *testing.T) {
	_, _, _, h := newLibraryApp(t)
	w := call(h, "GET", "/api/library", "")
	var lib struct{ Albums []LibAlbum }
	json.Unmarshal(w.Body.Bytes(), &lib)
	tag := w.Header().Get("ETag")
	if w.Code != 200 || len(lib.Albums) != 2 || tag == "" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("got %d %v %s", w.Code, w.Header(), w.Body)
	}
	r := httptest.NewRequest("GET", "/api/library", nil)
	r.Header.Set("Tailscale-User-Login", "a@b")
	r.Header.Set("If-None-Match", tag)
	if w := do(h, r); w.Code != 304 || w.Body.Len() != 0 {
		t.Errorf("revalidation: %d %d bytes", w.Code, w.Body.Len())
	}
	r = httptest.NewRequest("GET", "/api/library", nil)
	r.Header.Set("Tailscale-User-Login", "a@b")
	r.Header.Set("Accept-Encoding", "gzip, deflate, br")
	w = do(h, r)
	zr, err := gzip.NewReader(w.Body)
	if err != nil || w.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(w.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("gzip: %v %v", err, w.Header())
	}
	if b, _ := io.ReadAll(zr); !bytes.Contains(b, []byte("Extension of a Man")) {
		t.Errorf("gzip body %s", b)
	}
}

func TestLibraryWithoutPlex(t *testing.T) {
	_, a, h := newTestApp(t)
	if w := call(h, "GET", "/api/library", ""); w.Code != 503 || errCode(w) != "plex.off" {
		t.Errorf("off: %d %s", w.Code, w.Body)
	}
	pf, p := newTestPlex(t)
	pf.reply("GET /identity", 500, `{}`)
	p.Reload(ctx)
	a.svc.plex = p
	if w := call(h, "GET", "/api/library", ""); w.Code != 503 || errCode(w) != "plex.down" {
		t.Errorf("down: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/api/library/123/album", ""); w.Code != 404 {
		t.Errorf("album while down: %d", w.Code)
	}
}

func TestLibraryCover(t *testing.T) {
	_, pf, _, h := newLibraryApp(t)
	pf.handle("GET /photo/:/transcode", func(w http.ResponseWriter, r *http.Request, _ string) {
		q := r.URL.Query()
		if q.Get("url") != "/library/metadata/123/thumb/1700" || q.Get("width") != "400" || q.Get("height") != "400" || q.Has("X-Plex-Token") {
			http.Error(w, "bad query "+r.URL.RawQuery, 400)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("\xff\xd8jpeg"))
	})
	w := call(h, "GET", "/api/library/123/cover?v=1700", "")
	if w.Code != 200 || w.Body.String() != "\xff\xd8jpeg" || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Cache-Control") != "private, max-age=86400" {
		t.Fatalf("got %d %v %q", w.Code, w.Header(), w.Body)
	}
	for _, path := range []string{"/api/library/456/cover", "/api/library/999/cover", "/api/library/12a/cover"} {
		if w := call(h, "GET", path, ""); w.Code != 404 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	pf.reply("GET /photo/:/transcode", 404, `{}`) // no transcoder: the artwork itself
	pf.handle("GET /library/metadata/123/thumb/1700", func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png"))
	})
	if w := call(h, "GET", "/api/library/123/cover", ""); w.Code != 200 || w.Body.String() != "png" || w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("fallback: %d %v %q", w.Code, w.Header(), w.Body)
	}
	pf.handle("GET /photo/:/transcode", func(w http.ResponseWriter, r *http.Request, _ string) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<script>"))
	})
	if w := call(h, "GET", "/api/library/123/cover", ""); w.Code != 502 || strings.Contains(w.Body.String(), "script") {
		t.Errorf("not an image: %d %s", w.Code, w.Body)
	}
}

func TestLibraryAlbumByGuid(t *testing.T) {
	f, pf, a, h := newLibraryApp(t)
	pf.reply("GET /library/sections/3/all", 200, strings.Replace(libraryJSON, "5b11f4ce-a62d-471e-81fc-a69a8278c7da", lateRegMBID, 1))
	a.svc.plex.Reload(ctx)
	a.meta = newAlbumMetadata(t, lateRegJSON)
	w := call(h, "GET", "/api/library/123/album", "")
	var ref struct{ MBID, ArtistMBID string }
	json.Unmarshal(w.Body.Bytes(), &ref)
	if w.Code != 200 || ref.MBID != lateRegMBID || ref.ArtistMBID != "164f0d73-1234-4e2c-8743-d77bf2191051" {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	if n := f.count("GET /api/v1/album/lookup"); n != 0 {
		t.Errorf("lookups %d", n)
	}
}

func TestLibraryAlbumByLookup(t *testing.T) {
	f, _, a, h := newLibraryApp(t)
	a.meta = newAlbumMetadata(t, lateRegJSON) // the guid 5b11… is unknown there
	f.reply("GET /api/v1/album/lookup", 200, `[
	 {"foreignAlbumId":"aaaaaaaa-0000-0000-0000-000000000001","title":"Extension of a Man","artist":{"foreignArtistId":"bbbbbbbb-0000-0000-0000-000000000001","artistName":"Donny Hathaway Tribute"}},
	 {"foreignAlbumId":"aaaaaaaa-0000-0000-0000-000000000002","title":"Extension of a Man (Deluxe)","artist":{"foreignArtistId":"bbbbbbbb-0000-0000-0000-000000000002","artistName":"Donny Hathaway"}}]`)
	var ref struct{ MBID, ArtistMBID string }
	for range 2 {
		w := call(h, "GET", "/api/library/123/album", "")
		json.Unmarshal(w.Body.Bytes(), &ref)
		if w.Code != 200 || ref.MBID != "aaaaaaaa-0000-0000-0000-000000000002" || ref.ArtistMBID != "bbbbbbbb-0000-0000-0000-000000000002" {
			t.Fatalf("exact match first: %d %s", w.Code, w.Body)
		}
	}
	if n := f.count("GET /api/v1/album/lookup?term=Donny+Hathaway+Extension+of+a+Man"); n != 1 {
		t.Errorf("lookups %v", f.calls)
	}
	// Plex files Ye's albums under « Kanye West »: the same title under another name still opens.
	f.reply("GET /api/v1/album/lookup", 200, `[{"foreignAlbumId":"aaaaaaaa-0000-0000-0000-000000000003","title":"Bootleg","artist":{"foreignArtistId":"bbbbbbbb-0000-0000-0000-000000000003","artistName":"Radiohead Tribute"}}]`)
	w := call(h, "GET", "/api/library/456/album", "")
	json.Unmarshal(w.Body.Bytes(), &ref)
	if w.Code != 200 || ref.MBID != "aaaaaaaa-0000-0000-0000-000000000003" {
		t.Fatalf("same title: %d %s", w.Code, w.Body)
	}
}

func TestLibraryAlbumNotFound(t *testing.T) {
	f, _, a, h := newLibraryApp(t)
	a.meta = newAlbumMetadata(t, lateRegJSON)
	f.reply("GET /api/v1/album/lookup", 200, `[{"foreignAlbumId":"aaaaaaaa-0000-0000-0000-000000000009","title":"Something Else","artist":{"foreignArtistId":"bbbbbbbb-0000-0000-0000-000000000009","artistName":"Donny Hathaway"}}]`)
	if w := call(h, "GET", "/api/library/123/album", ""); w.Code != 404 || errCode(w) != "album.notFound" {
		t.Errorf("got %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/api/library/999/album", ""); w.Code != 404 || errCode(w) != "plex.gone" {
		t.Errorf("unknown key: %d %s", w.Code, w.Body)
	}
	f.reply("GET /api/v1/album/lookup", 500, `{"message":"boom"}`)
	if w := call(h, "GET", "/api/library/456/album", ""); w.Code != 502 {
		t.Errorf("lidarr down: %d %s", w.Code, w.Body)
	}
}
