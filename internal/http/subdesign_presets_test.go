package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/subpreset"
)

type subPresetFetchFunc func(context.Context) (subpreset.Snapshot, error)

func (f subPresetFetchFunc) Fetch(ctx context.Context) (subpreset.Snapshot, error) { return f(ctx) }

func newSubPresetRig(t *testing.T, fetch subPresetFetchFunc) (*store.Store, *client, int64, int64) {
	t.Helper()
	return newSubDesignRig(t, Options{SubPresets: fetch})
}

func newSubDesignRig(t *testing.T, options Options) (*store.Store, *client, int64, int64) {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "captain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err = db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	u := &domain.User{Email: "admin@example.com", Role: domain.RoleAdmin, Status: "active"}
	if err = st.CreateStaff(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	token, info, err := st.CreateAPIToken(context.Background(), u.ID, "test", "full", nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.BaseURL = "http://example.com"
	srv := httptest.NewServer(New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), options).Handler())
	t.Cleanup(srv.Close)
	return st, &client{t: t, srv: srv, token: token}, u.ID, info.ID
}

func exampleSubPresetSnapshot() subpreset.Snapshot {
	// Independently written minimal fixture, not copied upstream content.
	text := "[custom]\nenable_rule_generator=true\noverwrite_original_rules=true\nruleset=DIRECT,[]GEOIP,CN\nruleset=Proxy,[]FINAL\ncustom_proxy_group=Proxy`select`.*\n"
	return subpreset.Snapshot{Revision: strings.Repeat("a", 40), SyncedAt: 1, Files: []subpreset.File{{Name: "Example.ini", URL: "https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/" + strings.Repeat("a", 40) + "/Clash/config/Example.ini", Content: text}, {Name: "Unsupported.ini", Content: text + "script=refused\n"}}}
}

func TestSubDesignPresetSyncIsolationAndFailure(t *testing.T) {
	var failFetch atomic.Bool
	fetch := subPresetFetchFunc(func(context.Context) (subpreset.Snapshot, error) {
		if failFetch.Load() {
			return subpreset.Snapshot{}, errors.New("unavailable")
		}
		return exampleSubPresetSnapshot(), nil
	})
	st, c, _, _ := newSubPresetRig(t, fetch)
	ctx := context.Background()
	active, _ := subdesign.Preset("basic")
	if err := st.SetSetting(ctx, store.SettingSubDesign, active); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, store.SettingSubTemplates, store.SubTemplates{"clash": "active"}); err != nil {
		t.Fatal(err)
	}
	if status, b, _ := c.do("POST", "/api/admin/settings/sub-design/presets/sync", nil, nil); status != 200 {
		t.Fatalf("sync: %d %s", status, b)
	}
	status, b, _ := c.do("GET", "/api/admin/settings/sub-design", nil, nil)
	if status != 200 {
		t.Fatal(status)
	}
	var catalogue struct {
		Presets []store.SubDesignPreset `json:"presets"`
		Design  subdesign.Design        `json:"design"`
	}
	if err := json.Unmarshal(b, &catalogue); err != nil {
		t.Fatal(err)
	}
	if len(catalogue.Presets) != 3 || catalogue.Presets[0].Key != "basic" || !catalogue.Presets[1].Supported || catalogue.Presets[2].Supported {
		t.Fatalf("unexpected catalogue: %s", b)
	}
	if strings.Contains(string(b), "enable_rule_generator") {
		t.Fatal("catalogue exposed upstream body")
	}
	if catalogue.Design.Final != active.Final {
		t.Fatal("sync changed draft")
	}
	if status, _, _ := c.do("GET", "/api/admin/settings/sub-design/presets/acl4ssr:Unsupported.ini", nil, nil); status != 422 {
		t.Fatal("unsupported preset was loadable", status)
	}
	status, b, _ = c.do("GET", "/api/admin/settings/sub-design/presets/acl4ssr:Example.ini", nil, nil)
	if status != 200 {
		t.Fatalf("load: %d %s", status, b)
	}
	imported := mustJSON[subdesign.Design](t, b)
	if imported.Source == nil || imported.Source.Revision != strings.Repeat("a", 40) {
		t.Fatal("lost revision")
	}
	status, b, _ = c.do("POST", "/api/admin/settings/sub-design/preview", map[string]any{"design": imported, "format": "clash"}, nil)
	if status != 200 || !strings.Contains(string(b), "CC-BY-SA") {
		t.Fatalf("preview lost attribution: %d %s", status, b)
	}
	failFetch.Store(true)
	if status, _, _ := c.do("POST", "/api/admin/settings/sub-design/presets/sync", nil, nil); status != 502 {
		t.Fatal("expected failed sync", status)
	}
	var cached store.SubPresetLibrary
	if err := st.GetSetting(ctx, store.SettingSubPresetLibrary, &cached); err != nil {
		t.Fatal(err)
	}
	if cached.Revision != strings.Repeat("a", 40) || len(cached.Items) != 2 {
		t.Fatal("failed sync replaced cached library")
	}
	var templates store.SubTemplates
	if err := st.GetSetting(ctx, store.SettingSubTemplates, &templates); err != nil {
		t.Fatal(err)
	}
	if templates["clash"] != "active" {
		t.Fatal("sync or preview changed published template")
	}
}

