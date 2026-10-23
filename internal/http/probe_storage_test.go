package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/config"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestProbeStorageRoundTripPrivacyAndRestart(t *testing.T) {
	for _, tc := range []struct {
		endpoint, name string
		skew           time.Duration
	}{
		{"report", "current", 0}, {"beat", "current", 0},
		{"report", "behind", -24 * time.Hour}, {"beat", "behind", -24 * time.Hour},
		{"report", "ahead", 24 * time.Hour}, {"beat", "ahead", 24 * time.Hour},
	} {
		t.Run(tc.endpoint+"/"+tc.name, func(t *testing.T) {
			endpoint := tc.endpoint
			r := newRig(t)
			anon := &client{t: t, srv: r.srv}
			ctx := context.Background()
			path := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
			get := func(c *client, path string) []byte {
				t.Helper()
				code, b, _ := c.do("GET", path, nil, nil)
				if code != 200 {
					t.Fatalf("get %s: %d %s", path, code, b)
				}
				return b
			}
			settings := func(disk bool) {
				t.Helper()
				sections := []string{"cpu", "system", "history"}
				if disk {
					sections = append(sections, "disk")
				}
				code, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": true, "visibility": "public", "public_sections": sections}, nil)
				if code != 200 {
					t.Fatalf("settings: %d %s", code, b)
				}
			}
			settings(true)
			at := time.Now().Add(tc.skew).UnixMilli()
			fs := func(m string, total, used int64, kind string) map[string]any {
				return map[string]any{"id": m, "mount": m, "device": "/dev/example", "type": kind, "total": total, "used": used, "sampled_at": at}
			}
			pool := fs("/data", 4000, 40, "fuse.mergerfs")
			pool["pool"] = true
			pool["member_mounts"] = []string{"/mnt/a", "/mnt/b"}
			host := map[string]any{"disk_total": 500, "disk_used": 5, "valid": map[string]bool{"disk": true}, "resources": map[string]any{"epoch": "storage", "sequence": 1, "at": at, "filesystems": []any{fs("/", 500, 5, "ext4"), pool, fs("/mnt/a", 2000, 20, "ext4"), fs("/mnt/b", 2000, 20, "ext4")}, "processes": []any{map[string]any{"name": "private-process", "pid": 99}}}}
			code, b, _ := r.agent.do("POST", "/api/agent/"+endpoint, map[string]any{"version": "0.75.0", "host": host}, nil)
			want := 200
			if endpoint == "beat" {
				want = 204
			}
			if code != want {
				t.Fatalf("ingest: %d %s", code, b)
			}
			type view struct {
				Storage []store.PublicStorage `json:"storage"`
				Host    spec.SystemStatus     `json:"host"`
			}
			public := func() view {
				t.Helper()
				raw := get(anon, "/api/probe")
				for _, private := range []string{"/data", "/mnt/", "/dev/", "private-process", "member_mounts", "sampled_at"} {
					if strings.Contains(string(raw), private) {
						t.Fatalf("private data leaked: %s", raw)
					}
				}
				doc := mustJSON[struct {
					Nodes []view `json:"nodes"`
				}](t, raw)
				if len(doc.Nodes) != 1 {
					t.Fatalf("nodes: %s", raw)
				}
				return doc.Nodes[0]
			}
			v := public()
			if len(v.Storage) != 2 || v.Storage[0].Kind != "system" || v.Storage[1].Kind != "pool" || v.Storage[1].Total == nil || *v.Storage[1].Total != 4000 {
				t.Fatalf("auto: %+v", v.Storage)
			}
			private := mustJSON[struct {
				Candidates []store.StorageCandidate `json:"storage_candidates"`
			}](t, get(r.c, path))
			if len(private.Candidates) != 4 || private.Candidates[1].Mount != "/data" {
				t.Fatalf("private candidates: %+v", private)
			}
			selected := store.NodeStorage{Mode: "manual", Items: []store.NodeStorageItem{{Mount: "/data", Name: "Archive"}, {Mount: "/missing", Name: "Backup"}, {Mount: "/mnt/a", Name: "Disk A"}}}
			save := func(body map[string]any, want int) {
				t.Helper()
				code, b, _ := r.c.do("PUT", path, body, nil)
				if code != want {
					t.Fatalf("save: %d %s", code, b)
				}
			}
			save(map[string]any{"Storage": selected}, 200)
			check := func() {
				t.Helper()
				v := public()
				if len(v.Storage) != 3 || v.Storage[0].Name != "Archive" || v.Storage[0].Total == nil || *v.Storage[0].Total != 4000 || v.Storage[0].Used == nil || *v.Storage[0].Used != 40 || v.Storage[1].Total != nil || v.Host.DiskTotal != 0 || v.Host.Validity().Disk {
					t.Fatalf("manual privacy/missing: %+v", v)
				}
				history := mustJSON[struct {
					Points []store.StatPoint `json:"points"`
				}](t, get(anon, "/api/probe/nodes/"+itoa(r.nodeID)+"/history?range=24h"))
				for _, p := range history.Points {
					if p.DiskTotal != 0 || p.DiskUsed != 0 || p.Valid.Disk {
						t.Fatalf("root history leaked: %+v", p)
					}
				}
			}
			check()
			save(map[string]any{"Info": map[string]any{"note": "legacy"}}, 200)
			save(map[string]any{"Storage": nil}, 200)
			np, err := r.st.NodeProbe(ctx, r.nodeID)
			if err != nil || !reflect.DeepEqual(np.Storage, selected) {
				t.Fatalf("legacy write lost storage: %+v %v", np, err)
			}
			for _, bad := range []any{map[string]any{"mode": "bad"}, map[string]any{"mode": "manual", "items": []any{map[string]any{"mount": "/data", "name": "<script>"}}}, map[string]any{"mode": "manual", "items": []any{map[string]any{"mount": "/data"}, map[string]any{"mount": "/data"}}}} {
				save(map[string]any{"Storage": bad, "Hidden": true, "LimitBytes": 999}, 400)
			}
			np, err = r.st.NodeProbe(ctx, r.nodeID)
			if err != nil || np.Hidden || np.LimitBytes != 0 || !reflect.DeepEqual(np.Storage, selected) {
				t.Fatalf("invalid update was not atomic: %+v %v", np, err)
			}
			check()
			// Persisted configuration and raw additive report fields survive restart.
			r.srv.Close()
			cfg := config.Default()
			cfg.BaseURL = "http://example.com"
			r.st = store.New(r.st.DB())
			r.srv = httptest.NewServer(New(cfg, r.st, slog.Default()).Handler())
			t.Cleanup(r.srv.Close)
			r.c.srv, anon.srv = r.srv, r.srv
			check()
			settings(false)
			if v := public(); len(v.Storage) != 0 || v.Host.DiskTotal != 0 {
				t.Fatal("disk permission ignored")
			}
			settings(true)
			check()
			save(map[string]any{"Storage": store.NodeStorage{Mode: "manual", Items: []store.NodeStorageItem{}}}, 200)
			if v := public(); v.Storage == nil || len(v.Storage) != 0 || v.Host.Validity().Disk {
				t.Fatal("empty selection fell back to root")
			}
			save(map[string]any{"Storage": store.NodeStorage{Mode: "auto"}}, 200)
			if v := public(); len(v.Storage) != 2 || v.Host.DiskTotal != 500 {
				t.Fatal("auto reset failed")
			}
			// Anonymous visitors cannot retrieve the path-bearing candidate inventory.
			if code, _, _ := anon.do("GET", path, nil, nil); code != 401 && code != 403 {
				t.Fatalf("candidate inventory status: %d", code)
			}
			// An ordinary old agent still supplies exactly the root filesystem view.
			legacy := spec.SystemStatus{DiskTotal: 100, DiskUsed: 20}
			candidates := store.StorageCandidates(&legacy, time.Now(), time.Now())
			raw, _ := json.Marshal((store.NodeStorage{}).Public(candidates))
			if strings.Contains(string(raw), "mount") || len(candidates) != 1 {
				t.Fatalf("legacy fallback: %s", raw)
			}
		})
	}
}
