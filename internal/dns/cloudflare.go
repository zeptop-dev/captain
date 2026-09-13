// Package dns keeps DNS records in step with what the panel knows: node
// host names and line entry names get A/AAAA records on Cloudflare.
package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBase is Cloudflare's v4 API.
const DefaultBase = "https://api.cloudflare.com/client/v4"

// Cloudflare is a minimal client: zone lookup, record upsert.
type Cloudflare struct {
	Token  string
	Base   string // "" = DefaultBase
	Client *http.Client
}

func (c *Cloudflare) base() string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return DefaultBase
}

func (c *Cloudflare) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	cl := c.Client
	if cl == nil {
		cl = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env struct {
		Success bool                       `json:"success"`
		Errors  []struct{ Message string } `json:"errors"`
		Result  json.RawMessage            `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("cloudflare: %s %s: %w", method, path, err)
	}
	if !env.Success {
		msg := resp.Status
		if len(env.Errors) > 0 {
			msg = env.Errors[0].Message
		}
		return fmt.Errorf("cloudflare: %s", msg)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// ZoneID resolves a zone by its name.
func (c *Cloudflare) ZoneID(ctx context.Context, zone string) (string, error) {
	var zones []struct{ ID string }
	if err := c.do(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(zone), nil, &zones); err != nil {
		return "", err
	}
	if len(zones) == 0 {
		return "", fmt.Errorf("cloudflare: zone %s not found with this token", zone)
	}
	return zones[0].ID, nil
}

// Record is one DNS record.
type Record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// EnsureAddress makes fqdn resolve to ip (A or AAAA by the address kind),
// creating or updating the record; other records of that type on the name
// are left alone only when they already match. Returns "created",
// "updated" or "unchanged".
func (c *Cloudflare) EnsureAddress(ctx context.Context, zone, fqdn, ip string) (string, error) {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "", errors.New("not an IP address: " + ip)
	}
	typ := "A"
	if parsed.To4() == nil {
		typ = "AAAA"
	}
	zid, err := c.ZoneID(ctx, zone)
	if err != nil {
		return "", err
	}
	var recs []Record
	if err := c.do(ctx, http.MethodGet, "/zones/"+zid+"/dns_records?type="+typ+"&name="+url.QueryEscape(fqdn), nil, &recs); err != nil {
		return "", err
	}
	want := Record{Type: typ, Name: fqdn, Content: ip, TTL: 1}
	if len(recs) == 0 {
		return "created", c.do(ctx, http.MethodPost, "/zones/"+zid+"/dns_records", want, nil)
	}
	if recs[0].Content == ip && !recs[0].Proxied {
		return "unchanged", nil
	}
	return "updated", c.do(ctx, http.MethodPut, "/zones/"+zid+"/dns_records/"+recs[0].ID, want, nil)
}
