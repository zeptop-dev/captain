package backup

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// WebDAV puts files into one collection with basic auth (Nextcloud,
// Synology, rclone serve webdav, ...).
type WebDAV struct {
	URL, Username, Password string
	Client                  *http.Client
}

func (w *WebDAV) do(ctx context.Context, method, url string, body io.Reader, size int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if size > 0 {
		req.ContentLength = size
	}
	if w.Username != "" {
		req.SetBasicAuth(w.Username, w.Password)
	}
	return w.Client.Do(req)
}

func (w *WebDAV) Put(ctx context.Context, name string, body io.Reader, size int64) error {
	resp, err := w.do(ctx, http.MethodPut, joinURL(w.URL, name), body, size)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("webdav PUT: %s", resp.Status)
	}
	return nil
}

func (w *WebDAV) Delete(ctx context.Context, name string) error {
	resp, err := w.do(ctx, http.MethodDelete, joinURL(w.URL, name), nil, 0)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("webdav DELETE: %s", resp.Status)
	}
	return nil
}

// List returns captain-*.db.gz names in the collection (PROPFIND depth 1).
func (w *WebDAV) List(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", strings.TrimRight(w.URL, "/")+"/", strings.NewReader(`<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/></d:prop></d:propfind>`))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")
	if w.Username != "" {
		req.SetBasicAuth(w.Username, w.Password)
	}
	resp, err := w.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("webdav PROPFIND: %s", resp.Status)
	}
	var ms struct {
		Responses []struct {
			Href string `xml:"href"`
		} `xml:"response"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, err
	}
	var out []string
	for _, r := range ms.Responses {
		n := path.Base(strings.TrimRight(r.Href, "/"))
		if strings.HasPrefix(n, "captain-") && strings.HasSuffix(n, ".db.gz") {
			out = append(out, n)
		}
	}
	return out, nil
}
