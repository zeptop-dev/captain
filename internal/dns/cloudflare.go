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
var ErrExternalRecords = errors.New("existing DNS records use multiple addresses or a proxy; manage this name externally")

const DefaultBase = "https://api.cloudflare.com/client/v4"

// Cloudflare is a minimal client: zone lookup, record upsert.
type Cloudflare struct {
	Token  string
	Base   string // "" = DefaultBase
	Client *http.Client
}

// Rejected means the provider explicitly rejected a request (not a timeout or
// ambiguous server failure). Do not persist Message: it is provider-controlled.
type Rejected struct{ Message string }

func (e *Rejected) Error() string { return "cloudflare: " + e.Message }

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
		if resp.StatusCode < 500 {
			return &Rejected{Message: msg}
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
// creating or updating a single DNS-only record. Multi-record pools and
// proxied records are externally managed and must not be overwritten. Returns "created",
// "updated" or "unchanged".
func (c *Cloudflare) EnsureAddress(ctx context.Context, zone, fqdn, ip string) (string, error) {
	p, err := c.PlanAddress(ctx, zone, fqdn, ip)
	if err != nil {
		return "skipped", err
	}
	if p.Action == "unchanged" {
		return p.Action, nil
	}
	_, err = c.ApplyAddress(ctx, p)
	return p.Action, err
}

// AddressPlan separates read/validation from mutation so callers can durably
// journal the previous record before sending a write. It is not a provider CAS:
// an external editor may still change the record between these requests.
type AddressPlan struct {
	ZoneID string
	Action string
	Before *Record
	Wanted Record
}

func (c *Cloudflare) PlanAddress(ctx context.Context, zone, fqdn, ip string) (AddressPlan, error) {
	p := AddressPlan{}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return p, errors.New("not an IP address: " + ip)
	}
	typ := "A"
	if parsed.To4() == nil {
		typ = "AAAA"
	}
	zid, err := c.ZoneID(ctx, zone)
	if err != nil {
		return p, err
	}
	var recs []Record
	if err := c.do(ctx, http.MethodGet, "/zones/"+zid+"/dns_records?type="+typ+"&name="+url.QueryEscape(fqdn), nil, &recs); err != nil {
		return p, err
	}
	want := Record{Type: typ, Name: fqdn, Content: ip, TTL: 1}
	p.ZoneID, p.Wanted, p.Action = zid, want, "created"
	if len(recs) == 0 {
		return p, nil
	}
	if len(recs) > 1 || recs[0].Proxied {
		return p, ErrExternalRecords
	}
	p.Before = &recs[0]
	p.Action = "updated"
	p.Wanted.TTL = recs[0].TTL
	if p.Wanted.TTL == 0 {
		p.Wanted.TTL = 1
	}
	if recs[0].Content == ip {
		p.Action = "unchanged"
	}
	return p, nil
}

func (c *Cloudflare) ApplyAddress(ctx context.Context, p AddressPlan) (*Record, error) {
	method, path := http.MethodPost, "/zones/"+p.ZoneID+"/dns_records"
	if p.Before != nil {
		method, path = http.MethodPut, path+"/"+p.Before.ID
	}
	var record *Record
	err := c.do(ctx, method, path, p.Wanted, &record)
	return record, err
}
