package hubapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// A computer's files: a folder's entries, a file to preview and its bytes to save (docs/protocol/protocol.md "File preview").

// FsEntry is a folder's entry.
type FsEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Symlink bool   `json:"symlink,omitempty"`
	Size    int64  `json:"size"`
	Mtime   string `json:"mtime,omitempty"`
	Ignored bool   `json:"ignored,omitempty"`
}

// FsListing is a folder (GET /machines/{mid}/fs).
type FsListing struct {
	Path      string    `json:"path"`
	Entries   []FsEntry `json:"entries"`
	Truncated bool      `json:"truncated,omitempty"`
}

// ListDir reads a folder: path, absolute or under cwd ("" is cwd itself).
func (c *Client) ListDir(ctx context.Context, machine, path, cwd string) (FsListing, error) {
	var out FsListing
	q := url.Values{"path": {path}, "cwd": {cwd}}
	err := c.Do(ctx, http.MethodGet, "/machines/"+url.PathEscape(machine)+"/fs?"+q.Encode(), nil, &out)
	return out, err
}

// FileContent is a file to preview (GET /machines/{mid}/file): its text, or its bytes in base64.
type FileContent struct {
	Path      string  `json:"path"`
	Name      string  `json:"name"`
	Size      int64   `json:"size"`
	Mime      string  `json:"mime,omitempty"`
	Text      *string `json:"text,omitempty"`
	Data      string  `json:"data,omitempty"`
	Truncated bool    `json:"truncated,omitempty"`
	IsDir     bool    `json:"is_dir,omitempty"`
	Offset    int64   `json:"offset,omitempty"`
	Next      int64   `json:"next,omitempty"`
}

// ReadFile reads a file to preview from offset (a text's next page).
func (c *Client) ReadFile(ctx context.Context, machine, path, cwd string, offset int64) (FileContent, error) {
	var out FileContent
	q := url.Values{"path": {path}, "cwd": {cwd}}
	if offset > 0 {
		q.Set("offset", strconv.FormatInt(offset, 10))
	}
	err := c.Do(ctx, http.MethodGet, "/machines/"+url.PathEscape(machine)+"/file?"+q.Encode(), nil, &out)
	return out, err
}

// Download writes a file's bytes to w (GET /machines/{mid}/file/download).
func (c *Client) Download(ctx context.Context, machine, path, cwd string, w io.Writer) error {
	q := url.Values{"path": {path}, "cwd": {cwd}}
	return c.Fetch(ctx, "/machines/"+url.PathEscape(machine)+"/file/download?"+q.Encode(), w)
}

// Fetch writes what a GET of path answers to w, as bytes (a download).
func (c *Client) Fetch(ctx context.Context, path string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(path), nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("X-Persona-Client", Platform+"/"+Version)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode >= 300 {
		e := &Error{Status: resp.StatusCode}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = json.Unmarshal(data, e)
		return e
	}
	_, err = io.Copy(w, resp.Body)
	return err
}
