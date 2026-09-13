package backup

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3 talks to any SigV4 bucket (AWS, Cloudflare R2, Backblaze B2, MinIO)
// with hand-rolled signing: PUT, DELETE and a prefix listing are all we need.
type S3 struct {
	Endpoint, Region, Bucket, Prefix string
	AccessKey, SecretKey             string
	PathStyle                        bool
	Client                           *http.Client
	// now is for tests.
	now func() time.Time
}

func (s *S3) key(name string) string {
	p := strings.Trim(s.Prefix, "/")
	if p == "" {
		return name
	}
	return p + "/" + name
}

// objectURL builds the request URL: virtual-hosted (bucket.host) unless
// PathStyle or the endpoint already names the bucket.
func (s *S3) objectURL(key string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(s.Endpoint, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("bad S3 endpoint %q", s.Endpoint)
	}
	if s.PathStyle {
		u.Path = "/" + s.Bucket
	} else {
		u.Host = s.Bucket + "." + u.Host
		u.Path = ""
	}
	if key != "" {
		u.Path += "/" + key
	}
	if u.Path == "" {
		u.Path = "/"
	}
	u.RawPath = escapePath(u.Path)
	return u, nil
}

// escapePath percent-encodes a path the way SigV4 canonicalises it.
func escapePath(p string) string {
	var b strings.Builder
	for _, c := range []byte(p) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' || c == '/' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sign adds the SigV4 Authorization header. payloadHash is the hex SHA-256
// of the body (or UNSIGNED-PAYLOAD when streaming).
func (s *S3) sign(req *http.Request, payloadHash string, at time.Time) {
	amzDate := at.UTC().Format("20060102T150405Z")
	date := amzDate[:8]
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{"host": host}
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		if lk == "host" || strings.HasPrefix(lk, "x-amz-") || lk == "content-type" || lk == "range" {
			headers[lk] = strings.TrimSpace(strings.Join(v, ","))
		}
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")
	// Canonical query: sorted, each key/value RFC 3986 encoded.
	q := req.URL.Query()
	qkeys := make([]string, 0, len(q))
	for k := range q {
		qkeys = append(qkeys, k)
	}
	sort.Strings(qkeys)
	var canonQuery []string
	for _, k := range qkeys {
		vals := q[k]
		sort.Strings(vals)
		for _, v := range vals {
			canonQuery = append(canonQuery, escapeQuery(k)+"="+escapeQuery(v))
		}
	}
	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := strings.Join([]string{req.Method, path, strings.Join(canonQuery, "&"), canonHeaders.String(), signed, payloadHash}, "\n")
	scope := date + "/" + s.Region + "/s3/aws4_request"
	toSign := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, sha256Hex([]byte(canonical))}, "\n")
	k := hmacSHA256([]byte("AWS4"+s.SecretKey), date)
	k = hmacSHA256(k, s.Region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", s.AccessKey, scope, signed, sig))
}

func escapeQuery(v string) string {
	return strings.ReplaceAll(url.QueryEscape(v), "+", "%20")
}

func (s *S3) do(ctx context.Context, method, key string, query url.Values, body io.Reader, size int64, payloadHash string) (*http.Response, error) {
	u, err := s.objectURL(key)
	if err != nil {
		return nil, err
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if size > 0 {
		req.ContentLength = size
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	s.sign(req, payloadHash, now())
	return s.Client.Do(req)
}

func (s *S3) Put(ctx context.Context, name string, body io.Reader, size int64) error {
	resp, err := s.do(ctx, http.MethodPut, s.key(name), nil, body, size, "UNSIGNED-PAYLOAD")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("s3 PUT: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

func (s *S3) Delete(ctx context.Context, name string) error {
	resp, err := s.do(ctx, http.MethodDelete, s.key(name), nil, nil, 0, sha256Hex(nil))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("s3 DELETE: %s", resp.Status)
	}
	return nil
}

// List returns captain-*.db.gz names under the prefix (ListObjectsV2).
func (s *S3) List(ctx context.Context) ([]string, error) {
	q := url.Values{"list-type": {"2"}}
	if p := strings.Trim(s.Prefix, "/"); p != "" {
		q.Set("prefix", p+"/captain-")
	} else {
		q.Set("prefix", "captain-")
	}
	resp, err := s.do(ctx, http.MethodGet, "", q, nil, 0, sha256Hex(nil))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("s3 LIST: %s", resp.Status)
	}
	var res struct {
		Contents []struct {
			Key string `xml:"Key"`
		} `xml:"Contents"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	var out []string
	for _, c := range res.Contents {
		n := c.Key
		if i := strings.LastIndex(n, "/"); i >= 0 {
			n = n[i+1:]
		}
		if strings.HasSuffix(n, ".db.gz") {
			out = append(out, n)
		}
	}
	return out, nil
}
