package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/clauderules"
	"github.com/zeptop-dev/captain/internal/store"
)

const claudeRulesPath = "/api/admin/settings/sub-design/claude-rules"

type claudeFetchFunc func(context.Context) (clauderules.Snapshot, error)

func (f claudeFetchFunc) Fetch(ctx context.Context) (clauderules.Snapshot, error) { return f(ctx) }
func testClaudeSnapshot(revision string) clauderules.Snapshot {
	return clauderules.Snapshot{Revision: strings.Repeat(revision, 40), SyncedAt: time.Now().Unix(), Rules: []clauderules.Rule{{Type: "domain", Value: revision + ".example.com"}, {Type: "full", Value: "exact.example.com"}}}
}

type claudeStateResponse struct {
	Library store.ClaudeRuleLibrary `json:"library"`
	Applied store.ClaudeRuleApplied `json:"applied"`
	Added   []subdesign.ClaudeRule  `json:"added"`
	Removed []subdesign.ClaudeRule  `json:"removed"`
}

func getClaudeState(t *testing.T, c *client) claudeStateResponse {
	t.Helper()
	status, body, _ := c.do("GET", claudeRulesPath, nil, nil)
	if status != 200 {
		t.Fatalf("state: %d %s", status, body)
	}
	return mustJSON[claudeStateResponse](t, body)
}

func TestClaudeRulesSyncDraftAndPublicationIsolation(t *testing.T) {
	var phase atomic.Int32
	st, c, _, _ := newSubDesignRig(t, Options{ClaudeRules: claudeFetchFunc(func(context.Context) (clauderules.Snapshot, error) {
		switch phase.Load() {
		case 1:
			return testClaudeSnapshot("b"), nil
		case 2:
			return clauderules.Snapshot{}, errors.New("unavailable")
		}
		return testClaudeSnapshot("a"), nil
	})})
	ctx := context.Background()
	d, _ := subdesign.Preset("basic")
	d.Enhancements.Claude = true
	if err := st.ApplySubDesign(ctx, &d, map[string]string{"clash": "legacy-active"}); err != nil {
		t.Fatal(err)
	}
	state := getClaudeState(t, c)
	if state.Library.Snapshot.Source != "builtin" || state.Applied.Snapshot.Source != "builtin" {
		t.Fatal("missing builtin fallback")
	}
	status, body, _ := c.do("POST", claudeRulesPath+"/sync", nil, nil)
	if status != 200 {
		t.Fatalf("sync: %d %s", status, body)
	}
	result := mustJSON[claudeStateResponse](t, body)
	if len(result.Added) != 2 || len(result.Removed) != 8 || len(result.Library.Snapshot.Rules) != 4 {
		t.Fatalf("unexpected diff %s", body)
	}
	var draft subdesign.Design
	if err := st.GetSetting(ctx, store.SettingSubDesign, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Enhancements.ClaudeRules != nil {
		t.Fatal("sync silently adopted into draft")
	}
	state = getClaudeState(t, c)
	if state.Applied.Snapshot.Source != "builtin" {
		t.Fatal("sync changed applied version")
	}
	d.Enhancements.ClaudeRules = state.Library.Snapshot
	d.Enhancements.DNSProtection = true
	status, body, _ = c.do("POST", "/api/admin/settings/sub-design/presets/custom", map[string]any{"name": "Pinned Claude", "design": d}, nil)
	if status != 200 {
		t.Fatalf("save custom: %d %s", status, body)
	}
	preset := mustJSON[store.SubDesignPreset](t, body)
	status, body, _ = c.do("POST", "/api/admin/settings/sub-design/apply", map[string]any{"design": d, "formats": []string{"clash"}}, nil)
	if status != 200 {
		t.Fatalf("apply: %d %s", status, body)
	}
	var before store.SubTemplates
	if err := st.GetSetting(ctx, store.SettingSubTemplates, &before); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before["clash"], "a.example.com") || !strings.Contains(before["clash"], "exact.example.com") {
		t.Fatal("snapshot absent from generated template")
	}
	phase.Store(1)
	if status, body, _ := c.do("POST", claudeRulesPath+"/sync", nil, nil); status != 200 {
		t.Fatalf("resync: %d %s", status, body)
	}
	state = getClaudeState(t, c)
	if state.Library.Snapshot.Revision != strings.Repeat("b", 40) || state.Applied.Snapshot.Revision != strings.Repeat("a", 40) {
		t.Fatal("library and applied collapsed", state)
	}
	status, body, _ = c.do("GET", "/api/admin/settings/sub-design/presets/"+preset.Key, nil, nil)
	if status != 200 || mustJSON[subdesign.Design](t, body).Enhancements.ClaudeRules.Revision != strings.Repeat("a", 40) {
		t.Fatal("custom preset not pinned")
	}
	var after store.SubTemplates
	if err := st.GetSetting(ctx, store.SettingSubTemplates, &after); err != nil {
		t.Fatal(err)
	}
	if after["clash"] != before["clash"] {
		t.Fatal("sync changed published template")
	}
	phase.Store(2)
	if status, _, _ := c.do("POST", claudeRulesPath+"/sync", nil, nil); status != 502 {
		t.Fatal("expected failed sync", status)
	}
	if state = getClaudeState(t, c); state.Library.Snapshot.Revision != strings.Repeat("b", 40) {
		t.Fatal("failed sync lost cache")
	}
}

