package http

import (
	"bytes"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

type configurationView struct {
	Status         string                           `json:"status"`
	ConfigRevision string                           `json:"config_revision"`
	StateRevision  string                           `json:"state_revision"`
	Current        *store.NodeConfigDeliveryRecord  `json:"current"`
	History        []store.NodeConfigDeliveryRecord `json:"history"`
}

func TestNodeConfigurationDeliveryAndDynamicState(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/configuration"
	view := func() configurationView {
		return mustJSON[configurationView](t, accessAuditCall(t, r.c, "GET", path, nil, 200))
	}
	if got := view(); got.Status != "unknown" || len(got.History) != 0 {
		t.Fatal("reads must not create deliveries", got)
	}
	session := "0123456789abcdef0123456789abcdef"
	headers := map[string]string{agentproto.ConfigSessionHeader: session}
	fetch := func() agentproto.State {
		t.Helper()
		code, b, _ := r.agent.do("GET", "/api/agent/state", nil, headers)
		if code != 200 {
			t.Fatalf("state status %d: %s", code, b)
		}
		return mustJSON[agentproto.State](t, b)
	}
	first := fetch()
	if first.ConfigRevision == "" || first.ConfigDelivery == nil {
		t.Fatal("no configuration delivery")
	}
	if got := view(); got.Status != "pending" || len(got.History) != 1 {
		t.Fatal(got)
	}
	headers["If-None-Match"] = `"` + first.Revision + `"`
	headers[agentproto.ConfigDeliveryHeader] = first.ConfigDelivery.ID
	if code, _, _ := r.agent.do("GET", "/api/agent/state", nil, headers); code != 304 {
		t.Fatalf("same content/delivery: %d", code)
	}
	// An ETag alone cannot suppress the initial session-specific delivery.
	delete(headers, agentproto.ConfigDeliveryHeader)
	again := fetch()
	if again.ConfigDelivery.ID != first.ConfigDelivery.ID {
		t.Fatal("retry created new delivery")
	}
	receipt := agentproto.ConfigReceipt{Session: session, DeliveryID: first.ConfigDelivery.ID, ConfigRevision: first.ConfigRevision, StateRevision: first.Revision, Sequence: 1, ObservedAt: time.Now(), Applied: true}
	report := func(state string, receipt agentproto.ConfigReceipt) {
		t.Helper()
		accessAuditCall(t, r.agent, "POST", "/api/agent/report", agentproto.Report{Revision: state, AppliedRevision: state, TrafficEpoch: "abcdef0123456789abcdef0123456789", TrafficSeq: 1, ConfigReceipt: &receipt}, 200)
	}
	report(first.Revision, receipt)
	confirmed := view()
	if confirmed.Status != "applied" || confirmed.Current.ConfirmedAt == nil {
		t.Fatal(confirmed)
	}
	// Same traffic batch, but an independently newer negative observation, must
	// revoke the current green state without erasing historical confirmation.
	receipt.Sequence++
	receipt.ObservedAt = time.Now()
	receipt.Applied = false
	report(first.Revision, receipt)
	if got := view(); got.Status != "pending" || got.Current.ConfirmedAt == nil {
		t.Fatal(got)
	}
	report(first.Revision, agentproto.ConfigReceipt{Session: session, DeliveryID: first.ConfigDelivery.ID, ConfigRevision: first.ConfigRevision, StateRevision: first.Revision, Sequence: 1, ObservedAt: time.Now(), Applied: true})
	if got := view(); got.Status != "pending" {
		t.Fatal("duplicate restored confirmation", got)
	}
	// User access is dynamic. Its change must invalidate current confirmation
	// even though the structural identity stays stable, before the next GET.
	r.user("configuration@example.com")
	if got := view(); got.Status != "pending" || got.ConfigRevision != first.ConfigRevision || got.StateRevision == first.Revision {
		t.Fatal("dynamic projection mixed with structure", got)
	}
	second := fetch()
	if second.ConfigRevision != first.ConfigRevision || second.ConfigDelivery.ID == first.ConfigDelivery.ID {
		t.Fatal("dynamic delivery identity", second.ConfigRevision)
	}
	receipt.Sequence++
	receipt.Applied = true
	receipt.ObservedAt = time.Now()
	report(first.Revision, receipt)
	if got := view(); got.Status != "pending" {
		t.Fatal("old delivery confirmed new state", got)
	}
	receipt.DeliveryID = second.ConfigDelivery.ID
	receipt.StateRevision = second.Revision
	receipt.Sequence++
	receipt.ObservedAt = time.Now()
	report(second.Revision, receipt)
	if got := view(); got.Status != "applied" {
		t.Fatal(got)
	}
	// A new process gets a new delivery despite identical content; retired
	// processes receive content with no delivery and cannot seize the session.
	oldSession := session
	session = "abcdefabcdefabcdefabcdefabcdefab"
	headers[agentproto.ConfigSessionHeader] = session
	headers["If-None-Match"] = `"` + second.Revision + `"`
	headers[agentproto.ConfigDeliveryHeader] = second.ConfigDelivery.ID
	third := fetch()
	if third.ConfigDelivery == nil || third.ConfigDelivery.ID == second.ConfigDelivery.ID || third.Revision != second.Revision {
		t.Fatal("process fence")
	}
	// A replacement must prove another round trip before it retires the old
	// process. Echoing the offered delivery keeps this immutable content 304.
	headers[agentproto.ConfigDeliveryHeader] = third.ConfigDelivery.ID
	if code, _, _ := r.agent.do("GET", "/api/agent/state", nil, headers); code != 304 {
		t.Fatalf("replacement challenge: %d", code)
	}
	headers[agentproto.ConfigSessionHeader] = oldSession
	retired := fetch()
	if retired.ConfigDelivery != nil {
		t.Fatal("retired process reclaimed delivery")
	}
	receipt.Sequence++
	receipt.ObservedAt = time.Now()
	report(second.Revision, receipt)
	if got := view(); got.Status != "pending" {
		t.Fatal("retired process confirmed current", got)
	}
	// A legacy reader receives neither a process ID nor another caller's token.
	legacy := securityState(t, r.agent)
	if legacy.ConfigDelivery != nil {
		t.Fatal("shared state cache leaked delivery")
	}
}

func TestNodeConfigurationPermissions(t *testing.T) {
	r := newRig(t)
	path := "/api/admin/nodes/" + itoa(r.nodeID) + "/configuration"
	accessAuditCall(t, &client{t: t, srv: r.srv}, "GET", path, nil, 401)
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, c := accessAuditStaff(t, r, role)
		accessAuditCall(t, c, "GET", path, nil, 403)
		accessAuditCall(t, accessAuditToken(t, r, u.ID, "read", []string{"nodes:*"}), "GET", path, nil, 403)
	}
	accessAuditCall(t, accessAuditToken(t, r, 1, "read", []string{"nodes:*"}), "GET", path, nil, 200)
	accessAuditCall(t, accessAuditToken(t, r, 1, "read", []string{"entries:*"}), "GET", path, nil, 403)
	if code, _, _ := r.agent.do("GET", "/api/agent/state", nil, map[string]string{agentproto.ConfigSessionHeader: "invalid"}); code != 400 {
		t.Fatal("invalid session", code)
	}
	b := accessAuditCall(t, r.c, "GET", path, nil, 200)
	if bytes.Contains(b, []byte(r.agent.token)) || bytes.Contains(b, []byte("token_hash")) {
		t.Fatal("credential exposed")
	}
}

// A first GET from a dead process can be delayed until after its replacement
// has registered. A challenge must require a second message before it can
// permanently retire the live process's already established session.
func TestNodeConfigurationLateUnknownSession(t *testing.T) {
	r := newRig(t)
	get := func(session string) agentproto.State {
		t.Helper()
		code, b, _ := r.agent.do("GET", "/api/agent/state", nil, map[string]string{agentproto.ConfigSessionHeader: session})
		if code != 200 {
			t.Fatal(code)
		}
		return mustJSON[agentproto.State](t, b)
	}
	live := "0123456789abcdef0123456789abcdef"
	initial := get(live)
	if initial.ConfigDelivery == nil {
		t.Fatal("live delivery missing")
	}
	_ = get("abcdefabcdefabcdefabcdefabcdefab") // delayed first packet of a dead process
	again := get(live)
	if again.ConfigDelivery == nil || again.ConfigDelivery.ID != initial.ConfigDelivery.ID {
		t.Fatal("a delayed first packet permanently fenced the established live process")
	}
}