func TestSubDesignSyncReauthorizesWithoutHoldingAccounts(t *testing.T) {
	for _, revoke := range []string{"token", "role"} {
		t.Run(revoke, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			fetch := subPresetFetchFunc(func(ctx context.Context) (subpreset.Snapshot, error) {
				close(entered)
				select {
				case <-release:
					return exampleSubPresetSnapshot(), nil
				case <-ctx.Done():
					return subpreset.Snapshot{}, ctx.Err()
				}
			})
			st, c, uid, tid := newSubPresetRig(t, fetch)
			done := make(chan int, 1)
			go func() {
				status, _, _ := c.do("POST", "/api/admin/settings/sub-design/presets/sync", nil, nil)
				done <- status
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("sync not started")
			}
			// This is the real root HTTP handler, not just the endpoint function.
			if !st.Accounts.TryLock() {
				close(release)
				<-done
				t.Fatal("network fetch held Accounts lock")
			}
			if revoke == "token" {
				if err := st.DeleteAPIToken(context.Background(), uid, tid); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := st.DB().Exec(`UPDATE staff SET role='operator' WHERE id=?`, uid); err != nil {
					t.Fatal(err)
				}
			}
			st.Accounts.Unlock()
			close(release)
			if status := <-done; status != 403 {
				t.Fatalf("revoked authority committed: %d", status)
			}
			var cached store.SubPresetLibrary
			if err := st.GetSetting(context.Background(), store.SettingSubPresetLibrary, &cached); err != nil {
				t.Fatal(err)
			}
			if cached.Revision != "" {
				t.Fatal("revoked sync wrote library")
			}
		})
	}
}

func TestSubDesignPresetAuthorization(t *testing.T) {
	var calls atomic.Int32
	st, c, uid, _ := newSubPresetRig(t, func(context.Context) (subpreset.Snapshot, error) {
		calls.Add(1)
		return exampleSubPresetSnapshot(), nil
	})
	adminToken := c.token
	read, _, err := st.CreateAPIToken(context.Background(), uid, "read", "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	d, _ := subdesign.Preset("basic")
	writes := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/admin/settings/sub-design/presets/sync", nil},
		{"POST", "/api/admin/settings/sub-design/presets/custom", map[string]any{"name": "Example", "design": d}},
		{"DELETE", "/api/admin/settings/sub-design/presets/custom:missing", nil},
		{"POST", "/api/admin/settings/sub-design/apply", map[string]any{"design": d}},
	}
	c.token = read
	for _, v := range writes {
		if status, _, _ := c.do(v.method, v.path, v.body, nil); status != 403 {
			t.Fatalf("read token: %s: %d", v.path, status)
		}
	}
	c.token = adminToken
	if _, err := st.DB().Exec(`UPDATE staff SET role='operator' WHERE id=?`, uid); err != nil {
		t.Fatal(err)
	}
	for _, v := range writes {
		if status, _, _ := c.do(v.method, v.path, v.body, nil); status != 403 {
			t.Fatalf("operator: %s: %d", v.path, status)
		}
	}
	if status, _, _ := c.do("GET", "/api/admin/settings/sub-design", nil, nil); status != 403 {
		t.Fatal("operator read library", status)
	}
	c.token = ""
	if status, _, _ := c.do("POST", "/api/admin/settings/sub-design/presets/sync", nil, nil); status != 401 {
		t.Fatal("anonymous sync", status)
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized caller reached network")
	}
}