func TestClaudeRuleSyncAuthorization(t *testing.T) {
	var calls atomic.Int32
	st, c, uid, _ := newSubDesignRig(t, Options{ClaudeRules: claudeFetchFunc(func(context.Context) (clauderules.Snapshot, error) { calls.Add(1); return testClaudeSnapshot("a"), nil })})
	admin := c.token
	read, _, err := st.CreateAPIToken(context.Background(), uid, "read", "read", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.token = read
	if status, _, _ := c.do("POST", claudeRulesPath+"/sync", nil, nil); status != 403 {
		t.Fatal("read token allowed sync", status)
	}
	c.token = admin
	if _, err = st.DB().Exec(`UPDATE staff SET role='operator' WHERE id=?`, uid); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "POST"} {
		path := claudeRulesPath
		if method == "POST" {
			path += "/sync"
		}
		if status, _, _ := c.do(method, path, nil, nil); status != 403 {
			t.Fatal("operator accessed Claude rules", status)
		}
	}
	c.token = ""
	if status, _, _ := c.do("POST", claudeRulesPath+"/sync", nil, nil); status != 401 {
		t.Fatal("anonymous sync", status)
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized request triggered network fetch")
	}
}

func TestClaudeRuleSyncReauthorizesAndRejectsOverlap(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	st, c, uid, tid := newSubDesignRig(t, Options{ClaudeRules: claudeFetchFunc(func(ctx context.Context) (clauderules.Snapshot, error) {
		close(entered)
		select {
		case <-release:
			return testClaudeSnapshot("a"), nil
		case <-ctx.Done():
			return clauderules.Snapshot{}, ctx.Err()
		}
	})})
	defer close(release)
	done := make(chan int, 1)
	go func() { status, _, _ := c.do("POST", claudeRulesPath+"/sync", nil, nil); done <- status }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sync not started")
	}
	if status, _, _ := c.do("POST", claudeRulesPath+"/sync", nil, nil); status != http.StatusConflict {
		t.Fatal("overlapping sync allowed", status)
	}
	if !st.Accounts.TryLock() {
		t.Fatal("fetch blocked account edits")
	}
	err := st.DeleteAPIToken(context.Background(), uid, tid)
	st.Accounts.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	if status := <-done; status != 403 {
		t.Fatal("revoked token committed rules", status)
	}
	var library store.ClaudeRuleLibrary
	if err := st.GetSetting(context.Background(), store.SettingClaudeRuleLibrary, &library); err != nil {
		t.Fatal(err)
	}
	if library.Snapshot != nil {
		t.Fatal("revoked request wrote cache")
	}
}

