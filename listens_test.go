package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	lateRegistration = "c563d738-3841-31ce-b29d-ec6b40fd1e7b"
	testListensToken = "lb-token"
)

// newTestListens answers like ListenBrainz for Ye, after wait; like it, it wants a user token.
func newTestListens(t *testing.T, status int, wait time.Duration) *Listens {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(wait)
		if r.URL.Path != "/1/popularity/top-release-groups-for-artist/"+yeMBID {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Token "+testListensToken {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(status)
		w.Write([]byte(`[{"artist":{"name":"Ye"},"release_group":{"name":"My Beautiful Dark Twisted Fantasy","type":"Album"},
		 "release_group_mbid":"0da340a0-6ad7-4fc2-a272-6f94393a7831","total_listen_count":6012345,"total_user_count":162000},
		 {"release_group":{"name":"Late Registration"},"release_group_mbid":"` + lateRegistration + `","total_listen_count":3300000,"total_user_count":90000},
		 {"release_group_mbid":"11111111-2222-3333-4444-555555555555","total_listen_count":2000000},
		 {"release_group_mbid":"7d4bba1d-4759-4d22-89f1-c0cc71f5736e","total_listen_count":null}]`))
	}))
	t.Cleanup(srv.Close)
	return NewListens(srv.URL, testListensToken)
}

func TestTopAlbums(t *testing.T) {
	got, err := newTestListens(t, 200, 0).TopAlbums(ctx, yeMBID)
	if err != nil || len(got) != 3 || got[lateRegistration] != 3300000 || got["0da340a0-6ad7-4fc2-a272-6f94393a7831"] != 6012345 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := newTestListens(t, 500, 0).TopAlbums(ctx, yeMBID); err == nil {
		t.Fatal("500 accepted")
	}
	if _, err := NewListens(newTestListens(t, 200, 0).base, "").TopAlbums(ctx, yeMBID); err == nil {
		t.Fatal("no token accepted")
	}
}

// ListenBrainz answers only with a user token: without one, no listens and no incontournables.
func TestListensFor(t *testing.T) {
	c, err := LoadConfig(env(map[string]string{"LIDARR_API_KEY": "k", "ALLOWED_LOGIN": "a@b", "LISTENBRAINZ_TOKEN": "t"}))
	if l := ListensFor(c); err != nil || l == nil || l.token != "t" || l.base != "https://api.listenbrainz.org" {
		t.Fatalf("with a token: %+v %v", l, err)
	}
	if l := ListensFor(Config{ListenBrainzURL: "https://api.listenbrainz.org"}); l != nil {
		t.Fatalf("without a token: %+v", l)
	}
	if l := ListensFor(Config{ListenBrainzURL: "off", ListenBrainzToken: "t"}); l != nil {
		t.Fatalf("off: %+v", l)
	}
}

func artistListens(t *testing.T, l *Listens) map[string]int64 {
	t.Helper()
	_, a, h := newTestApp(t)
	a.meta = newTestMetadata(t)
	a.listens = l
	w := call(h, "GET", "/api/artists/"+yeMBID, "")
	var res struct{ Results []Item }
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || len(res.Results) != 3 {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
	out := map[string]int64{}
	for _, it := range res.Results {
		out[it.Title] = it.Listens
	}
	return out
}

func TestArtistPageListens(t *testing.T) {
	got := artistListens(t, newTestListens(t, 200, 0))
	if got["Late Registration"] != 3300000 || got["Yeezus"] != 2000000 || got["Power"] != 0 {
		t.Fatalf("got %v", got)
	}
}

// ListenBrainz down or slow: the albums show anyway, without their listens.
func TestArtistPageWithoutListens(t *testing.T) {
	if got := artistListens(t, newTestListens(t, 503, 0)); got["Late Registration"] != 0 {
		t.Fatalf("down: %v", got)
	}
	defer func(w time.Duration) { listensWait = w }(listensWait)
	listensWait = 50 * time.Millisecond
	start := time.Now()
	if got := artistListens(t, newTestListens(t, 200, time.Second)); got["Late Registration"] != 0 || time.Since(start) > 900*time.Millisecond {
		t.Fatalf("slow: %v after %v", got, time.Since(start))
	}
}

func TestPopularity(t *testing.T) {
	var body struct {
		MBIDs []string `json:"release_group_mbids"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/1/popularity/release-group" || !strings.Contains(r.UserAgent(), "github.com/smile2578/musaarrr") {
			w.WriteHeader(400)
			return
		}
		json.NewDecoder(r.Body).Decode(&body)
		fmt.Fprintf(w, `[{"release_group_mbid":%q,"total_listen_count":1462621,"total_user_count":25280},
			{"release_group_mbid":%q,"total_listen_count":null,"total_user_count":null}]`, body.MBIDs[0], body.MBIDs[1])
	}))
	t.Cleanup(srv.Close)
	got, err := NewListens(srv.URL, "").Popularity(context.Background(), []string{shopID(1), shopID(2)})
	if err != nil {
		t.Fatal(err)
	}
	if len(body.MBIDs) != 2 || len(got) != 1 || got[shopID(1)] != [2]int64{25280, 1462621} {
		t.Errorf("Popularity : %v (envoyé %v)", got, body.MBIDs)
	}
}

func TestLBCallWaitsWhenBusy(t *testing.T) {
	quickShop(t, 10, 10)
	shopRetry = time.Hour // a 429 says how long to wait: the usual pause between tries does not apply
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("X-RateLimit-Reset-In", "0.05")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	l := NewListens(srv.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := lbCall(ctx, func() error { _, err := l.Popularity(ctx, []string{shopID(1)}); return err })
	if err != nil || calls.Load() != 2 {
		t.Errorf("lbCall : %v après %d appels", err, calls.Load())
	}
}