func TestSubDesignCustomSnapshotAndFormatApplication(t *testing.T) {
	st, c, _, _ := newSubPresetRig(t, func(context.Context) (subpreset.Snapshot, error) { return exampleSubPresetSnapshot(), nil })
	d, _ := subdesign.Preset("basic")
	d.Enhancements.DNSProtection = true
	d.Enhancements.Claude = true
	if err := st.SetSetting(context.Background(), store.SettingSubTemplates, store.SubTemplates{"surge": "retain", "clash": "before"}); err != nil {
		t.Fatal(err)
	}
	// Non-ASCII names must be validated after source metadata is filled too.
	status, b, _ := c.do("POST", "/api/admin/settings/sub-design/presets/custom", map[string]any{"name": strings.Repeat("例", 90), "design": d}, nil)
	if status != 200 {
		t.Fatalf("save-as: %d %s", status, b)
	}
	p := mustJSON[store.SubDesignPreset](t, b)
	status, b, _ = c.do("GET", "/api/admin/settings/sub-design/presets/"+p.Key, nil, nil)
	if status != 200 {
		t.Fatal(status)
	}
	loaded := mustJSON[subdesign.Design](t, b)
	if !loaded.Enhancements.DNSProtection || !loaded.Enhancements.Claude || loaded.Source.Key != p.Key {
		t.Fatal("custom snapshot lost options/source")
	}
	if status, b, _ := c.do("POST", "/api/admin/settings/sub-design/inspect", map[string]any{"design": loaded}, nil); status != 200 {
		t.Fatalf("inspect saved snapshot: %d %s", status, b)
	}
	if status, _, _ := c.do("POST", "/api/admin/settings/sub-design/apply", map[string]any{"design": loaded}, nil); status != 400 {
		t.Fatal("legacy all-format apply silently dropped enhancements", status)
	}
	if status, b, _ := c.do("POST", "/api/admin/settings/sub-design/apply", map[string]any{"design": loaded, "formats": []string{"clash"}}, nil); status != 200 {
		t.Fatalf("apply selected: %d %s", status, b)
	}
	var templates store.SubTemplates
	if err := st.GetSetting(context.Background(), store.SettingSubTemplates, &templates); err != nil {
		t.Fatal(err)
	}
	if templates["surge"] != "retain" || !strings.Contains(templates["clash"], "dns:") {
		t.Fatal("selective publish altered other format or missed DNS")
	}
	if status, _, _ := c.do("DELETE", "/api/admin/settings/sub-design/presets/"+p.Key, nil, nil); status != 200 {
		t.Fatal(status)
	}
	if status, _, _ := c.do("DELETE", "/api/admin/settings/sub-design/presets/basic", nil, nil); status != 400 {
		t.Fatal("deleted builtin", status)
	}
	var active subdesign.Design
	if err := st.GetSetting(context.Background(), store.SettingSubDesign, &active); err != nil {
		t.Fatal(err)
	}
	if !active.Enhancements.DNSProtection {
		t.Fatal("delete changed active design")
	}
}
