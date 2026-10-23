package http

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

func TestProbeStorageHistoryHTTPPermissionsAndProjection(t *testing.T) {
	r := newRig(t)
	anon := &client{t: t, srv: r.srv}
	endpoint := "/api/probe/nodes/" + itoa(r.nodeID) + "/storage-history"
	configPath := "/api/admin/nodes/" + itoa(r.nodeID) + "/probe"
	settings := func(page bool, visibility string, sections ...string) {
		t.Helper()
		code, b, _ := r.c.do("PUT", "/api/admin/settings/probe", map[string]any{"enabled": true, "page_enabled": page, "visibility": visibility, "public_sections": sections}, nil)
		if code != 200 {
			t.Fatalf("settings: %d %s", code, b)
		}
	}
	settings(true, "public", "disk", "history")
	at := time.Now().UnixMilli()
	host := map[string]any{"disk_total": 500, "disk_used": 5, "valid": map[string]bool{"disk": true}, "resources": map[string]any{
		"epoch": "storage", "sequence": 1, "at": at,
		"filesystems": []any{map[string]any{"id": "private-volume", "mount": "/private/data", "device": "/dev/example", "type": "fuse.mergerfs", "pool": true, "member_mounts": []string{"/private/member"}, "total": 4000, "used": 40, "sampled_at": at}},
	}}
	code, b, _ := r.agent.do("POST", "/api/agent/report", map[string]any{"version": "0.76.0", "host": host}, nil)
	if code != 200 {
		t.Fatalf("report: %d %s", code, b)
	}
	get := func(c *client, suffix string, status int) store.PublicStorageHistory {
		t.Helper()
		code, b, headers := c.do("GET", endpoint+suffix, nil, nil)
		if code != status {
			t.Fatalf("history %s: %d %s", suffix, code, b)
		}
		if status != 200 {
			return store.PublicStorageHistory{}
		}
		if headers.Get("Cache-Control") != "no-store" {
			t.Fatal("history response can be cached after visibility changes")
		}
		for _, private := range []string{"/private/", "/dev/", "private-volume", "member_mounts", "sampled_at", "filesystem:"} {
			if strings.Contains(string(b), private) {
				t.Fatalf("private data leaked: %s", b)
			}
		}
		return mustJSON[store.PublicStorageHistory](t, b)
	}
	// A normal report updates the snapshot only; it must not fabricate a
	// historical capacity curve before the monitoring beat is persisted.
	for _, volume := range get(anon, "", 200).Volumes {
		for _, p := range volume.Points {
			if p.Used != nil || p.Total != nil {
				t.Fatal("snapshot was synthesized as historical data")
			}
		}
	}
	code, b, _ = r.agent.do("POST", "/api/agent/beat", map[string]any{"version": "0.76.0", "host": host}, nil)
	if code != 204 {
		t.Fatalf("beat: %d %s", code, b)
	}
	check := func(h store.PublicStorageHistory, volumes int, dataIndex int) {
		t.Helper()
		if len(h.Volumes) != volumes {
			t.Fatalf("wrong volumes: %+v", h)
		}
		found := false
		for _, p := range h.Volumes[dataIndex].Points {
			if p.Used != nil {
				if *p.Used != 40 || p.Total == nil || *p.Total != 4000 {
					t.Fatalf("wrong data-volume bytes: %+v", p)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("actual data-volume history missing")
		}
	}
	for _, span := range []string{"1h", "4h", "24h", "7d", "30d"} {
		check(get(anon, "?range="+span, 200), 2, 1)
	}
	check(get(anon, fmt.Sprintf("?from=%d&to=%d", time.Now().Add(-time.Hour).Unix(), time.Now().Unix()), 200), 2, 1)
	for _, suffix := range []string{"?range=unbounded", "?from=0&to=1", "?from=1", "?from=word&to=2", "?from=1&to=99999999999", "?from=2&to=1"} {
		get(anon, suffix, 400)
	}
	save := func(hidden bool, items []store.NodeStorageItem) {
		t.Helper()
		code, b, _ := r.c.do("PUT", configPath, map[string]any{"Hidden": hidden, "Storage": store.NodeStorage{Mode: "manual", Items: items}}, nil)
		if code != 200 {
			t.Fatalf("save: %d %s", code, b)
		}
	}
	selected := []store.NodeStorageItem{{Mount: "/missing", Name: "Backup"}, {Mount: "/private/data", Name: "Archive"}}
	save(false, selected)
	h := get(anon, "", 200)
	check(h, 2, 1)
	if h.Volumes[0].Name != "Backup" || h.Volumes[1].Name != "Archive" || h.Volumes[1].ID != "storage-2" {
		t.Fatalf("current selection/order lost: %+v", h)
	}
	for _, p := range h.Volumes[0].Points {
		if p.Used != nil || p.Total != nil {
			t.Fatal("missing volume received root history")
		}
	}
	save(false, []store.NodeStorageItem{})
	if h = get(anon, "", 200); h.Volumes == nil || len(h.Volumes) != 0 {
		t.Fatal("empty selection revealed history")
	}
	save(true, selected)
	get(anon, "", 404)
	check(get(r.c, "", 200), 2, 1)
	save(false, selected)
	settings(true, "public", "history")
	get(anon, "", 404)
	settings(true, "public", "disk")
	get(anon, "", 404)
	settings(false, "public", "disk", "history")
	get(anon, "", 404)
	settings(true, "users", "disk", "history")
	get(anon, "", 401)
	settings(true, "admins", "disk", "history")
	get(anon, "", 403)
	check(get(r.c, "", 200), 2, 1)
	settings(true, "public", "disk", "history")
	if err := r.st.ResetPairCode(context.Background(), r.nodeID, "NEWPAIR", time.Hour); err != nil {
		t.Fatal(err)
	}
	get(anon, "", 404)
}
