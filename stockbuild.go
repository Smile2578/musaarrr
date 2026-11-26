package main

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

// stockMargin is how many more albums than wanted are chosen before the ones without a cover go.
const stockMargin = 1.15

// stockWiki is how many albums, per album wanted, are looked up on Wikidata, most listened first.
const stockWiki = 4

// buildStock builds n albums from the dumps: the albums with a Discogs master (when its dump is given)
// or a Wikidata item, ranked by their listeners on ListenBrainz, filed by their genres, chosen by
// pickStock, and kept only with a cover.
func buildStock(ctx context.Context, mb, discogs io.Reader, l *Listens, w *Wikidata, n int, now time.Time) ([]ShopRecord, error) {
	cands, bad, err := readGroups(mb)
	if err != nil {
		return nil, fmt.Errorf("MusicBrainz: %w", err)
	}
	log.Printf("stock: %d MusicBrainz albums with a Discogs master or a Wikidata item (%d unreadable lines)", len(cands), bad)
	if err := rankStock(ctx, l, cands); err != nil {
		return nil, err
	}
	filed, err := fileStock(ctx, discogs, w, cands, n)
	if err != nil {
		return nil, err
	}
	covered, err := withCovers(ctx, l, pickStock(filed, int(float64(n)*stockMargin), stockWeights, now))
	if err != nil {
		return nil, err
	}
	var recs []ShopRecord
	for _, c := range pickStock(covered, n, stockWeights, now) {
		recs = append(recs, c.rec)
	}
	slices.SortStableFunc(recs, func(x, y ShopRecord) int { return cmp.Compare(y.Listens, x.Listens) })
	logStock(recs)
	return recs, nil
}

// fileStock gives the candidates their bin: by their Discogs master's styles when the dump is given,
// else by their genres on Wikidata for the stockWiki×n most listened. The others are left out.
func fileStock(ctx context.Context, discogs io.Reader, w *Wikidata, cands []stockCand, n int) ([]stockCand, error) {
	var filed, rest []stockCand
	if discogs != nil {
		want := map[int]bool{}
		for _, c := range cands {
			want[c.master] = true
		}
		masters, err := readMasters(discogs, want)
		if err != nil {
			return nil, fmt.Errorf("Discogs: %w", err)
		}
		for i, c := range cands {
			if m, ok := masters[c.master]; ok {
				cands[i].rec.Bin, cands[i].rec.Style, cands[i].rec.Year = m.bin, m.style, cmp.Or(c.rec.Year, m.year)
			}
		}
	}
	for _, c := range cands {
		switch {
		case c.rec.Bin != "":
			filed = append(filed, c)
		case w != nil && c.wd != "" && c.users >= stockMinUsers:
			rest = append(rest, c)
		}
	}
	log.Printf("stock: %d albums filed by their Discogs master", len(filed))
	slices.SortStableFunc(rest, byListeners)
	more, err := fileByWikidata(ctx, w, rest[:min(len(rest), stockWiki*n)])
	return append(filed, more...), err
}

// rankStock reads the listeners and listens of every candidate, a thousand per call.
func rankStock(ctx context.Context, l *Listens, cands []stockCand) error {
	for i := 0; i < len(cands); i += 1000 {
		batch := cands[i:min(i+1000, len(cands))]
		ids := make([]string, len(batch))
		for k, c := range batch {
			ids[k] = c.rec.MBID
		}
		var pop map[string][2]int64
		if err := lbCall(ctx, func() (err error) { pop, err = l.Popularity(ctx, ids); return }); err != nil {
			return fmt.Errorf("ListenBrainz, popularity: %w", err)
		}
		for k := range batch {
			p := pop[batch[k].rec.MBID]
			batch[k].users, batch[k].rec.Listens = p[0], p[1]
		}
		if i/1000%100 == 99 {
			log.Printf("stock: %d albums ranked of %d", i+len(batch), len(cands))
		}
	}
	return nil
}

