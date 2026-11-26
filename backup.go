package main

import (
	"archive/zip"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Backup and restore: one zip of what Musaarr cannot rebuild (accounts, settings, requests, the
// room), to keep, or to move an install to another machine. The shop, its covers and the Plex
// index are rebuilt on their own, so they stay out. The zip holds keys and password hashes:
// admins only, and the page says to keep it like a password.

var backupFiles = []string{"auth.json", "requests.json", "room.json", "settings.json"}

const backupMax = 16 << 20

func (c *Core) backup(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range backupFiles {
		b, err := os.ReadFile(filepath.Join(c.dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		var f io.Writer
		if err == nil {
			f, err = zw.Create(name)
		}
		if err == nil {
			_, err = f.Write(b)
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "backup.failed", nil)
			return
		}
	}
	if err := zw.Close(); err != nil {
		fail(w, http.StatusInternalServerError, "backup.failed", nil)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="musaarr-backup-%s.zip"`, time.Now().Format("2006-01-02")))
	h.Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

// backupSet is a backup read and checked: nil for a file it does not hold.
type backupSet struct {
	auth     *authFile
	settings *settingsFile
	requests *[]Request
	room     *Room
}

// readBackup checks the whole archive before anything is replaced.
func readBackup(body []byte) (backupSet, *setupErr) {
	var set backupSet
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return set, badSetup("backup.unreadable", nil)
	}
	read := map[string]bool{}
	for _, f := range zr.File {
		if !slices.Contains(backupFiles, f.Name) {
			continue // files are taken by name only: nothing is ever extracted to a path
		}
		if read[f.Name] { // each file is read once: copies would multiply the work
			return set, badSetup("backup.unreadable", nil)
		}
		read[f.Name] = true
		rc, err := f.Open()
		if err != nil {
			return set, badSetup("backup.unreadable", nil)
		}
		b, err := io.ReadAll(io.LimitReader(rc, backupMax+1))
		rc.Close()
		if err != nil || len(b) > backupMax {
			return set, badSetup("backup.unreadable", nil)
		}
		dst := map[string]any{"auth.json": &set.auth, "requests.json": &set.requests, "room.json": &set.room, "settings.json": &set.settings}[f.Name]
		if json.Unmarshal(b, dst) != nil {
			return set, badSetup("backup.badFile", map[string]any{"name": f.Name})
		}
	}
	switch {
	case len(read) == 0:
		return set, badSetup("backup.empty", nil)
	case set.auth != nil && !slices.ContainsFunc(set.auth.Users, func(u User) bool { return u.Admin }):
		return set, badSetup("backup.noAdmin", nil)
	case set.settings != nil && set.settings.Version > settingsVersion:
		return set, badSetup("backup.tooNew", nil)
	}
	if set.settings != nil {
		for k, v := range set.settings.Values {
			if strings.HasSuffix(k, "_URL") && !webAddress(v) {
				return set, badSetup("backup.badFile", map[string]any{"name": "settings.json"})
			}
		}
	}
	return set, nil
}

// addressesStay refuses a backup that would send a key fixed by the environment somewhere new.
func (c *Core) addressesStay(set backupSet) *setupErr {
	if set.settings == nil {
		return nil
	}
	for addr, key := range addressKeys {
		to := cmp.Or(set.settings.Values[addr], configDefaults[addr])
		if c.settings.Locked(key) && !c.settings.Locked(addr) && to != "" &&
			!sameURL(to, cmp.Or(c.settings.Getenv(addr), configDefaults[addr])) {
			return badSetup("setup.addressLocked", map[string]any{"key": key})
		}
	}
	return nil
}

// restore replaces what the backup holds, then starts the app it describes. Whoever restores is
// signed out when the backup's accounts are not theirs.
func (c *Core) restore(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, backupMax))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "backup.tooBig", nil)
		return
	}
	set, bad := readBackup(body)
	if bad == nil {
		bad = c.addressesStay(set)
	}
	if bad != nil {
		bad.write(w)
		return
	}
	if err := c.restoreSet(set); err != nil {
		fail(w, http.StatusInternalServerError, "backup.notRestored", nil)
		return
	}
	writeJSON(w, http.StatusOK, c.view())
}

// restoreSet writes each file through its owner, so nothing still running writes an old copy over it.
func (c *Core) restoreSet(set backupSet) error {
	var errs []error
	if set.settings != nil {
		errs = append(errs, c.settings.replace(set.settings.Values))
	}
	if set.auth != nil {
		errs = append(errs, c.auth.replace(*set.auth))
	}
	if set.requests != nil {
		errs = append(errs, c.store.replace(*set.requests))
	}
	if set.room != nil {
		b, err := json.MarshalIndent(set.room.clean(), "", " ")
		if err == nil {
			err = writeFileSync(filepath.Join(c.dir, "room.json"), b)
		}
		errs = append(errs, err)
	}
	errs = append(errs, c.Apply()) // the new App reads the room from disk
	return errors.Join(errs...)
}
