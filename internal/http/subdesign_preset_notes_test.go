package http

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/subpreset"
)

func TestSubDesignPresetNotesFromCachedSource(t *testing.T) {
	var downloads atomic.Int32
	st, c, _, _ := newSubPresetRig(t, func(context.Context) (subpreset.Snapshot, error) {
		downloads.Add(1)
		return exampleSubPresetSnapshot(), nil
	})
	ctx := context.Background()
	// This is an existing library without a description field. The original
	// source is sufficient; reading notes must not require another sync or write.
	library := store.SubPresetLibrary{Revision: strings.Repeat("a", 40), SyncedAt: 1, Items: []store.SubDesignPreset{
		{Key: "acl4ssr:Example.ini", Name: "Example", Source: "acl4ssr", Supported: true, Content: "[custom]\n; Read this cached note.\nruleset=Proxy,[]FINAL\n; Excluded implementation comment\n"},
		{Key: "acl4ssr:Unsupported.ini", Name: "Unsupported", Source: "acl4ssr", Supported: false, Reason: "unsupported directive", Content: "; Explanation of the unsupported example.\n[custom]\nscript=refused\n"},
		{Key: "acl4ssr:Empty.ini", Name: "Empty", Source: "acl4ssr", Supported: true, Content: "[custom]\nruleset=Proxy,[]FINAL\n"},
	}}
	if err := st.SetSetting(ctx, store.SettingSubPresetLibrary, library); err != nil {
		t.Fatal(err)
	}
	custom := []store.SubDesignPreset{{Key: "custom:example", Name: "Local example", Source: "custom", URL: "https://github.com/ACL4SSR/ACL4SSR", Content: "; A custom note cannot claim to be official.\n", Supported: true}}
	if err := st.SetSetting(ctx, store.SettingSubCustomPresets, custom); err != nil {
		t.Fatal(err)
	}
	status, body, _ := c.do("GET", "/api/admin/settings/sub-design", nil, nil)
	if status != 200 {
		t.Fatalf("catalogue: %d %s", status, body)
	}
	var response struct {
		Presets []map[string]json.RawMessage `json:"presets"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Presets) != 5 {
		t.Fatalf("incorrect catalogue size: %d", len(response.Presets))
	}
	want := map[string]string{
		"acl4ssr:Example.ini":     "Read this cached note.",
		"acl4ssr:Unsupported.ini": "Explanation of the unsupported example.",
	}
	for _, item := range response.Presets {
		var key, notes string
		if err := json.Unmarshal(item["key"], &key); err != nil {
			t.Fatal(err)
		}
		if raw, exists := item["official_notes"]; exists {
			if err := json.Unmarshal(raw, &notes); err != nil {
				t.Fatal(err)
			}
			if want[key] == "" {
				t.Fatalf("unexpected official notes on %s", key)
			}
		}
		if notes != want[key] {
			t.Fatalf("notes for %s = %q, want %q", key, notes, want[key])
		}
		if _, exists := item["content"]; exists {
			t.Fatal("catalogue leaked raw source")
		}
	}
	if downloads.Load() != 0 || strings.Contains(string(body), "Excluded implementation") {
		t.Fatal("catalogue downloaded sources or exposed later comments")
	}
	var retained store.SubPresetLibrary
	if err := st.GetSetting(ctx, store.SettingSubPresetLibrary, &retained); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(library)
	after, _ := json.Marshal(retained)
	if string(before) != string(after) {
		t.Fatal("reading notes rewrote the cached snapshot")
	}
}
