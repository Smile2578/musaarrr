package main

import (
	"math"
	"strings"
	"time"
)

// Signals is everything Lidarr and Plex tell us about one request.
type Signals struct {
	Cmd     *Command
	Queue   []QueueItem
	Percent float64
	InPlex  bool
	Now     time.Time
}

// grabGrace covers the gap between a grab and the download showing up in the queue.
const grabGrace = 2 * time.Minute

// computeStatus maps Lidarr/Plex signals to one status code; first matching rule wins.
func computeStatus(s Signals) (status string, progress int, timeleft string) {
	switch {
	case s.InPlex:
		return "in_plex", 0, ""
	case s.Percent >= 100:
		return "imported", 0, ""
	case len(s.Queue) > 0:
		q := s.Queue[0]
		switch q.Status { // grabbed, but held back by Lidarr before reaching the client
		case "delay", "downloadClientUnavailable", "fallback":
			return "searching", 0, ""
		}
		switch q.State {
		case "downloading":
			if q.Size > 0 {
				progress = int(math.Round((q.Size - q.Sizeleft) / q.Size * 100))
			}
			return "downloading", max(0, min(100, progress)), q.Timeleft
		case "importPending", "importing":
			return "importing", 0, ""
		case "importBlocked", "importFailed":
			return "check", 0, ""
		case "downloadFailedPending", "downloadFailed":
			return "failed", 0, ""
		}
		return "searching", 0, ""
	case s.Cmd != nil && (s.Cmd.Status == "queued" || s.Cmd.Status == "started"):
		return "searching", 0, ""
	case s.Percent > 0:
		return "check", 0, ""
	case s.Cmd == nil:
		return "requested", 0, ""
	case s.Cmd.Status == "completed" && !strings.Contains(s.Cmd.Message, " 0 reports") && s.Now.Sub(s.Cmd.Ended) < grabGrace:
		return "searching", 0, ""
	}
	return "waiting", 0, ""
}
