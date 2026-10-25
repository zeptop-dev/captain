package admin

import (
	"context"
	"net/http"
	"sort"

	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/clauderules"
	"github.com/zeptop-dev/captain/internal/store"
)

type claudeLibraryInfo struct {
	store.ClaudeRuleLibrary
	Count      int    `json:"count"`
	URL        string `json:"url"`
	LicenseURL string `json:"license_url"`
}

func claudeLibrary(library store.ClaudeRuleLibrary) claudeLibraryInfo {
	info := claudeLibraryInfo{ClaudeRuleLibrary: library, Count: len(library.Snapshot.Rules), URL: clauderules.SourceURL, LicenseURL: clauderules.LicenseURL}
	if library.Snapshot.Source == "v2fly" {
		base := "https://github.com/v2fly/domain-list-community/blob/" + library.Snapshot.Revision + "/"
		info.URL, info.LicenseURL = base+"data/anthropic", base+"LICENSE"
	}
	return info
}

func (h *handlers) getClaudeRules(w http.ResponseWriter, r *http.Request) {
	library, applied, err := h.Store.ClaudeRulesFormatsState(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]any{"library": claudeLibrary(library), "builtin": subdesign.BuiltinClaudeRules(), "applied": applied["clash"], "applied_formats": applied})
}

func (h *handlers) syncClaudeRules(w http.ResponseWriter, r *http.Request) {
	authorized := false
	h.Store.Accounts.RLock()
	h.requireAdmin(func(_ http.ResponseWriter, _ *http.Request) { authorized = true })(w, r.WithContext(context.WithValue(r.Context(), subPresetAuthOnlyKey{}, true)))
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	finish := func(next http.HandlerFunc) {
		// Ordinary staff mutations hold the shared lock. Take the exclusive
		// lock here so final authorization and publication cannot straddle
		// a concurrent revocation after the remote fetch has completed.
		h.Store.Accounts.Lock()
		defer h.Store.Accounts.Unlock()
		// Recheck after I/O so reset, revocation or demotion cannot authorize a write.
		h.requireAdmin(next)(w, r)
	}
	if !h.claudeRuleSync.TryLock() {
		finish(func(w http.ResponseWriter, _ *http.Request) {
			fail(w, 409, "Claude rule synchronization is already running")
		})
		return
	}
	defer h.claudeRuleSync.Unlock()
	fetched, err := h.ClaudeRules.Fetch(r.Context())
	if err != nil {
		finish(func(w http.ResponseWriter, _ *http.Request) {
			fail(w, 502, "Claude rule synchronization failed; the cached library was retained")
		})
		return
	}
	snapshot := &subdesign.ClaudeRuleSnapshot{Source: "v2fly", Revision: fetched.Revision, Rules: make([]subdesign.ClaudeRule, 0, len(fetched.Rules)+2)}
	for _, rule := range fetched.Rules {
		snapshot.Rules = append(snapshot.Rules, subdesign.ClaudeRule{Type: rule.Type, Value: rule.Value})
	}
	// Keep only Captain's explicit supplements; upstream removals must not be
	// undone by merging the entire legacy builtin list on every synchronization.
	for _, host := range []string{"anthropic.auth0.com", "anthropic.com.cdn.cloudflare.net"} {
		candidate := subdesign.ClaudeRule{Type: "full", Value: host}
		found := false
		for _, rule := range snapshot.Rules {
			if rule == candidate {
				found = true
				break
			}
		}
		if !found {
			snapshot.Rules = append(snapshot.Rules, candidate)
		}
	}
	sort.Slice(snapshot.Rules, func(i, j int) bool {
		a, b := snapshot.Rules[i], snapshot.Rules[j]
		if a.Type == b.Type {
			return a.Value < b.Value
		}
		return a.Type < b.Type
	})
	if err := subdesign.ValidateClaudeRules(snapshot); err != nil || len(fetched.Rules) == 0 || fetched.SyncedAt <= 0 {
		finish(func(w http.ResponseWriter, _ *http.Request) {
			fail(w, 502, "Claude rule synchronization returned invalid data; the cached library was retained")
		})
		return
	}
	library := store.ClaudeRuleLibrary{Snapshot: snapshot, SyncedAt: fetched.SyncedAt}
	finish(func(w http.ResponseWriter, r *http.Request) {
		previous, _, err := h.Store.ClaudeRulesState(r.Context())
		if err != nil {
			serverErr(w, err)
			return
		}
		if err := h.Store.SetSetting(r.Context(), store.SettingClaudeRuleLibrary, library); err != nil {
			serverErr(w, err)
			return
		}
		added, removed := claudeRuleDiff(previous.Snapshot.Rules, snapshot.Rules)
		ok(w, map[string]any{"library": claudeLibrary(library), "added": added, "removed": removed, "previous_revision": previous.Snapshot.Revision})
	})
}

func claudeRuleDiff(before, after []subdesign.ClaudeRule) (added, removed []subdesign.ClaudeRule) {
	oldSet, newSet := map[subdesign.ClaudeRule]bool{}, map[subdesign.ClaudeRule]bool{}
	for _, rule := range before {
		oldSet[rule] = true
	}
	for _, rule := range after {
		newSet[rule] = true
	}
	added, removed = []subdesign.ClaudeRule{}, []subdesign.ClaudeRule{}
	for _, rule := range after {
		if !oldSet[rule] {
			added = append(added, rule)
		}
	}
	for _, rule := range before {
		if !newSet[rule] {
			removed = append(removed, rule)
		}
	}
	return
}
