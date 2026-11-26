package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var obiSlots = make(chan struct{}, 3)

// japanReleases lists an album's Japanese releases, most pressed edition first, then by date:
// the pressings most likely to be photographed with their obi.
func japanReleases(d AlbumDetail) []string {
	var out []string
	for _, ed := range d.Editions {
		for _, p := range ed.pressed {
			for _, c := range strings.Split(p.country, ", ") {
				if c == "JP" || c == "Japan" {
					out = append(out, p.release)
					break
				}
			}
		}
	}
	return out
}

// obiArt is the Cover Art Archive path of the front of a Japanese pressing of the album
// (« release/<id>/<image> »), or "" when its first three Japanese releases have none. The answer,
// a miss included, is kept in obi.json; a failed read is not, so it is asked again next time.
// A rack shows a hundred records at once: three lookups at a time spare the metadata server and the archive.
func (a *App) obiArt(ctx context.Context, rg string) string {
	a.obiMu.Lock()
	art, known := a.obi[rg]
	a.obiMu.Unlock()
	if known || a.meta == nil {
		return art
	}
	select {
	case obiSlots <- struct{}{}:
		defer func() { <-obiSlots }()
	case <-ctx.Done():
		return ""
	}
	d, err := a.meta.Album(ctx, rg)
	if err != nil {
		return ""
	}
	ids := japanReleases(d)
	for _, id := range ids[:min(3, len(ids))] {
		front, err := releaseFront(ctx, id)
		if err != nil {
			log.Printf("disquaire, pressage japonais %s : %v", id, err)
			return ""
		}
		if front != "" {
			art = "release/" + url.PathEscape(id) + "/" + front
			break
		}
	}
	a.obiMu.Lock()
	defer a.obiMu.Unlock()
	a.obi[rg] = art
	if a.obiPath != "" {
		b, err := json.Marshal(a.obi)
		if err == nil {
			err = writeFileSync(a.obiPath, b)
		}
		if err != nil {
			log.Printf("disquaire, %s : %v", a.obiPath, err)
		}
	}
	return art
}

// releaseFront is the image id of a release's front on the Cover Art Archive, "" when it has none.
func releaseFront(ctx context.Context, release string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", coverArtBase+"/release/"+url.PathEscape(release), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Musaarr")
	req.Header.Set("Accept", "application/json")
	resp, err := coverClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var list struct {
		Images []struct {
			Front bool
			ID    json.Number // a number, or a string of digits in older answers
		}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&list); err != nil {
		return "", err
	}
	for _, im := range list.Images {
		if _, err := im.ID.Int64(); im.Front && err == nil {
			return im.ID.String(), nil
		}
	}
	return "", nil
}

// obiPause spaces warmCovers' trips to the archive.
var obiPause = time.Second

// stockWarm is how many records of the starting stock the background pass prepares, most listened
// first; the deeper stock waits for a page to show it, so an install makes no bulk calls.
var stockWarm = 2000

// warmList is what warmCovers prepares: the personal layer (this month's chart, the user's artists), then
// the first stockWarm records of the starting stock, in the shop's order.
func (a *App) warmList() []ShopRecord {
	st := a.shopNow()
	if st == nil {
		return nil
	}
	inBase := map[string]bool{}
	for _, r := range a.baseNow() {
		inBase[r.MBID] = true
	}
	var mine, base []ShopRecord
	for _, r := range st.recs {
		switch {
		case !inBase[r.MBID] || r.Month > 0 || r.Listens > 0: // the stock has no counts: the personal layer gave it one
			mine = append(mine, r)
		case len(base) < stockWarm:
			base = append(base, r)
		}
	}
	return append(mine, base...)
}

// warmCovers looks up, one album a second, the Japanese pressing of every record of warmList not yet
// known, and keeps its covers on disk, so the racks seldom wait on the archive. It ends by bringing the
// covers back under coverCap.
func (a *App) warmCovers(ctx context.Context) {
	st := a.shopNow()
	if st == nil {
		return
	}
	recs := a.warmList()
	for _, r := range recs {
		a.obiMu.Lock()
		_, known := a.obi[r.MBID]
		a.obiMu.Unlock()
		went := !known
		for _, art := range []string{st.art[r.MBID], a.obiArt(ctx, r.MBID)} {
			if _, err := os.Stat(a.coverPath(art)); art != "" && a.coverPath(art) != "" && err != nil {
				a.shopThumb(ctx, art) // a failure is only a cover fetched later, by the page
				went = true
			}
		}
		if !went {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(obiPause):
		}
	}
	if a.coverDir != "" {
		if err := pruneCovers(a.coverDir, coverCap); err != nil {
			log.Printf("shop, covers: %v", err)
		}
	}
}
