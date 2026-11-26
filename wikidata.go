package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Wikidata reads albums' genres on the Wikidata Query Service (CC0). The stock is filed by them when
// no Discogs dump is given: data.discogs.com refuses GitHub's runners.
type Wikidata struct {
	base string // the SPARQL endpoint
	hc   *http.Client
}

func NewWikidata(base string) *Wikidata {
	return &Wikidata{base, &http.Client{Timeout: 90 * time.Second}}
}

// wdBatch is how many albums one query asks for.
const wdBatch = 200

// wdGenres are an album's genres on Wikidata, in English, then its performers' genres.
type wdGenres struct{ album, artist []string }

// Genres reads the genres of albums by their Wikidata items ("Q123", as readGroup keeps them).
func (w *Wikidata) Genres(ctx context.Context, items []string) (map[string]wdGenres, error) {
	// The hint keeps the written order: left to itself, the service joins every English label on
	// Wikidata before the albums, and each query times out.
	query := `SELECT ?item ?kind ?label WHERE { hint:Query hint:optimizer "None" .
  VALUES ?item { wd:` + strings.Join(items, " wd:") + ` }
  { ?item wdt:P136 ?g . BIND("a" AS ?kind) } UNION { ?item wdt:P175/wdt:P136 ?g . BIND("p" AS ?kind) }
  ?g rdfs:label ?label . FILTER(LANG(?label) = "en") }`
	req, err := http.NewRequestWithContext(ctx, "POST", w.base, strings.NewReader(url.Values{"query": {query}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", lbUserAgent)
	resp, err := w.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
		if err != nil {
			secs = 60
		}
		return nil, lbBusy{time.Duration(secs) * time.Second}
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Wikidata: HTTP %d", resp.StatusCode)
	}
	var res struct {
		Results struct {
			Bindings []struct{ Item, Kind, Label struct{ Value string } }
		}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&res); err != nil {
		return nil, fmt.Errorf("Wikidata: %w", err)
	}
	out := map[string]wdGenres{}
	for _, b := range res.Results.Bindings {
		q := b.Item.Value[strings.LastIndex(b.Item.Value, "/")+1:]
		g := out[q]
		if b.Kind.Value == "a" {
			g.album = append(g.album, b.Label.Value)
		} else {
			g.artist = append(g.artist, b.Label.Value)
		}
		out[q] = g
	}
	return out, nil
}

// wikiBin files an album by the rack most of its genres go to, else most of its performers' genres,
// else « divers » when it has genres the racks do not know. The style is the longest genre of that rack.
func wikiBin(g wdGenres) (bin, style string) {
	for _, labels := range [][]string{g.album, g.artist} {
		votes := map[string]int{}
		for _, l := range labels {
			if b := genreBin(l); b != "" {
				votes[b]++
			}
		}
		for _, b := range shopBins {
			if votes[b] > votes[bin] {
				bin = b
			}
		}
		if bin == "" {
			continue
		}
		for _, l := range labels {
			if s := strings.TrimSuffix(strings.ToLower(l), " music"); genreBin(l) == bin && len(s) > len(style) {
				style = s
			}
		}
		return bin, style
	}
	if len(g.album) > 0 {
		return "divers", strings.ToLower(slices.Min(g.album))
	}
	return "", ""
}

// fileByWikidata files by their genres on Wikidata the candidates that have an item, wdBatch per
// query, one query a second; the others are left out. A query that still fails after two more tries
// leaves its albums out; the build stops once more than three, and more than a tenth, have failed.
func fileByWikidata(ctx context.Context, w *Wikidata, cands []stockCand) ([]stockCand, error) {
	var out []stockCand
	failed := 0
	for i := 0; i < len(cands); i += wdBatch {
		batch := cands[i:min(i+wdBatch, len(cands))]
		items := make([]string, len(batch))
		for k, c := range batch {
			items[k] = c.wd
		}
		var genres map[string]wdGenres
		if err := lbCall(ctx, func() (err error) { genres, err = w.Genres(ctx, items); return }); err != nil {
			if failed++; failed > max(3, (i/wdBatch+1)/10) || ctx.Err() != nil {
				return nil, fmt.Errorf("%d Wikidata queries failed, the last: %w", failed, err)
			}
			log.Printf("stock: %d albums left out, Wikidata: %v", len(batch), err)
			continue
		}
		for _, c := range batch {
			if c.rec.Bin, c.rec.Style = wikiBin(genres[c.wd]); c.rec.Bin != "" {
				out = append(out, c)
			}
		}
		if i/wdBatch%50 == 49 {
			log.Printf("stock: %d albums looked up on Wikidata of %d", i+len(batch), len(cands))
		}
	}
	log.Printf("stock: %d of %d albums filed by their genres on Wikidata", len(out), len(cands))
	return out, nil
}
