package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"io/fs"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"
)

func getStatic(t *testing.T, path string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	_, _, h := newTestApp(t)
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Tailscale-User-Login", "a@b")
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return do(h, r)
}

func embedded(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fs.ReadFile(webFS, "web/"+name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestStaticGzip(t *testing.T) {
	w := getStatic(t, "/app.js", "Accept-Encoding", "gzip, deflate, br")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("got %d %v", w.Code, w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if !bytes.Equal(got, embedded(t, "app.js")) {
		t.Fatal("gzip body is not app.js")
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Fatalf("content type %q", ct)
	}
}

func TestStaticIdentity(t *testing.T) {
	w := getStatic(t, "/app.css")
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), embedded(t, "app.css")) {
		t.Fatalf("got %d %v", w.Code, w.Header())
	}
}

func TestStaticRevalidates(t *testing.T) {
	for _, enc := range []string{"", "gzip"} {
		w := getStatic(t, "/app.css", "Accept-Encoding", enc)
		etag := w.Header().Get("ETag")
		if etag == "" || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%q: headers %v", enc, w.Header())
		}
		w2 := getStatic(t, "/app.css", "Accept-Encoding", enc, "If-None-Match", etag)
		if w2.Code != 304 || w2.Body.Len() != 0 {
			t.Fatalf("%q: revalidation got %d with %d bytes", enc, w2.Code, w2.Body.Len())
		}
	}
	plain := getStatic(t, "/app.css").Header().Get("ETag")
	zipped := getStatic(t, "/app.css", "Accept-Encoding", "gzip").Header().Get("ETag")
	if plain == zipped {
		t.Fatal("gzip and identity bodies share one ETag")
	}
}

func TestStaticRoutes(t *testing.T) {
	w := getStatic(t, "/")
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || !bytes.Equal(w.Body.Bytes(), embedded(t, "index.html")) {
		t.Fatalf("index: %d %v", w.Code, w.Header())
	}
	if w := getStatic(t, "/manifest.webmanifest"); w.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("manifest: %v", w.Header())
	}
	for _, p := range []string{"/fonts/", "/nope.js", "/api.go"} {
		if w := getStatic(t, p); w.Code != 404 {
			t.Fatalf("%s: %d", p, w.Code)
		}
	}
}

// The 3D shop's models are binary glTF, served as such; three.js is the one bundle the stage
// uses too, and it carries everything the shop and the stage take from it.
func TestShop3DFiles(t *testing.T) {
	w := getStatic(t, "/shop3d/assets/wooden_stool_01/wooden_stool_01.glb")
	if w.Code != 200 || w.Header().Get("Content-Type") != "model/gltf-binary" || !bytes.HasPrefix(w.Body.Bytes(), []byte("glTF")) {
		t.Fatalf("glb: %d %v", w.Code, w.Header())
	}
	tex := getStatic(t, "/shop3d/assets/tex/painted_plaster_wall_diffuse.ktx2")
	if tex.Code != 200 || tex.Header().Get("Content-Type") != "image/ktx2" || !bytes.HasPrefix(tex.Body.Bytes(), []byte("\xabKTX 20\xbb")) {
		t.Fatalf("ktx2: %d %v", tex.Code, tex.Header())
	}
	exported := map[string]bool{}
	for _, m := range regexp.MustCompile(`[\w$]+ as (\w+)[,}]`).FindAllStringSubmatch(string(embedded(t, "vendor/three.min.js")), -1) {
		exported[m[1]] = true
	}
	files, err := fs.Glob(webFS, "web/shop3d/*.js")
	if err != nil {
		t.Fatal(err)
	}
	// what a file takes from the bundle: THREE.Name, and the names in braces imported from it
	used := regexp.MustCompile(`THREE\.(\w+)`)
	named := regexp.MustCompile(`(?s)import \{([^}]*)\} from "[./]*vendor/three\.min\.js"`)
	for _, f := range append(files, "web/stage.js") {
		src := string(embedded(t, strings.TrimPrefix(f, "web/")))
		var names []string
		for _, m := range used.FindAllStringSubmatch(src, -1) {
			names = append(names, m[1])
		}
		for _, m := range named.FindAllStringSubmatch(src, -1) {
			for _, n := range strings.Split(m[1], ",") {
				if n = strings.TrimSpace(n); n != "" {
					names = append(names, n)
				}
			}
		}
		for _, n := range names {
			if !exported[n] {
				t.Errorf("%s uses %s, which vendor/three.min.js does not export", f, n)
			}
		}
	}
}

// The racks' textures are WebP, served as such, and stay light: 150 kB for all of them.
func TestRackTextures(t *testing.T) {
	total := 0
	for _, name := range []string{"rack/bois.webp", "rack/carton.webp"} {
		w := getStatic(t, "/"+name)
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/webp" || !bytes.Equal(w.Body.Bytes(), embedded(t, name)) {
			t.Fatalf("%s: %d %v", name, w.Code, w.Header())
		}
		total += w.Body.Len()
	}
	if total >= 150000 {
		t.Fatalf("%d bytes of textures", total)
	}
}

// Every picture a model or the room names is embedded, as KTX2: the GPU keeps it compressed.
func TestShop3DTextures(t *testing.T) {
	models, err := fs.Glob(webFS, "web/shop3d/assets/*/*.glb")
	if err != nil || len(models) == 0 {
		t.Fatal(models, err)
	}
	for _, m := range models {
		glb := embedded(t, strings.TrimPrefix(m, "web/"))
		size := binary.LittleEndian.Uint32(glb[12:])
		var gltf struct {
			Images []struct{ URI string }
		}
		if err := json.Unmarshal(glb[20:20+size], &gltf); err != nil {
			t.Fatal(m, err)
		}
		for _, im := range gltf.Images {
			p := path.Join(path.Dir(m), im.URI)
			if _, err := fs.Stat(webFS, p); err != nil || path.Ext(p) != ".ktx2" {
				t.Errorf("%s names %s: %v", m, im.URI, err)
			}
		}
	}
	for _, set := range regexp.MustCompile(`pbr\(\w*,? ?"(\w+)"`).FindAllStringSubmatch(string(embedded(t, "shop3d/world.js")), -1) {
		for _, kind := range []string{"diffuse", "nor_gl", "rough"} {
			if _, err := fs.Stat(webFS, "web/shop3d/assets/tex/"+set[1]+"_"+kind+".ktx2"); err != nil {
				t.Error(err)
			}
		}
	}
	// three.js's transcoder compiles its bindings with new Function, which the CSP refuses: the
	// vendored copy is patched (tools/basis-no-eval.mjs).
	if bytes.Contains(embedded(t, "vendor/basis/basis_transcoder.js"), []byte("newFunc(Function")) {
		t.Error("vendor/basis/basis_transcoder.js still compiles code from text: run tools/basis-no-eval.mjs")
	}
	fs.WalkDir(webFS, "web/shop3d/assets", func(p string, _ fs.DirEntry, _ error) error {
		if path.Ext(p) == ".jpg" {
			t.Errorf("still JPEG: %s", p)
		}
		return nil
	})
}
