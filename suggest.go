package main

import (
	"cmp"
	"context"
	"log"
	"net/http"
	"slices"
	"time"
)

// Suggestions are the "incontournables": for each artist the user has in Plex, its most listened
// studio albums that Plex lacks. One pass a day, in the background: it asks Lidarr, the
// metadata server and ListenBrainz about every artist.

const (
	suggestPerArtist = 2
	suggestShown     = 12
	suggestArtists   = 80 // shortcut: the artists with the most albums in Plex, raise it if the library grows
)

// suggestPause spaces the pass's artists: ListenBrainz allows 30 calls per 10 s, and Lidarr
// times out under a burst of lookups.
var suggestPause = time.Second

type suggestion struct {
	al      Album
	listens int64
}

func (a *App) refreshSuggestions(ctx context.Context) {
	if a.svc.plex == nil || a.listens == nil || a.meta == nil {
		return
	}
	type owned struct {
		name string
		n    int
	}
	var artists []owned
	for name, n := range a.svc.plex.Artists() {
		artists = append(artists, owned{name, n})
	}
	slices.SortFunc(artists, func(x, y owned) int { return cmp.Or(cmp.Compare(y.n, x.n), cmp.Compare(x.name, y.name)) })
	artists = artists[:min(len(artists), suggestArtists)]
	var rounds [suggestPerArtist][]suggestion // every artist's best first, then every artist's second
	var mine []suggestion
	for i, ar := range artists {
		if ctx.Err() != nil {
			return
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(suggestPause):
			}
		}
		albums := a.studioAlbums(ctx, ar.name)
		mine = append(mine, albums...)
		missing := slices.DeleteFunc(slices.Clone(albums), func(s suggestion) bool {
			return a.svc.plex.Find(ar.name, s.al.Title) != "" || a.svc.plex.Find(s.al.ArtistName, s.al.Title) != ""
		})
		for i := 0; i < len(missing) && i < suggestPerArtist; i++ {
			rounds[i] = append(rounds[i], missing[i])
		}
	}
	all := []suggestion{}
	for _, r := range rounds {
		slices.SortStableFunc(r, func(x, y suggestion) int { return cmp.Compare(y.listens, x.listens) })
		all = append(all, r...)
	}
	a.sugMu.Lock()
	a.sug, a.mine = all, mine
	a.sugMu.Unlock()
	log.Printf("suggestions: %d albums for %d artists", len(all), len(artists))
}

// studioAlbums lists an artist's studio albums that ListenBrainz knows, most listened first.
func (a *App) studioAlbums(ctx context.Context, name string) []suggestion {
	mbid := a.artistMBID(ctx, name)
	if mbid == "" {
		return nil
	}
	p, err := a.discography(ctx, mbid)
	if err != nil {
		log.Printf("suggestions, discography of %s: %v", name, err)
		return nil
	}
	counts, err := a.listens.TopAlbums(ctx, mbid)
	if err != nil {
		log.Printf("suggestions, listens of %s: %v", name, err)
		return nil
	}
	var out []suggestion
	for _, al := range p.albums {
		if rank(al) != 0 || counts[al.MBID] == 0 {
			continue
		}
		out = append(out, suggestion{al, counts[al.MBID]})
	}
	slices.SortStableFunc(out, func(x, y suggestion) int { return cmp.Compare(y.listens, x.listens) })
	return out
}

// artistMBID finds the artist Plex names, through Lidarr's lookup; "" when no artist has exactly that name,
// as its name or as one of its other names.
func (a *App) artistMBID(ctx context.Context, name string) string {
	v, err := a.remember("artist:"+name, func() (any, error) { return a.svc.lidarr.ArtistLookup(ctx, name) })
	if err != nil {
		log.Printf("suggestions, artist %s: %v", name, err)
		return ""
	}
	want := words(name)
	found := v.([]Artist)
	for _, ar := range found {
		if slices.Equal(words(ar.Name), want) {
			return ar.MBID
		}
	}
	if len(found) == 0 { // a renamed artist: Plex may keep the former name ("Kanye West" for Ye)
		return ""
	}
	for _, n := range a.svc.artistNames(ctx, found[0].MBID) {
		if slices.Equal(words(n), want) {
			return found[0].MBID
		}
	}
	return ""
}

func (a *App) suggestions(w http.ResponseWriter, r *http.Request) {
	a.sugMu.Lock()
	sug, ready := a.sug, a.sug != nil
	a.sugMu.Unlock()
	items := []Item{}
	for _, s := range sug {
		it := a.item(s.al)
		if it.Status != "" { // asked for, or in Plex, since the pass
			continue
		}
		it.Cover, it.Listens = "/api/cover/"+s.al.MBID, s.listens
		if items = append(items, it); len(items) == suggestShown {
			break
		}
	}
	writeJSON(w, 200, map[string]any{"ready": ready, "results": items})
}

// suggestDaily runs the suggestion pass now, then once a day, and every 10 minutes while Plex
// has not answered yet. It rebuilds the shop when its stock is missing (tried each hour) or a
// day old, and once Plex has answered if the stock was built without the user's artists. A restart
// or a Plex outage leaves a fresh stock alone.
func (a *App) suggestDaily(ctx context.Context) {
	var sugAt time.Time
	plexSeen := false
	shopPlex := a.shopNow() != nil // a stock kept on disk was built with Plex's artists
	for {
		now := time.Now()
		if !plexSeen || now.Sub(sugAt) >= 24*time.Hour {
			a.refreshSuggestions(ctx)
			sugAt, plexSeen = now, a.svc.plex == nil || len(a.svc.plex.Artists()) > 0
		}
		fresh := a.fetchStock(ctx, now) // a new starting stock, once a month
		if (a.listens != nil || a.baseNow() != nil) && (fresh || a.shopDue(now) || (plexSeen && !shopPlex)) {
			if err := a.buildShop(ctx); err != nil {
				log.Printf("%v (the previous stock stays)", err)
			} else {
				shopPlex = plexSeen
			}
		}
		wait := time.Hour
		if !plexSeen {
			wait = 10 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
