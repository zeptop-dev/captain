package http

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestBilingualCategoryAPIPartialEdits(t *testing.T) {
	r := newRig(t)
	base := "/api/admin/entry-categories"
	call := func(method, path string, in any, want int) []byte {
		t.Helper()
		code, body, _ := r.c.do(method, path, in, nil)
		if code != want {
			t.Fatalf("%s %s: %d %s", method, path, code, body)
		}
		return body
	}
	zh := mustJSON[domain.EntryCategory](t, call("POST", base, map[string]string{"NameZH": " 中文 ", "NameEN": ""}, 200))
	en := mustJSON[domain.EntryCategory](t, call("POST", base, map[string]string{"NameEN": "English"}, 200))
	if zh.Name != "中文" || zh.NameZH != "中文" || en.Name != "English" || en.NameZH != "" {
		t.Fatal(zh, en)
	}
	path := base + "/" + itoa(zh.ID)
	bilingual := mustJSON[domain.EntryCategory](t, call("PATCH", path, map[string]string{"NameEN": "Chinese"}, 200))
	if bilingual.NameZH != "中文" || bilingual.NameEN != "Chinese" || bilingual.Name != "中文" {
		t.Fatal(bilingual)
	}
	for _, payload := range []map[string]string{{"Description": "Updated"}, {"Name": "Legacy rename"}} {
		got := mustJSON[domain.EntryCategory](t, call("PATCH", path, payload, 200))
		if got.NameZH != "中文" || got.NameEN != "Chinese" {
			t.Fatal("omitted translation discarded", got)
		}
	}
	for _, payload := range []map[string]string{{"NameZH": "", "NameEN": ""}, {"NameZH": "bad\nlabel"}, {"NameEN": "<markup>"}} {
		call("PATCH", path, payload, 400)
	}
	got, err := r.st.EntryCategoryByID(context.Background(), zh.ID)
	if err != nil || got.NameZH != "中文" || got.NameEN != "Chinese" || got.Description != "Updated" {
		t.Fatal("invalid partial write", got, err)
	}
	cleared := mustJSON[domain.EntryCategory](t, call("PATCH", path, map[string]string{"NameZH": " "}, 200))
	if cleared.NameZH != "" || cleared.NameEN != "Chinese" || cleared.Name != "Chinese" {
		t.Fatal(cleared)
	}
	call("PATCH", path, map[string]string{"NameEN": ""}, 400)
	call("POST", base, map[string]string{"Name": "Legacy fallback", "NameZH": "", "NameEN": ""}, 400)
	call("POST", base, map[string]string{"NameEN": "english"}, 409)
	// Translations can coincide while the legacy identity Name remains distinct.
	for _, payload := range []map[string]string{{"NameZH": "独立甲", "NameEN": "Shared"}, {"NameZH": "独立乙", "NameEN": "Shared"}} {
		call("POST", base, payload, 200)
	}
}

func TestBilingualGroupsPublicProjectionKeepsIdentityAndPrivacy(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	second := &domain.Node{Name: "Second"}
	hidden := &domain.Node{Name: "Hidden"}
	for _, n := range []*domain.Node{second, hidden} {
		if err := r.st.CreateNode(ctx, n, n.Name, time.Hour); err != nil {
			t.Fatal(err)
		}
		if _, err := r.st.RedeemPairCode(ctx, n.Name, n.Name, "host", "v1.0.0", "linux"); err != nil {
			t.Fatal(err)
		}
	}
	state := func() agentproto.State {
		t.Helper()
		code, body, _ := r.agent.do("GET", "/api/agent/state", nil, nil)
		if code != 200 {
			t.Fatalf("agent state %d %s", code, body)
		}
		return mustJSON[agentproto.State](t, body)
	}
	before := state()
	layout, err := r.st.NodeLayout(ctx)
	if err != nil {
		t.Fatal(err)
	}
	layout.Groups = []string{"first-key", "second-key", "hidden-key", "empty-key"}
	layout.GroupNames = map[string]store.DisplayGroupName{"first-key": {ZH: "亚洲", EN: "Asia"}, "second-key": {ZH: "亚洲", EN: "Asia"}, "hidden-key": {ZH: "秘密分组", EN: "Secret group"}, "empty-key": {EN: "Secret empty group"}}
	layout.Nodes = []store.NodePlacement{{ID: r.nodeID, Group: "first-key"}, {ID: second.ID, Group: "second-key"}, {ID: hidden.ID, Group: "hidden-key"}}
	if err := r.st.SaveNodeLayout(ctx, layout); err != nil {
		t.Fatal(err)
	}
	if err := r.st.UpdateNodeProbe(ctx, hidden.ID, true, store.NodeProbeInfo{}, 0, 1, "sum"); err != nil {
		t.Fatal(err)
	}
	after := state()
	if before.Revision != after.Revision || before.ConfigRevision != after.ConfigRevision {
		t.Fatal("display groups changed agent configuration")
	}
	configureKomariProbe(t, r, "public")
	type node struct {
		Group string `json:"group"`
		ZH    string `json:"group_zh"`
		EN    string `json:"group_en"`
	}
	for _, path := range []string{"/api/probe", "/api/probe/v2", "/api/probe/v2/nodes"} {
		code, body, _ := probeRequest(t, r, "GET", path, "test", nil, nil, nil)
		if code != 200 {
			t.Fatalf("%s %d %s", path, code, body)
		}
		got := mustJSON[struct {
			Nodes []node `json:"nodes"`
		}](t, body)
		want := []node{{Group: "first-key", ZH: "亚洲", EN: "Asia"}, {Group: "second-key", ZH: "亚洲", EN: "Asia"}}
		if !reflect.DeepEqual(got.Nodes, want) {
			t.Fatalf("%s lost identity/labels: %s", path, body)
		}
		for _, private := range []string{"Secret", "秘密", "hidden-key", "empty-key", "group_names"} {
			if bytes.Contains(body, []byte(private)) {
				t.Fatalf("%s leaked %q", path, private)
			}
		}
	}
	for _, path := range []string{"/api/admin/nodes", "/api/admin/nodes/" + itoa(r.nodeID)} {
		code, body, _ := r.c.do("GET", path, nil, nil)
		if code != 200 || !bytes.Contains(body, []byte(`"monitor_group":"first-key"`)) || !bytes.Contains(body, []byte(`"monitor_group_zh":"亚洲"`)) || !bytes.Contains(body, []byte(`"monitor_group_en":"Asia"`)) {
			t.Fatalf("admin labels %s: %d %s", path, code, body)
		}
	}
}
