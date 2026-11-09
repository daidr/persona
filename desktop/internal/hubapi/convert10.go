package hubapi

import (
	"context"
	"net/http"
	"net/url"
)

// ConvertFile has the computer convert an Office document (Word, PowerPoint, Excel…) and answers where the PDF is.
func (c *Client) ConvertFile(ctx context.Context, machine, path, cwd, to string) (string, error) {
	var out struct {
		Path string `json:"path"`
	}
	q := url.Values{"path": {path}, "cwd": {cwd}, "to": {to}}
	err := c.Do(ctx, http.MethodGet, "/machines/"+url.PathEscape(machine)+"/file/convert?"+q.Encode(), nil, &out)
	return out.Path, err
}
