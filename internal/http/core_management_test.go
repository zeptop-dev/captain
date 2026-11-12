package http

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/http/admin"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestCoreManagementPermissionsExpiryAndReport(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	base := "/api/admin/nodes/" + itoa(r.nodeID)
	request := spec.CoreRequest{Action: "activate", Distribution: "singbox-extended", Version: "1.14.1-extended-2.7.2-r1", Revision: 1}
	body := map[string]any{"kind": spec.CoreManagementKind, "params": request}
	if code, _, _ := r.c.do("POST", base+"/jobs", body, nil); code != 409 {
		t.Fatal("unreported capability accepted", code)
	}
	inventory := &spec.CoreInventory{Revision: 1, Packages: []spec.CorePackage{{Distribution: request.Distribution, Version: request.Version, Status: "caution", Available: true, Installed: true}}, Instances: []spec.CoreInstance{}}
	report := agentproto.Report{Version: "dev", CoreInventory: inventory}
	if code, b, _ := r.agent.do("POST", "/api/agent/report", report, nil); code != 200 {
		t.Fatalf("report: %d %s", code, b)
	}
	for _, role := range []string{"operator", "support"} {
		u, _ := admin.NewUser(role+"@example.com", "password123", role)
		if err := r.st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		c.do("POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil)
		if code, _, _ := c.do("POST", base+"/jobs", body, nil); code != 403 {
			t.Fatal("non-admin could execute", role, code)
		}
	}
	bad := request
	bad.Version = "../../bin/sh"
	if code, _, _ := r.c.do("POST", base+"/jobs", map[string]any{"kind": spec.CoreManagementKind, "params": bad}, nil); code != 400 {
		t.Fatal("path accepted", code)
	}
	bad = request
	bad.Revision++
	if code, _, _ := r.c.do("POST", base+"/jobs", map[string]any{"kind": spec.CoreManagementKind, "params": bad}, nil); code != 409 {
		t.Fatal("stale revision accepted", code)
	}
	code, b, _ := r.c.do("POST", base+"/jobs", body, nil)
	if code != 200 {
		t.Fatalf("queue %d %s", code, b)
	}
	id := mustJSON[map[string]string](t, b)["id"]
	if code, _, _ := r.c.do("POST", base+"/jobs", body, nil); code != 409 {
		t.Fatal("concurrent job accepted", code)
	}
	_, b, _ = r.agent.do("GET", "/api/agent/state", nil, nil)
	state := mustJSON[agentproto.State](t, b)
	if len(state.Jobs) != 1 {
		t.Fatalf("missing delivery: %s", b)
	}
	var p spec.CoreJobParams
	if err := json.Unmarshal(state.Jobs[0].Params, &p); err != nil || p.ExpiresAt <= time.Now().Unix() || p.ExpiresAt > time.Now().Unix()+spec.CoreManagementTTL {
		t.Fatal("missing bounded deadline", p, err)
	}
	if code, _, _ := r.c.do("GET", "/api/admin/nodes/999999/jobs/"+id, nil, nil); code != 404 {
		t.Fatal("cross-node result exposed", code)
	}
	report.Jobs = []agentproto.JobResult{{ID: id, Kind: spec.CoreManagementKind, Result: json.RawMessage(`{"revision":2}`)}}
	if _, err := r.st.DB().Exec(`CREATE TRIGGER reject_core_report BEFORE UPDATE OF core_inventory_json ON nodes BEGIN SELECT RAISE(FAIL,'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := r.agent.do("POST", "/api/agent/report", report, nil); code != 500 {
		t.Fatal("failed inventory persistence acknowledged", code)
	}
	if _, err := r.st.DB().Exec(`DROP TRIGGER reject_core_report`); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := r.agent.do("POST", "/api/agent/report", report, nil); code != 200 {
		t.Fatal(code)
	}
	_, b, _ = r.c.do("GET", base+"/jobs/"+id, nil, nil)
	if j := mustJSON[store.NodeJob](t, b); j.DoneAt == nil || j.Error != "" {
		t.Fatal(j)
	}
	if err := r.st.QueueCoreOperation(ctx, "expired-core", r.nodeID, request); err != nil {
		t.Fatal(err)
	}
	if _, err := r.st.DB().Exec(`UPDATE node_jobs SET created_at = ? WHERE id = ?`, time.Now().Unix()-spec.CoreManagementTTL-1, "expired-core"); err != nil {
		t.Fatal(err)
	}
	jobs, err := r.st.PendingNodeJobs(ctx, r.nodeID)
	if err != nil || len(jobs) != 0 {
		t.Fatal("expired activation still delivered", jobs, err)
	}
}

func TestSSHInboundRequiresCapabilityAndKeepsHostIdentity(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/inbounds"
	ib := map[string]any{"Tag": "ssh", "Protocol": "ssh", "Core": "singbox-extended", "Port": 2222}
	if code, _, _ := r.c.do("POST", path, ib, nil); code != 400 {
		t.Fatal("old node accepted SSH", code)
	}
	caps := spec.CapabilitiesForCore("singbox-extended")
	r.agent.do("POST", "/api/agent/report", agentproto.Report{Cores: map[string]agentproto.CoreStatus{"singbox-extended": {Capabilities: &caps}}}, nil)
	code, b, _ := r.c.do("POST", path, ib, nil)
	if code != 200 {
		t.Fatalf("create %d %s", code, b)
	}
	created := mustJSON[struct {
		ID       int64
		Settings spec.Inbound
	}](t, b)
	if created.Settings.SSH == nil || created.Settings.SSH.PublicKey == "" {
		t.Fatal("missing generated host key")
	}
	code, b, _ = r.c.do("PATCH", "/api/admin/inbounds/"+itoa(created.ID), map[string]any{"Port": 2223}, nil)
	if code != 200 {
		t.Fatalf("edit %d %s", code, b)
	}
	stored, err := r.st.InboundByID(context.Background(), created.ID)
	if err != nil || stored.Settings.SSH.PrivateKey != created.Settings.SSH.PrivateKey {
		t.Fatal("ordinary edit rotated host identity", err)
	}
}

func TestCorePreparationFailureRoundTripsWithoutGoingOffline(t *testing.T) {
	r := newRig(t)
	const reason = "Captain does not support core singbox 1.14.1-r4 (linux/amd64); update Captain first (HTTP 404)"
	inventory := &spec.CoreInventory{Revision: 1, Instances: []spec.CoreInstance{{Distribution: "singbox", Version: "1.14.1-r4", PreparationError: reason}}}
	report := agentproto.Report{Version: "dev", CoreInventory: inventory, Cores: map[string]agentproto.CoreStatus{"singbox": {Running: false, PreparationError: reason}}}
	if code, b, _ := r.agent.do("POST", "/api/agent/report", report, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	_, b, _ := r.c.do("GET", "/api/admin/nodes/"+itoa(r.nodeID), nil, nil)
	d := mustJSON[struct {
		Node struct {
			Online bool `json:"online"`
		} `json:"node"`
		Status struct {
			Cores         map[string]agentproto.CoreStatus `json:"cores"`
			CoreInventory *spec.CoreInventory              `json:"core_inventory"`
		} `json:"status"`
	}](t, b)
	if !d.Node.Online || d.Status.Cores["singbox"].Running || d.Status.Cores["singbox"].PreparationError != reason {
		t.Fatalf("lost managed degraded state: %s", b)
	}
	// Persisted inventory must retain the optional field for the management UI.
	var raw string
	if err := r.st.DB().QueryRow("SELECT core_inventory_json FROM nodes WHERE id=?", r.nodeID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored spec.CoreInventory
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || len(stored.Instances) != 1 || stored.Instances[0].PreparationError != reason {
		t.Fatal("preparation evidence dropped", raw, err)
	}
}
