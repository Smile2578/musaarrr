package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// asset is one embedded file, read once at startup with its gzip copy and validators.
type asset struct {
	body, gz    []byte
	etag, gzTag string
	ctype       string
}

// webFiles compresses the embedded files once per process, not once per App.
var webFiles = sync.OnceValue(func() http.Handler {
	static, _ := fs.Sub(webFS, "web")
	return newStatic(static)
})

var types = map[string]string{".webmanifest": "application/manifest+json", ".woff2": "font/woff2", ".glb": "model/gltf-binary", ".ktx2": "image/ktx2"}

// newStatic serves the embedded web files. Every open revalidates (no-cache), but an unchanged
// file answers 304 by its ETag, and text goes out gzipped: the phone fetches little over Tailscale.
func newStatic(fsys fs.FS) http.Handler {
	assets := map[string]*asset{}
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		tag := hex.EncodeToString(sum[:8])
		a := &asset{body: b, etag: `"` + tag + `"`, gzTag: `"` + tag + `-gz"`, ctype: types[path.Ext(p)]}
		if a.ctype == "" {
			a.ctype = mime.TypeByExtension(path.Ext(p))
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		zw.Write(b)
		zw.Close()
		if buf.Len() < len(b)*9/10 { // woff2 and png are already compressed
			a.gz = buf.Bytes()
		}
		assets["/"+p] = a
		return nil
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" {
			p = "/index.html"
		}
		a := assets[p]
		if a == nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-cache")
		h.Set("Content-Type", a.ctype)
		body, tag := a.body, a.etag
		if a.gz != nil {
			h.Set("Vary", "Accept-Encoding")
			if acceptsGzip(r.Header.Get("Accept-Encoding")) {
				body, tag = a.gz, a.gzTag
				h.Set("Content-Encoding", "gzip")
			}
		}
		h.Set("ETag", tag)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	})
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, q, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			return strings.ReplaceAll(strings.TrimSpace(q), " ", "") != "q=0"
		}
	}
	return false
}
