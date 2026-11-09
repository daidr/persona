package hubapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// The Git panel's reads and writes beyond the status and a file's diff (docs/protocol/protocol.md "git/stage … git/stash").

func gitPath(machine, what string) string {
	return "/machines/" + url.PathEscape(machine) + "/git/" + what
}

// GitShow is a commit with its files and diff (GET …/git/show).
type GitShow struct {
	Commit    GitCommit `json:"commit"`
	Files     []GitFile `json:"files"`
	Diff      string    `json:"diff"`
	Truncated bool      `json:"truncated,omitempty"`
}

func (c *Client) GitShow(ctx context.Context, machine, cwd, hash string) (GitShow, error) {
	var out GitShow
	err := c.Do(ctx, http.MethodGet, gitPath(machine, "show")+"?"+url.Values{"cwd": {cwd}, "hash": {hash}}.Encode(), nil, &out)
	return out, err
}

// GitLog reads older commits: limit of them after the first skip.
func (c *Client) GitLog(ctx context.Context, machine, cwd string, skip, limit int) ([]GitCommit, bool, error) {
	var out struct {
		Commits []GitCommit `json:"commits"`
		HasMore *bool       `json:"has_more"`
	}
	q := url.Values{"cwd": {cwd}, "skip": {strconv.Itoa(skip)}, "limit": {strconv.Itoa(limit)}}
	err := c.Do(ctx, http.MethodGet, gitPath(machine, "log")+"?"+q.Encode(), nil, &out)
	more := len(out.Commits) >= limit
	if out.HasMore != nil {
		more = *out.HasMore
	}
	return out.Commits, more, err
}

// GitBranch is a local or remote branch (GET …/git/branches).
type GitBranch struct {
	Name     string `json:"name"`
	Remote   bool   `json:"remote,omitempty"`
	Current  bool   `json:"current,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Hash     string `json:"hash"`
	Subject  string `json:"subject"`
	Date     string `json:"date"`
}

func (c *Client) GitBranches(ctx context.Context, machine, cwd string) ([]GitBranch, error) {
	var out struct {
		Branches []GitBranch `json:"branches"`
	}
	err := c.Do(ctx, http.MethodGet, gitPath(machine, "branches")+"?cwd="+url.QueryEscape(cwd), nil, &out)
	return out.Branches, err
}

// GitStash is a stash entry; Oid is missing on older nodes, which are never written to by index alone.
type GitStash struct {
	Index   int    `json:"index"`
	Oid     string `json:"oid,omitempty"`
	Message string `json:"message"`
	Date    string `json:"date"`
}

// GitWriteResult is what a write answers: the repository as it is after it.
type GitWriteResult struct {
	Status  GitStatus  `json:"status"`
	Commit  *GitCommit `json:"commit,omitempty"`
	Output  string     `json:"output,omitempty"`
	Stashes []GitStash `json:"stashes,omitempty"`
}

// GitWrite runs one of the writes (stage, commit, push, switch, stash) in cwd.
func (c *Client) GitWrite(ctx context.Context, machine, op, cwd string, body map[string]any) (GitWriteResult, error) {
	b := map[string]any{"cwd": cwd}
	for k, v := range body {
		b[k] = v
	}
	var out GitWriteResult
	err := c.Do(ctx, http.MethodPost, gitPath(machine, op), b, &out)
	return out, err
}

// GitCheck is one of a pull request's checks.
type GitCheck struct {
	Name  string `json:"name"`
	State string `json:"state"` // pass | fail | pending | skipped
	URL   string `json:"url,omitempty"`
}

// GitPR is the branch's pull request, read through gh on the computer.
type GitPR struct {
	Number      int        `json:"number"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	State       string     `json:"state"` // OPEN | CLOSED | MERGED
	Draft       bool       `json:"draft,omitempty"`
	Base        string     `json:"base"`
	Head        string     `json:"head"`
	MergeState  string     `json:"merge_state,omitempty"`
	Review      string     `json:"review,omitempty"`
	AutoMerge   bool       `json:"auto_merge,omitempty"`
	Checks      []GitCheck `json:"checks"`
	ChecksState string     `json:"checks_state,omitempty"`
}

// GitPRs reads the branch's pull request; nil when it has none.
func (c *Client) GitPR(ctx context.Context, machine, cwd string) (*GitPR, error) {
	var out struct {
		PR *GitPR `json:"pr"`
	}
	err := c.Do(ctx, http.MethodGet, gitPath(machine, "pr")+"?cwd="+url.QueryEscape(cwd), nil, &out)
	return out.PR, err
}

// GitPRCreate opens a pull request for the branch.
func (c *Client) GitPRCreate(ctx context.Context, machine, cwd, title, body string, draft bool) (*GitPR, error) {
	var out struct {
		PR *GitPR `json:"pr"`
	}
	err := c.Do(ctx, http.MethodPost, gitPath(machine, "pr"), map[string]any{"cwd": cwd, "title": title, "body": body, "draft": draft}, &out)
	return out.PR, err
}

// GitPRMerge merges the pull request (squash), or sets it to merge once its checks pass (auto).
func (c *Client) GitPRMerge(ctx context.Context, machine, cwd string, number int, auto bool) (*GitPR, error) {
	var out struct {
		PR *GitPR `json:"pr"`
	}
	b := map[string]any{"cwd": cwd, "number": number, "method": "squash"}
	if auto {
		b["auto"] = true
	}
	err := c.Do(ctx, http.MethodPost, gitPath(machine, "pr/merge"), b, &out)
	return out.PR, err
}