// withCovers keeps the albums the Cover Art Archive has a front for, as ListenBrainz knows it.
func withCovers(ctx context.Context, l *Listens, cands []stockCand) ([]stockCand, error) {
	var out []stockCand
	for i := 0; i < len(cands); i += shopBatch {
		batch := cands[i:min(i+shopBatch, len(cands))]
		ids := make([]string, len(batch))
		for k, c := range batch {
			ids[k] = c.rec.MBID
		}
		var meta map[string]lbMeta
		if err := lbCall(ctx, func() (err error) { meta, err = l.GroupMeta(ctx, ids); return }); err != nil {
			return nil, fmt.Errorf("ListenBrainz, covers: %w", err)
		}
		for _, c := range batch {
			if meta[c.rec.MBID].ReleaseGroup.CaaID > 0 {
				out = append(out, c)
			}
		}
		if i/shopBatch%200 == 199 {
			log.Printf("stock: %d albums checked for a cover of %d", i+len(batch), len(cands))
		}
	}
	log.Printf("stock: %d of %d preselected albums have a cover", len(out), len(cands))
	return out, nil
}

// logStock writes what the CI's summary shows: the stock by bin and layer, and by decade.
func logStock(recs []ShopRecord) {
	bins, layers, decades := map[string]int{}, map[string]int{}, map[int]int{}
	for _, r := range recs {
		bins[r.Bin]++
		layers[cmp.Or(r.Layer, "canon")]++
		decades[r.Year/10*10]++
	}
	var b strings.Builder
	for _, id := range shopBins {
		fmt.Fprintf(&b, " %s %d", id, bins[id])
	}
	log.Printf("stock: %d albums;%s", len(recs), b.String())
	log.Printf("stock: layers %v", layers)
	b.Reset()
	for _, d := range slices.Sorted(maps.Keys(decades)) {
		fmt.Fprintf(&b, " %d:%d", d, decades[d])
	}
	log.Printf("stock: decades%s", b.String())
}

// stockFile is stock.json.gz, the stock the CI publishes: CC0 data only, each artist written once.
type stockFile struct {
	Version int               `json:"version"`
	Built   time.Time         `json:"built"`
	License string            `json:"license"`
	Sources map[string]string `json:"sources,omitempty"`
	Artists []stockArtist     `json:"artists"`
	Records []stockRecord     `json:"records"`
}

type stockArtist struct {
	Name string `json:"name"`
	MBID string `json:"mbid,omitempty"`
	Sort string `json:"sort,omitempty"`
}

// stockRecord is an album of the stock. The records come most listened first, but without their
// counts: the licence of ListenBrainz's popularity figures is not confirmed.
type stockRecord struct {
	MBID  string `json:"mbid"`
	Title string `json:"title"`
	A     int    `json:"a"` // the artist, by its place in Artists
	Year  int    `json:"year,omitempty"`
	Bin   string `json:"bin"`
	Style string `json:"style,omitempty"`
	Layer string `json:"layer,omitempty"`
}

// writeStock writes the stock, gzipped, through a temporary file.
func writeStock(path string, recs []ShopRecord, built time.Time, sources map[string]string) error {
	f := stockFile{Version: 1, Built: built.UTC(), License: "CC0-1.0", Sources: sources, Records: make([]stockRecord, len(recs))}
	index := map[stockArtist]int{}
	for i, r := range recs {
		a := stockArtist{r.Artist, r.ArtistMBID, r.Sort}
		if _, ok := index[a]; !ok {
			index[a] = len(f.Artists)
			f.Artists = append(f.Artists, a)
		}
		f.Records[i] = stockRecord{r.MBID, r.Title, index[a], r.Year, r.Bin, r.Style, r.Layer}
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err := json.NewEncoder(zw).Encode(f); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return writeFileSync(path, buf.Bytes())
}

// readStock reads a stock written by writeStock. It comes from the network: records with an id that
// is not an MBID, an unknown artist or an unknown bin are left out.
func readStock(r io.Reader) ([]ShopRecord, time.Time, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, time.Time{}, err
	}
	var f stockFile
	if err := json.NewDecoder(io.LimitReader(zr, 256<<20)).Decode(&f); err != nil {
		return nil, time.Time{}, err
	}
	if f.Version != 1 {
		return nil, time.Time{}, fmt.Errorf("stock version %d, this Musaarr reads version 1", f.Version)
	}
	recs := make([]ShopRecord, 0, len(f.Records))
	for _, s := range f.Records {
		id, ok := validMBID(s.MBID)
		if !ok || s.A < 0 || s.A >= len(f.Artists) || binNames[s.Bin] == "" {
			continue
		}
		a := f.Artists[s.A]
		if _, ok := validMBID(a.MBID); !ok {
			a.MBID = ""
		}
		recs = append(recs, ShopRecord{MBID: id, Title: s.Title, Artist: a.Name, ArtistMBID: a.MBID, Sort: a.Sort,
			Year: s.Year, Bin: s.Bin, Style: s.Style, Layer: s.Layer})
	}
	return recs, f.Built, nil
}

