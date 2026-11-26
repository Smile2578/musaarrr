package main

import (
	"testing"
	"time"
)

func TestComputeStatus(t *testing.T) {
	now := time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)
	done0 := &Command{Status: "completed", Message: "Album search completed. 0 reports downloaded.", Ended: now.Add(-time.Minute)}
	done1 := &Command{Status: "completed", Message: "Album search completed. 1 reports downloaded.", Ended: now.Add(-time.Minute)}
	done1old := &Command{Status: "completed", Message: "Album search completed. 1 reports downloaded.", Ended: now.Add(-10 * time.Minute)}
	dl := QueueItem{AlbumID: 7, Size: 1000, Sizeleft: 250, State: "downloading", Timeleft: "00:03:00"}
	q := func(state, status string) []QueueItem { return []QueueItem{{State: state, Status: status}} }
	rows := []struct {
		name     string
		s        Signals
		status   string
		progress int
	}{
		{"plex wins", Signals{InPlex: true, Percent: 100}, "in_plex", 0},
		{"imported", Signals{Percent: 100, Cmd: done1}, "imported", 0},
		{"downloading", Signals{Queue: []QueueItem{dl}, Cmd: done1}, "downloading", 75},
		{"import pending", Signals{Queue: q("importPending", "completed")}, "importing", 0},
		{"importing", Signals{Queue: q("importing", "completed")}, "importing", 0},
		{"import blocked", Signals{Queue: q("importBlocked", "completed")}, "check", 0},
		{"import failed", Signals{Queue: q("importFailed", "completed")}, "check", 0},
		{"failed pending", Signals{Queue: q("downloadFailedPending", "failed")}, "failed", 0},
		{"failed", Signals{Queue: q("downloadFailed", "failed")}, "failed", 0},
		{"delay", Signals{Queue: q("downloading", "delay")}, "searching", 0},
		{"client unavailable", Signals{Queue: q("downloading", "downloadClientUnavailable")}, "searching", 0},
		{"partial import", Signals{Percent: 40, Cmd: done1old}, "check", 0},
		{"partial import, new search", Signals{Percent: 40, Cmd: &Command{Status: "started"}}, "searching", 0},
		{"not searched yet", Signals{}, "requested", 0},
		{"queued", Signals{Cmd: &Command{Status: "queued"}}, "searching", 0},
		{"started", Signals{Cmd: &Command{Status: "started"}}, "searching", 0},
		{"nothing found", Signals{Cmd: done0}, "waiting", 0},
		{"grabbed recently", Signals{Cmd: done1}, "searching", 0},
		{"grabbed long ago, gone", Signals{Cmd: done1old}, "waiting", 0},
		{"command failed", Signals{Cmd: &Command{Status: "failed"}}, "waiting", 0},
	}
	for _, r := range rows {
		r.s.Now = now
		st, p, _ := computeStatus(r.s)
		if st != r.status || p != r.progress {
			t.Errorf("%s: got %s %d, want %s %d", r.name, st, p, r.status, r.progress)
		}
	}
	if _, _, tl := computeStatus(Signals{Queue: []QueueItem{dl}, Now: now}); tl != "00:03:00" {
		t.Errorf("timeleft %q", tl)
	}
}

func TestStatusCommandGone(t *testing.T) {
	if st, _, _ := computeStatus(Signals{Cmd: &Command{Gone: true}, Now: time.Now()}); st != "waiting" {
		t.Fatalf("got %s", st)
	}
}

func TestProgressZeroSize(t *testing.T) {
	st, p, _ := computeStatus(Signals{Queue: []QueueItem{{State: "downloading"}}, Now: time.Now()})
	if st != "downloading" || p != 0 {
		t.Fatalf("got %s %d", st, p)
	}
	_, p, _ = computeStatus(Signals{Queue: []QueueItem{{State: "downloading", Size: 10, Sizeleft: 20}}, Now: time.Now()})
	if p != 0 {
		t.Fatalf("negative progress not clamped: %d", p)
	}
}
