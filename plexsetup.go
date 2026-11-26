package main

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Signing in to Plex with a PIN: Musaarr asks plex.tv for a code, the admin approves it on
// plex.tv, and Musaarr lists the servers that account reaches, each address tried from here.
// Tokens stay on the server: the page sees names and addresses, and the account's own token is
// dropped once the servers are listed. Only the chosen server's token is kept.

const plexPinLife = 15 * time.Minute

type plexPin struct {
	created  time.Time
	clientID string
	servers  []plexServer // nil until the account has signed in
}

type plexServer struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Connections []plexConnection `json:"connections"`
	token       string
}

type plexConnection struct {
	URI   string `json:"uri"`
	Local bool   `json:"local,omitempty"`
	Relay bool   `json:"relay,omitempty"`
	OK    bool   `json:"ok,omitempty"` // Musaarr reached the server there
}

// plexTVDo calls plex.tv as the Musaarr app this install registered as.
func (c *Core) plexTVDo(ctx context.Context, method, path, clientID, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.plexTV+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Product", "Musaarr")
	req.Header.Set("X-Plex-Client-Identifier", clientID)
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("plex.tv %s : %d", req.URL.Path, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// plexClientID is this install's name for plex.tv, made once and kept in the settings.
func (c *Core) plexClientID() (string, error) {
	if id := c.settings.Getenv("PLEX_CLIENT_ID"); id != "" {
		return id, nil
	}
	id := rand.Text()
	return id, c.settings.Save(map[string]string{"PLEX_CLIENT_ID": id})
}

// pin returns a PIN this Musaarr asked for in the last 15 minutes. Callers hold c.pinMu.
func (c *Core) pin(id int) *plexPin {
	for k, p := range c.pins {
		if time.Since(p.created) > plexPinLife {
			delete(c.pins, k)
		}
	}
	return c.pins[id]
}

func plexTVDown(w http.ResponseWriter) {
	fail(w, http.StatusBadGateway, "setup.plexTVUnreachable", nil)
}

func (c *Core) newPlexPin(w http.ResponseWriter, r *http.Request) {
	clientID, err := c.plexClientID()
	if err != nil {
		fail(w, http.StatusInternalServerError, "setup.notSaved", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var pin struct {
		ID   int    `json:"id"`
		Code string `json:"code"`
	}
	if err := c.plexTVDo(ctx, "POST", "/api/v2/pins?strong=true", clientID, "", &pin); err != nil || pin.ID == 0 {
		plexTVDown(w)
		return
	}
	c.pinMu.Lock()
	c.pin(0) // drops the expired ones
	c.pins[pin.ID] = &plexPin{created: time.Now(), clientID: clientID}
	c.pinMu.Unlock()
	q := url.Values{"clientID": {clientID}, "code": {pin.Code}, "context[device][product]": {"Musaarr"}}
	writeJSON(w, http.StatusOK, map[string]any{"id": pin.ID, "code": pin.Code, "url": "https://app.plex.tv/auth#?" + q.Encode()})
}

func pinUnknown(w http.ResponseWriter) {
	fail(w, http.StatusNotFound, "setup.plexPinUnknown", nil)
}

// plexPinState says whether the admin approved the code yet and, once they did, lists the servers.
func (c *Core) plexPinState(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	c.pinMu.Lock()
	p := c.pin(id)
	var clientID string
	var servers []plexServer
	if p != nil {
		clientID, servers = p.clientID, p.servers
	}
	c.pinMu.Unlock()
	type state struct {
		Done    bool         `json:"done"`
		Servers []plexServer `json:"servers,omitempty"`
	}
	switch {
	case p == nil:
		pinUnknown(w)
		return
	case servers != nil:
		writeJSON(w, http.StatusOK, state{true, servers})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var st struct {
		AuthToken string `json:"authToken"`
	}
	if err := c.plexTVDo(ctx, "GET", "/api/v2/pins/"+strconv.Itoa(id), clientID, "", &st); err != nil {
		plexTVDown(w)
		return
	}
	if st.AuthToken == "" {
		writeJSON(w, http.StatusOK, state{})
		return
	}
	servers, err := c.plexServers(ctx, clientID, st.AuthToken)
	if err != nil {
		plexTVDown(w)
		return
	}
	c.pinMu.Lock()
	p.servers = servers
	c.pinMu.Unlock()
	writeJSON(w, http.StatusOK, state{true, servers})
}

// plexServers lists the account's servers, the addresses that answer from here first.
func (c *Core) plexServers(ctx context.Context, clientID, token string) ([]plexServer, error) {
	var devices []struct {
		Name, ClientIdentifier, Provides, AccessToken string
		Connections                                   []plexConnection
	}
	if err := c.plexTVDo(ctx, "GET", "/api/v2/resources?includeHttps=1&includeRelay=1", clientID, token, &devices); err != nil {
		return nil, err
	}
	servers := []plexServer{}
	for _, d := range devices {
		if !slices.Contains(strings.Split(d.Provides, ","), "server") {
			continue
		}
		conns := slices.DeleteFunc(d.Connections, func(c plexConnection) bool {
			return !strings.HasPrefix(c.URI, "http://") && !strings.HasPrefix(c.URI, "https://")
		})
		tryConnections(ctx, conns, d.AccessToken)
		servers = append(servers, plexServer{ID: d.ClientIdentifier, Name: d.Name, Connections: conns, token: d.AccessToken})
	}
	return servers, nil
}

// tryConnections asks each address for the server's identity, all at once, then sorts them:
// those that answered, then local ones, relays last.
func tryConnections(ctx context.Context, conns []plexConnection, token string) {
	var wg sync.WaitGroup
	for i := range conns {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			var id struct{}
			conns[i].OK = NewPlex(conns[i].URI, token).get(ctx, "/identity", &id) == nil
		})
	}
	wg.Wait()
	score := func(c plexConnection) int {
		s := 0
		for _, b := range []bool{c.OK, c.Local, !c.Relay} {
			s *= 2
			if b {
				s++
			}
		}
		return s
	}
	slices.SortStableFunc(conns, func(a, b plexConnection) int { return cmp.Compare(score(b), score(a)) })
}

// choosePlexServer keeps the server and address the admin picked among those plex.tv listed.
func (c *Core) choosePlexServer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Pin    int    `json:"pin"`
		Server string `json:"server"`
		URI    string `json:"uri"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "setup.badFormat", nil)
		return
	}
	c.pinMu.Lock()
	p := c.pin(in.Pin)
	token := ""
	if p != nil {
		for _, s := range p.servers {
			if s.ID == in.Server && slices.ContainsFunc(s.Connections, func(c plexConnection) bool { return c.URI == in.URI }) {
				token = s.token
			}
		}
	}
	c.pinMu.Unlock()
	switch {
	case p == nil:
		pinUnknown(w)
		return
	case token == "":
		fail(w, http.StatusBadRequest, "setup.plexUnknownServer", nil)
		return
	case c.settings.Locked("PLEX_URL") || c.settings.Locked("PLEX_TOKEN"):
		fail(w, http.StatusConflict, "setup.locked", map[string]any{"key": "PLEX_URL"})
		return
	}
	if err := c.settings.Save(map[string]string{"PLEX_URL": in.URI, "PLEX_TOKEN": token}); err != nil {
		fail(w, http.StatusInternalServerError, "setup.notSaved", nil)
		return
	}
	c.pinMu.Lock()
	delete(c.pins, in.Pin)
	c.pinMu.Unlock()
	if err := c.Apply(); err != nil {
		fail(w, http.StatusInternalServerError, "setup.notApplied", nil)
		return
	}
	writeJSON(w, http.StatusOK, c.view())
}