// runBuildStock is `musaarr -build-stock out.json.gz`, for the CI: the MusicBrainz dump of release
// groups on mbPath ("-" for the standard input), and the Discogs masters dump on discogsPath when given;
// without it, the albums are filed by their genres on Wikidata.
func runBuildStock(out, mbPath, discogsPath string, n int, sources string) error {
	mb := io.Reader(os.Stdin)
	if mbPath != "-" {
		f, err := os.Open(mbPath)
		if err != nil {
			return err
		}
		defer f.Close()
		mb = f
	}
	var discogs io.Reader
	if discogsPath != "" {
		df, err := os.Open(discogsPath)
		if err != nil {
			return err
		}
		defer df.Close()
		if discogs, err = gzip.NewReader(df); err != nil {
			return fmt.Errorf("%s: %w", discogsPath, err)
		}
	}
	l := NewListens(cmp.Or(os.Getenv("LISTENBRAINZ_URL"), "https://api.listenbrainz.org"), os.Getenv("LISTENBRAINZ_TOKEN"))
	w := NewWikidata(cmp.Or(os.Getenv("WIKIDATA_URL"), "https://query.wikidata.org/sparql"))
	recs, err := buildStock(context.Background(), mb, discogs, l, w, n, time.Now())
	if err != nil {
		return err
	}
	src := map[string]string{}
	for _, kv := range strings.Split(sources, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			src[k] = v
		}
	}
	return writeStock(out, recs, time.Now(), src)
}

// stockFresh is how old the kept stock may get before it is fetched again: the CI makes one a month.
const stockFresh = 7 * 24 * time.Hour

var stockClient = &http.Client{Timeout: 2 * time.Minute}

func (a *App) baseNow() []ShopRecord {
	a.shopMu.Lock()
	defer a.shopMu.Unlock()
	return a.base
}

// loadStock reads the kept stock; a missing or unreadable file only means no starting stock.
func (a *App) loadStock() {
	f, err := os.Open(a.stockPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var recs []ShopRecord
	if err == nil {
		defer f.Close()
		recs, _, err = readStock(f)
	}
	if err != nil {
		log.Printf("shop, %s: %v", a.stockPath, err)
		return
	}
	a.shopMu.Lock()
	a.base = recs
	a.shopMu.Unlock()
}

// fetchStock downloads the starting stock when the kept one is missing or a week old, and says
// whether it got a new one. A failed fetch keeps the last one and waits a day.
func (a *App) fetchStock(ctx context.Context, now time.Time) bool {
	if a.stockURL == "" || a.stockPath == "" || now.Sub(a.stockTried) < 24*time.Hour {
		return false
	}
	if fi, err := os.Stat(a.stockPath); err == nil && now.Sub(fi.ModTime()) < stockFresh {
		return false
	}
	b, err := getStock(ctx, a.stockURL)
	if err == nil {
		_, _, err = readStock(bytes.NewReader(b))
	}
	if err == nil {
		err = writeFileSync(a.stockPath, b)
	}
	if err != nil {
		a.stockTried = now
		log.Printf("shop, starting stock %s: %v (the last one stays)", a.stockURL, err)
		return false
	}
	a.loadStock()
	log.Printf("shop: starting stock of %d records", len(a.baseNow()))
	return true
}

func getStock(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", lbUserAgent)
	resp, err := stockClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}
