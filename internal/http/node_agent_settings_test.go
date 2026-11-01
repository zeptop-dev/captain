package http

import "testing"

func TestNodeAgentSettingsCreateEditAndRepair(t *testing.T) {
	r := newRig(t)
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		code, b, _ := r.c.do(method, path, body, nil)
		if code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, path, code, want, b)
		}
		return b
	}
	type view struct {
		ID                int64  `json:"id"`
		AgentURL          string `json:"agent_url"`
		EffectiveAgentURL string `json:"effective_agent_url"`
		DownloadSource    string `json:"download_source"`
		PairCode          string `json:"pair_code"`
	}
	created := mustJSON[view](t, call("POST", "/api/admin/nodes", map[string]any{"Name": "Private node", "AgentURL": "https://10.10.0.2:18443/", "DownloadSource": "captain"}, 200))
	if created.AgentURL != "https://10.10.0.2:18443" || created.EffectiveAgentURL != created.AgentURL || created.DownloadSource != "captain" || created.PairCode == "" {
		t.Fatalf("created %+v", created)
	}
	path := "/api/admin/nodes/" + itoa(created.ID)
	call("POST", "/api/admin/nodes", map[string]any{"Name": "Plaintext default", "DownloadSource": "captain"}, 400)
	check := func(wantURL, wantSource string) view {
		t.Helper()
		out := mustJSON[struct {
			Node view `json:"node"`
		}](t, call("GET", path, nil, 200)).Node
		if out.AgentURL != wantURL || out.DownloadSource != wantSource {
			t.Fatalf("got %+v", out)
		}
		return out
	}
	// Omitted and null additive values preserve installation settings.
	call("PATCH", path, map[string]any{"Name": "Renamed"}, 200)
	check(created.AgentURL, "captain")
	call("PATCH", path, map[string]any{"Name": "Renamed", "AgentURL": nil, "DownloadSource": nil}, 200)
	check(created.AgentURL, "captain")
	for _, update := range []map[string]any{
		{"Name": "Invalid", "AgentURL": "https://example.com/path"},
		{"Name": "Invalid", "AgentURL": "http://10.10.0.2:8080"},
		{"Name": "Invalid", "DownloadSource": "https://example.com"},
		{"Name": "Invalid", "AgentURL": ""}, // would fall back to the HTTP panel origin
	} {
		call("PATCH", path, update, 400)
		check(created.AgentURL, "captain")
	}
	repair := mustJSON[view](t, call("POST", path+"/repair", nil, 200))
	if repair.PairCode == "" || repair.EffectiveAgentURL != created.AgentURL || repair.DownloadSource != "captain" {
		t.Fatalf("repair %+v", repair)
	}
	call("PATCH", path, map[string]any{"Name": "Default", "AgentURL": "", "DownloadSource": "github"}, 200)
	if got := check("", "github"); got.EffectiveAgentURL == "" || got.EffectiveAgentURL == created.AgentURL {
		t.Fatalf("default endpoint missing: %+v", got)
	}
	legacy := mustJSON[view](t, call("POST", "/api/admin/nodes", map[string]any{"Name": "Legacy"}, 200))
	if legacy.AgentURL != "" || legacy.DownloadSource != "github" || legacy.EffectiveAgentURL == "" {
		t.Fatalf("legacy %+v", legacy)
	}
	call("POST", "/api/admin/nodes", map[string]any{"Name": "Bad source", "DownloadSource": "mirror"}, 400)
}
