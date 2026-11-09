package hubapi

import (
	"context"
	"net/http"
	"slices"
	"sort"
)

// Hub features for agent archives (docs/protocol/protocol.md "Agent session archives").
const (
	FeatureArchives     = "agent_session_archives"
	FeatureArchiveClose = "agent_session_archive_close"
)

// ArchiveBusy is busy as the manager counts it for an archive: working, starting, waiting for an answer or background work.
func (s Session) ArchiveBusy() bool {
	return s.Starting || s.Status == "working" || s.Status == "blocked" || s.State == StateWorking || s.State == StateNeedsYou ||
		s.BackgroundWorking || s.BackgroundWaiting || s.Pending > 0
}

// CloseBusy is busy for closing: also a /loop, queued messages or a server it runs.
func (s Session) CloseBusy() bool {
	return s.ArchiveBusy() || s.Loop != nil || len(s.Queued) > 0 || len(s.Ports) > 0
}

// ArchiveOffer is what a session's menu offers: "ok", "busy" (shown disabled) or "no" (not offered).
// Archive and close is for an agent in a terminal tab the node can bring back: Claude Code, or Codex that Persona started.
func ArchiveOffer(s Session, features []string) (archive, close string) {
	if s.Assistant || s.Archived || !slices.Contains(features, FeatureArchives) {
		return "no", "no"
	}
	archive = "ok"
	if s.ArchiveBusy() {
		archive = "busy"
	}
	resumable := s.Agent == "" || s.Agent == "claude" || ((s.Agent == "codex" || s.Agent == "traex") && s.StartedByPersona)
	open := s.State != StateOffline && s.Status != "offline" && s.StartError == ""
	switch {
	case !slices.Contains(features, FeatureArchiveClose) || s.SourceKind != "" || !resumable || !open:
		close = "no"
	case s.CloseBusy():
		close = "busy"
	default:
		close = "ok"
	}
	return archive, close
}

// CloseResult is what "Archive and close" did beyond the archive.
type CloseResult struct {
	Closed  bool   `json:"closed"`
	Outcome string `json:"outcome"` // closed | not_running | refused | offline | failed
	Reason  string `json:"reason,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// ArchiveSession takes an agent out of the Agents list (POST …/archive), and with close also closes it on its computer.
// close is nil when the hub said nothing about closing.
func (c *Client) ArchiveSession(ctx context.Context, machine, session string, close bool) (*CloseResult, error) {
	path := sessionPath(machine, session) + "/archive"
	if close {
		path += "?close=1"
	}
	var out struct {
		Close *CloseResult `json:"close"`
	}
	err := c.Do(ctx, http.MethodPost, path, map[string]any{}, &out)
	return out.Close, err
}

// UnarchiveSession brings an archived agent back to the Agents list.
func (c *Client) UnarchiveSession(ctx context.Context, machine, session string) error {
	return c.Do(ctx, http.MethodPost, sessionPath(machine, session)+"/unarchive", map[string]any{}, nil)
}

// ArchivedSessions is every archived agent, newest activity first.
func (c *Client) ArchivedSessions(ctx context.Context) ([]Session, error) {
	var out struct {
		Sessions []Session `json:"sessions"`
	}
	if err := c.Do(ctx, http.MethodGet, "/sessions/archived", nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Sessions {
		out.Sessions[i].Archived = true
	}
	sort.SliceStable(out.Sessions, func(i, j int) bool {
		a, b := out.Sessions[i], out.Sessions[j]
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		return a.Key() < b.Key()
	})
	return out.Sessions, nil
}