func TestClaudeRuleSyncRejectsInvalidSnapshotsWithoutReplacingCache(t *testing.T) {
	for _, name := range []string{"empty", "unsupported", "bad-revision", "duplicate", "timestamp"} {
		t.Run(name, func(t *testing.T) {
			invalid := testClaudeSnapshot("a")
			switch name {
			case "empty":
				invalid.Rules = nil
			case "unsupported":
				invalid.Rules[0].Type = "regexp"
			case "bad-revision":
				invalid.Revision = "../master"
			case "duplicate":
				invalid.Rules = append(invalid.Rules, invalid.Rules[0])
			case "timestamp":
				invalid.SyncedAt = 0
			}
			st, c, _, _ := newSubDesignRig(t, Options{ClaudeRules: claudeFetchFunc(func(context.Context) (clauderules.Snapshot, error) { return invalid, nil })})
			old := store.ClaudeRuleLibrary{Snapshot: subdesign.BuiltinClaudeRules(), SyncedAt: 1}
			if err := st.SetSetting(context.Background(), store.SettingClaudeRuleLibrary, old); err != nil {
				t.Fatal(err)
			}
			if status, _, _ := c.do("POST", claudeRulesPath+"/sync", nil, nil); status != 502 {
				t.Fatal("invalid fetched snapshot accepted", status)
			}
			state := getClaudeState(t, c)
			if state.Library.SyncedAt != 1 || state.Library.Snapshot.Source != "builtin" {
				t.Fatal("invalid snapshot replaced cache")
			}
		})
	}
}

func TestClaudePinnedRulesReachCustomerSubscription(t *testing.T) {
	r := newRig(t)
	_, path, _ := profileSetup(t, r)
	d, _ := subdesign.Preset("basic")
	d.Enhancements.Claude = true
	d.Enhancements.DNSProtection = true
	d.Enhancements.ClaudeRules = &subdesign.ClaudeRuleSnapshot{Source: "v2fly", Revision: strings.Repeat("a", 40), Rules: []subdesign.ClaudeRule{{Type: "domain", Value: "ai.example.com"}, {Type: "full", Value: "exact.example.com"}}}
	if status, body, _ := r.c.do("POST", "/api/admin/settings/sub-design/apply", map[string]any{"design": d, "formats": []string{"clash"}}, nil); status != 200 {
		t.Fatalf("apply: %d %s", status, body)
	}
	visitor := &client{t: t, srv: r.srv}
	status, body, _ := visitor.do("GET", path+"?client=clash", nil, nil)
	if status != 200 {
		t.Fatalf("subscription: %d %s", status, body)
	}
	doc := decodeSubDesignDelivery(t, body)
	rules := strings.Join(doc.Rules, "\n")
	if !strings.Contains(rules, "DOMAIN-SUFFIX,ai.example.com,🤖 Claude") || !strings.Contains(rules, "DOMAIN,exact.example.com,🤖 Claude") || strings.Contains(rules, "DOMAIN-SUFFIX,anthropic.com,🤖 Claude") {
		t.Fatal("subscription did not use pinned rules")
	}
	if len(doc.DNS.NameserverPolicy["+.ai.example.com"]) == 0 || len(doc.DNS.NameserverPolicy["exact.example.com"]) == 0 || len(doc.DNS.NameserverPolicy["+.exact.example.com"]) != 0 {
		t.Fatal("DNS changed domain matching semantics")
	}
	if !strings.Contains(string(body), "Copyright (c) 2018-2019 V2Ray") || !strings.Contains(string(body), strings.Repeat("a", 40)) {
		t.Fatal("subscription lost community attribution")
	}
}
