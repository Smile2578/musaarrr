package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// The shop (the Recherche home) is a record store: a starting stock of tens of thousands of albums
// (stockbuild.go), this month's chart and the user's artists, in bins by genre. Its stock is built once
// a day after the suggestion pass, kept in shop.json so a restart does not empty the store, and served
// pre-encoded.

var (
	shopTop      = 1000 // the site's most listened albums taken
	shopMonthTop = 100  // this month's most listened release groups, for the « Ce mois-ci » rack
	shopPage     = 100  // shortcut: the documented ceiling is not checked from here, 100 is safe
	shopBatch    = 50   // albums per metadata call
	shopPause    = time.Second
	shopRetry    = 10 * time.Second
	coverArtBase = "https://coverartarchive.org"
	coverClient  = &http.Client{Timeout: 20 * time.Second}
)

// shopBins is the order of the bins; the names are the page's.
var shopBins = []string{"jazz", "soul", "funk", "hiphop", "rock", "indie", "pop", "japon", "electro", "ambient", "chanson", "classique",
	"reggae", "afro", "latin", "monde", "blues", "folk", "country", "metal", "punk", "bo", "divers"}

type ShopRecord struct {
	MBID       string `json:"mbid"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	ArtistMBID string `json:"artistMbid,omitempty"`
	Year       int    `json:"year,omitempty"`
	Bin        string `json:"bin"`
	Style      string `json:"style,omitempty"` // the tag that chose the bin: "hard bop"
	Listens    int64  `json:"listens,omitempty"`
	Cover      string `json:"cover,omitempty"`
	Plex       bool   `json:"plex,omitempty"`  // in Plex when the stock was built
	Sort       string `json:"sort,omitempty"`  // MusicBrainz's sort name of an artist not written in Latin letters: « Takanaka, Masayoshi »
	Month      int    `json:"month,omitempty"` // rank in this month's sitewide chart, 1 the most listened
	Layer      string `json:"layer,omitempty"` // "new": out in the last 90 days, "deep": an artist's 3rd to 8th album; else the canon
	art        string // "release/{mbid}/{id}" on the Cover Art Archive
	key        string // recKey, worked out once while the racks are built
}

type shopStock struct {
	lib   Lib
	art   map[string]string
	built time.Time
	recs  []ShopRecord // filed in racks on each ask, against the Plex of the moment
}

// shopFile is shop.json: the records and where their covers are.
type shopFile struct {
	Built   time.Time
	Records []ShopRecord
	Art     map[string]string
}

func (a *App) shopNow() *shopStock {
	a.shopMu.Lock()
	defer a.shopMu.Unlock()
	return a.shop
}

// openShop loads the last stock from dir; a missing or unreadable file only means an empty store
// until the next build.
func (a *App) openShop(dir string) {
	if err := os.MkdirAll(filepath.Join(dir, "covers"), 0o700); err != nil {
		log.Printf("disquaire, pochettes : %v (relayées sans être gardées)", err)
	} else {
		a.coverDir = filepath.Join(dir, "covers")
		a.loadFacts()
	}
	a.obiPath = filepath.Join(dir, "obi.json")
	if b, err := os.ReadFile(a.obiPath); err == nil {
		if err := json.Unmarshal(b, &a.obi); err != nil {
			log.Printf("disquaire, %s : %v", a.obiPath, err)
		}
	}
	a.stockPath = filepath.Join(dir, "stock.json.gz")
	a.loadStock()
	a.shopPath = filepath.Join(dir, "shop.json")
	b, err := os.ReadFile(a.shopPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var f shopFile
	if err == nil {
		err = json.Unmarshal(b, &f)
	}
	if err != nil {
		log.Printf("disquaire, %s : %v (reconstruit au prochain passage)", a.shopPath, err)
		return
	}
	for i := range f.Records {
		f.Records[i].art = f.Art[f.Records[i].MBID]
	}
	if err := a.setShop(f.Records, f.Built, false); err != nil {
		log.Printf("disquaire : %v", err)
	}
}

func (a *App) setShop(recs []ShopRecord, built time.Time, save bool) error {
	n := map[string]int{}
	art := map[string]string{}
	for _, r := range recs {
		n[r.Bin]++
		if r.art != "" {
			art[r.MBID] = r.art
		}
	}
	type bin struct {
		ID string `json:"id"`
		N  int    `json:"n"`
	}
	bins := make([]bin, len(shopBins))
	for i, id := range shopBins {
		bins[i] = bin{id, n[id]}
	}
	lib, err := encodeJSON(map[string]any{"records": recs, "bins": bins, "built": built})
	if err != nil {
		return err
	}
	a.shopMu.Lock()
	a.shop = &shopStock{lib, art, built, recs}
	a.shopMu.Unlock()
	if !save || a.shopPath == "" {
		return nil
	}
	b, err := json.Marshal(shopFile{built, recs, art})
	if err == nil {
		err = writeFileSync(a.shopPath, b)
	}
	return err
}

// buildShop files the starting stock (stockbuild.go) with the personal layer: this month's chart
// and the user's artists' albums, whose genre, date and type ListenBrainz gives. Without a starting
// stock, the site's most listened albums stand in for it. Any call that still fails after two more
// tries leaves the last stock in place.
func (a *App) buildShop(ctx context.Context) error {
	base := a.baseNow()
	if a.listens == nil && base == nil {
		return nil
	}
	c := &shopCands{byID: map[string]*ShopRecord{}}
	for _, r := range base {
		r.Cover, r.art = "/api/shop/"+r.MBID+"/cover", "release-group/"+r.MBID+"/front"
		c.add(r)
	}
	inBase := len(c.ids)
	if a.listens != nil {
		if err := a.personalLayer(ctx, c, base == nil); err != nil {
			return err
		}
	}
	recs, err := a.fileRecords(ctx, c, inBase)
	if err != nil {
		return err
	}
	a.sortNames(ctx, recs[inBase:])
	for i, r := range recs {
		recs[i].Plex = a.plexKey(Album{Title: r.Title, ArtistName: r.Artist, ArtistMBID: r.ArtistMBID}) != ""
	}
	slices.SortStableFunc(recs, func(x, y ShopRecord) int { return cmp.Compare(y.Listens, x.Listens) })
	if err := a.setShop(recs, time.Now().UTC().Truncate(time.Second), true); err != nil {
		log.Printf("shop, saving: %v", err) // the stock is served anyway
	}
	log.Printf("shop: %d albums, %d of them from the starting stock", len(recs), inBase)
	return nil
}

// shopCands gathers the shop's records, each once, in the order they come.
type shopCands struct {
	byID map[string]*ShopRecord
	ids  []string
}

func (c *shopCands) add(r ShopRecord) {
	if _, ok := validMBID(r.MBID); !ok {
		return
	}
	if old := c.byID[r.MBID]; old != nil {
		old.Listens = max(old.Listens, r.Listens)
		old.ArtistMBID = cmp.Or(old.ArtistMBID, r.ArtistMBID) // Lidarr's artist opens the artist page
		return
	}
	c.byID[r.MBID] = &r
	c.ids = append(c.ids, r.MBID)
}

// personalLayer adds this month's chart, the user's artists' albums from the last suggestion pass, and
// with top the site's most listened albums.
func (a *App) personalLayer(ctx context.Context, c *shopCands, top bool) error {
	fromLB := func(g lbGroup) ShopRecord {
		r := ShopRecord{MBID: g.MBID, Title: g.Name, Artist: g.Artist, Listens: g.Listens, art: caaPath(g.CaaRelease, g.CaaID)}
		if len(g.ArtistMBIDs) > 0 {
			r.ArtistMBID = g.ArtistMBIDs[0]
		}
		return r
	}
	for off := 0; top && off < shopTop; off += shopPage {
		var page []lbGroup
		if err := lbCall(ctx, func() (err error) {
			page, err = a.listens.Sitewide(ctx, "all_time", off, min(shopPage, shopTop-off))
			return
		}); err != nil {
			return fmt.Errorf("disquaire, classement : %w", err)
		}
		for _, g := range page {
			c.add(fromLB(g))
		}
		top = len(page) == shopPage
	}
	month, err := a.monthChart(ctx)
	if err != nil {
		log.Printf("disquaire, classement du mois : %v (pas de bac « Ce mois-ci » cette fois)", err)
	}
	for i, g := range month {
		r := fromLB(g)
		r.Listens = 0 // this month's counts are not the all-time ones the shop is sorted by
		c.add(r)
		if r := c.byID[g.MBID]; r != nil && r.Month == 0 {
			r.Month = i + 1
		}
	}
	a.sugMu.Lock()
	mine := a.mine
	a.sugMu.Unlock()
	for _, s := range mine {
		c.add(ShopRecord{MBID: s.al.MBID, Title: s.al.Title, Artist: s.al.ArtistName, ArtistMBID: s.al.ArtistMBID, Year: s.al.Year, Listens: s.listens})
	}
	return nil
}

// fileRecords keeps the albums: the first inBase records, from the starting stock, as they are; the
// others with their genre, date and cover read on ListenBrainz.
func (a *App) fileRecords(ctx context.Context, c *shopCands, inBase int) ([]ShopRecord, error) {
	meta := map[string]lbMeta{}
	for i := inBase; i < len(c.ids); i += shopBatch {
		var m map[string]lbMeta
		if err := lbCall(ctx, func() (err error) { m, err = a.listens.GroupMeta(ctx, c.ids[i:min(i+shopBatch, len(c.ids))]); return }); err != nil {
			return nil, fmt.Errorf("disquaire, métadonnées : %w", err)
		}
		maps.Copy(meta, m)
	}
	recs := []ShopRecord{}
	for k, id := range c.ids {
		r, m := c.byID[id], meta[id]
		if k < inBase {
			recs = append(recs, *r)
			continue
		}
		if _, ok := meta[id]; !ok || (m.ReleaseGroup.Type != "Album" && m.ReleaseGroup.Type != "") {
			continue // singles, EP, broadcasts, and albums ListenBrainz no longer knows
		}
		if y := yearOf(m.ReleaseGroup.Date); y > 0 {
			r.Year = y
		}
		r.art = cmp.Or(r.art, caaPath(m.ReleaseGroup.CaaRelease, m.ReleaseGroup.CaaID))
		if r.art != "" {
			r.Cover = "/api/shop/" + r.MBID + "/cover"
		}
		if r.Bin, r.Style = binFor(m.Tag.ReleaseGroup); r.Bin == "divers" {
			r.Bin, r.Style = binFor(m.Tag.Artist)
		}
		r.Artist = cmp.Or(r.Artist, m.Artist.Name)
		if r.ArtistMBID == "" && len(m.Artist.Artists) > 0 {
			r.ArtistMBID = m.Artist.Artists[0].MBID
		}
		recs = append(recs, *r)
	}
	return recs, nil
}

// sortNames reads, once per artist, the MusicBrainz sort name of the artists not written in Latin
// letters, so « 高中正義 » is filed under T. The starting stock carries its own.
func (a *App) sortNames(ctx context.Context, recs []ShopRecord) {
	sorts := map[string]string{}
	for i, r := range recs {
		if a.meta == nil || r.ArtistMBID == "" || !otherScript(r.Artist) {
			continue
		}
		if _, ok := sorts[r.ArtistMBID]; !ok {
			var ar struct{ SortName string }
			if err := a.meta.artist(ctx, r.ArtistMBID, &ar); err != nil {
				log.Printf("disquaire, nom de classement de %s : %v", r.Artist, err) // filed under « # » meanwhile
			}
			sorts[r.ArtistMBID] = ar.SortName
		}
		recs[i].Sort = sorts[r.ArtistMBID]
	}
}

// monthChart is this month's most listened albums on ListenBrainz, or last month's while this
// month's statistics are not computed yet (its first days).
func (a *App) monthChart(ctx context.Context) ([]lbGroup, error) {
	var groups []lbGroup
	for _, rng := range []string{"this_month", "month"} {
		err := lbCall(ctx, func() (err error) { groups, err = a.listens.Sitewide(ctx, rng, 0, shopMonthTop); return })
		if err != nil || len(groups) > 0 {
			return groups, err
		}
	}
	return nil, nil
}

// lbCall paces the calls (ListenBrainz asks for one a second at most) and tries a failed call twice
// more, after the wait a 429 gives.
func lbCall(ctx context.Context, call func() error) error {
	var err error
	for try := range 3 {
		wait := shopPause
		if busy := (lbBusy{}); errors.As(err, &busy) {
			wait = busy.wait
		} else if try > 0 {
			wait = shopRetry
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		if err = call(); err == nil {
			return nil
		}
	}
	return err
}

func caaPath(release string, id int64) string {
	if _, ok := validMBID(release); !ok || id <= 0 {
		return ""
	}
	return "release/" + release + "/" + strconv.FormatInt(id, 10)
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}

// genreBins maps the words of a genre tag, or of a Discogs style or genre, to a bin.
var genreBins = func() map[string]string {
	words := map[string][]string{
		"jazz":   {"jazz", "bop", "bebop", "hard bop", "post bop", "swing", "big band", "fusion", "ragtime", "dixieland", "modal"},
		"soul":   {"soul", "r&b", "rnb", "rhythm and blues", "rhythm & blues", "motown", "gospel", "quiet storm", "new jack swing", "doo wop"},
		"funk":   {"funk", "disco", "boogie", "p funk"},
		"hiphop": {"hip hop", "hiphop", "rap", "trap", "boom bap", "grime", "drill", "turntablism", "g funk"},
		"rock":   {"rock", "garage", "psychedelia", "krautrock", "rockabilly", "rock and roll", "rock & roll", "surf", "prog", "glam"},
		"indie": {"indie", "indie rock", "indie pop", "alternative", "alternative rock", "grunge", "shoegaze", "post punk", "new wave",
			"no wave", "britpop", "emo", "dream pop", "lo fi", "noise pop", "post rock", "math rock"},
		"punk":  {"punk", "punk rock", "hardcore", "hardcore punk", "post hardcore", "pop punk", "screamo", "crust", "oi", "riot grrrl"},
		"metal": {"metal", "metalcore", "deathcore", "grindcore", "thrash", "doom", "sludge", "djent"},
		"pop":   {"pop", "synthpop", "electropop", "europop", "bubblegum", "adult contemporary", "easy listening", "schlager"},
		"japon": {"city pop", "j pop", "jpop", "j rock", "kayokyoku", "kayōkyoku", "enka", "shibuya kei", "visual kei", "anison"},
		"electro": {"electronic", "electronica", "electro", "house", "techno", "trance", "idm", "downtempo", "trip hop",
			"drum and bass", "drum & bass", "dnb", "jungle", "dubstep", "uk garage", "breakbeat", "breaks", "big beat", "edm", "dance",
			"eurodance", "synthwave", "chillwave", "vaporwave", "glitch", "industrial", "electroclash", "minimal"},
		"ambient": {"ambient", "drone", "experimental", "noise", "musique concrete", "musique concrète", "sound collage", "new age",
			"berlin school", "avant garde", "avantgarde"},
		"chanson": {"chanson", "variété", "variete", "french pop", "pop française", "yé yé", "ye ye"},
		"classique": {"classical", "classique", "baroque", "opera", "orchestral", "chamber music", "choral", "symphony", "minimalism",
			"renaissance", "medieval", "impressionism", "romanticism", "romantic", "contemporary"},
		"reggae": {"reggae", "dub", "ska", "rocksteady", "dancehall", "lovers rock", "ragga"},
		"afro": {"afrobeat", "afrobeats", "afro", "afropop", "afro pop", "highlife", "soukous", "mbalax", "african", "rai", "gnawa",
			"juju", "ethio jazz", "zouk", "kompa", "biguine", "calypso", "soca"},
		"latin": {"latin", "latin pop", "bossa nova", "samba", "mpb", "tropicalia", "tropicália", "salsa", "cumbia", "reggaeton", "bachata",
			"merengue", "tango", "bolero", "son cubano", "mambo", "brazilian", "forró", "axé", "baile funk", "flamenco", "afro cuban"},
		"monde": {"world", "folk world & country", "fado", "klezmer", "indian classical", "hindustani", "carnatic", "qawwali", "rebetiko",
			"gamelan", "balkan", "chaabi", "arabic", "turkish", "persian", "polka", "volksmusik", "throat singing", "bhangra"},
		"blues":   {"blues", "boogie woogie"},
		"folk":    {"folk", "singer songwriter", "celtic"},
		"country": {"country", "country rock", "alt country", "americana", "bluegrass", "honky tonk", "western swing", "outlaw country"},
		"bo":      {"soundtrack", "film score", "score", "film music", "video game music", "musical", "show tunes", "bande originale", "stage & screen"},
	}
	m := map[string]string{}
	for bin, ws := range words {
		for _, w := range ws {
			m[w] = bin
		}
	}
	return m
}()

// binFor is the bin of an album's first genre tag by count; "divers" when no tag is a genre.
func binFor(tags []lbTag) (bin, style string) {
	sorted := slices.Clone(tags)
	slices.SortStableFunc(sorted, func(x, y lbTag) int { return cmp.Compare(y.Count, x.Count) })
	for _, t := range sorted {
		if b := genreBin(t.Tag); b != "" {
			return b, strings.ToLower(strings.TrimSpace(t.Tag))
		}
	}
	return "divers", ""
}

// genreBin reads a tag by its longest known phrase; at equal length the last one wins, as in
// "jazz rap" (rap) or "blues rock" (rock).
func genreBin(tag string) string {
	w := strings.Fields(strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r == '/' || r == ',' { // Discogs: « Folk, World, & Country »
			return ' '
		}
		return unicode.ToLower(r)
	}, tag))
	bin, best := "", 0
	for i := range w {
		for j := i + 1; j <= len(w) && j-i <= 4; j++ {
			if b, ok := genreBins[strings.Join(w[i:j], " ")]; ok && j-i >= best {
				bin, best = b, j-i
			}
		}
	}
	return bin
}

func (a *App) getShop(w http.ResponseWriter, r *http.Request) {
	st := a.shopNow()
	switch {
	case st != nil:
		serveEncoded(w, r, st.lib)
	case a.listens == nil && a.stockURL == "" && a.baseNow() == nil:
		fail(w, 503, "shop.noSource", nil)
	default:
		fail(w, 503, "shop.filling", nil)
	}
}

// getShopRacks files the shop in racks (racks.go); ?hide=1 leaves out what is already in Plex: in it when
// the stock was built, or in the Plex of the moment. For the 3D shop, ?summary=1 gives the racks and
// their crates without the records, and ?rack=<id> one rack with its records.
func (a *App) getShopRacks(w http.ResponseWriter, r *http.Request) {
	st := a.shopNow()
	if st == nil {
		a.getShop(w, r) // the same 503 messages
		return
	}
	q := r.URL.Query()
	recs := st.recs
	if id := q.Get("rack"); id != "" {
		recs = slices.DeleteFunc(slices.Clone(recs), func(rec ShopRecord) bool { // the others' sorting is not needed
			if id == "mois" {
				return rec.Month == 0
			}
			return rec.Bin != id
		})
	}
	racks := a.racksOf(recs, q.Get("hide") == "1")
	if id := q.Get("rack"); id != "" {
		i := slices.IndexFunc(racks, func(rk Rack) bool { return rk.ID == id })
		if i < 0 {
			fail(w, 404, "shop.noRack", nil)
			return
		}
		serveJSON(w, r, racks[i])
		return
	}
	if q.Get("summary") == "1" {
		for _, rk := range racks {
			for i := range rk.Crates {
				rk.Crates[i].Items = nil
			}
		}
	}
	serveJSON(w, r, map[string]any{"racks": racks, "built": st.built})
}

// racksOf files records in racks, marks the records whose Japanese pressing is known and gives them
// the facts of the cover the page shows: that pressing's front, else the record's own.
func (a *App) racksOf(recs []ShopRecord, hide bool) []Rack {
	owned := func(rec ShopRecord) bool {
		return rec.Plex || (a.svc.plex != nil && a.svc.plex.Find(rec.Artist, rec.Title) != "")
	}
	racks := buildRacks(recs, owned, hide)
	a.obiMu.Lock()
	defer a.obiMu.Unlock()
	a.factsMu.Lock()
	defer a.factsMu.Unlock()
	for _, rk := range racks {
		for _, c := range rk.Crates {
			for i, it := range c.Items {
				if it.Record == nil {
					continue
				}
				jp := a.obi[it.Record.MBID]
				f, ok := a.facts[jp]
				if !ok {
					f = a.facts[it.Record.art]
				}
				c.Items[i].JP, c.Items[i].Color, c.Items[i].Thumb = jp != "", f.Color, f.Thumb
			}
		}
	}
	return racks
}

func serveJSON(w http.ResponseWriter, r *http.Request, v any) {
	lib, err := encodeJSON(v)
	if err != nil {
		fail(w, 500, "shop.unreadable", nil)
		return
	}
	serveEncoded(w, r, lib)
}

// shopDue says the stock is missing or a day old. 23 h, so a daily pass that starts a little
// earlier than the last one still rebuilds it.
func (a *App) shopDue(now time.Time) bool {
	s := a.shopNow()
	return s == nil || now.Sub(s.built) >= 23*time.Hour
}
