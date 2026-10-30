package http

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

func topologyDependencyCall(t *testing.T, c *client, method, path string, body any, want int) []byte {
	t.Helper()
	code, raw, _ := c.do(method, path, body, nil)
	if code != want {
		t.Fatalf("%s %s: %d want %d: %s", method, path, code, want, raw)
	}
	if code == 403 {
		value := mustJSON[map[string]any](t, raw)
		if len(value) != 1 || value["error"] == nil {
			t.Fatal("denied request returned partial preview", string(raw))
		}
	}
	return raw
}

func topologyDependencyFixture(t *testing.T) (*rig, *domain.Inbound, *store.ForwardLine, *domain.Entry, *store.TopologyCatalog) {
	t.Helper()
	r := newRig(t)
	ctx := context.Background()
	ibs, err := r.st.AllInboundsByNode(ctx, r.nodeID)
	if err != nil || len(ibs) == 0 {
		t.Fatal("inbounds", err)
	}
	n := &domain.Node{Name: "Scoped relay", PublicAddr: "192.0.2.10"}
	if err = r.st.CreateNode(ctx, n, "LOCAL-TOPOLOGY-REVIEW", time.Hour); err != nil {
		t.Fatal(err)
	}
	a := &client{t: t, srv: r.srv}
	topologyDependencyCall(t, a, "POST", "/api/agent/pair", agentproto.PairRequest{Code: "LOCAL-TOPOLOGY-REVIEW", Version: "v0.85.0"}, 200)
	landing, err := r.st.NodeByID(ctx, r.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	landing.PublicAddr = "203.0.113.9"
	if err = r.st.UpdateNode(ctx, landing); err != nil {
		t.Fatal(err)
	}
	line := &store.ForwardLine{Name: "Restricted dependency", TargetInboundID: ibs[0].ID, Protocol: "tcp", Backend: "relay", Groups: []store.LineGroup{{Members: []store.LineMember{{NodeID: n.ID, Port: 18443}}}}}
	if err = r.st.SaveLine(ctx, line); err != nil {
		t.Fatal(err)
	}
	entry := &domain.Entry{Name: "Restricted subscription", InboundID: ibs[0].ID, LineID: &line.ID, LineMemberID: line.Groups[0].Members[0].ID, Rate: 1, Enabled: true}
	if err = r.st.CreateEntry(ctx, entry); err != nil {
		t.Fatal(err)
	}
	catalog, err := r.st.TopologyChangesetCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return r, ibs[0], line, entry, catalog
}

func topologyDependencyInbound(catalog *store.TopologyCatalog, ib *domain.Inbound, key string) store.TopologyOperation {
	port := ib.Port + 1000
	op := store.TopologyOperation{Key: key, Kind: "inbound", ID: ib.ID, Inbound: &store.TopologyInboundPatch{Port: &port}}
	for _, in := range catalog.Inbounds {
		if in.ID == ib.ID {
			op.Revision = in.Revision
		}
	}
	return op
}

func topologyDependencyLine(t *testing.T, catalog *store.TopologyCatalog, id int64, changePort bool) store.TopologyOperation {
	t.Helper()
	for _, item := range catalog.Lines {
		if item.Line.ID == id {
			line := item.Line
			if changePort {
				line.Groups[0].Members[0].Port++
			}
			return store.TopologyOperation{Key: "line", Kind: "line", ID: id, Revision: item.Revision, Line: &line}
		}
	}
	t.Fatal("missing line revision")
	return store.TopologyOperation{}
}

func topologyDependencySave(t *testing.T, r *rig, ops ...store.TopologyOperation) (store.TopologyChangeset, string) {
	t.Helper()
	draft := mustJSON[store.TopologyChangeset](t, topologyDependencyCall(t, r.c, "POST", "/api/admin/topology-changesets", map[string]any{"name": "Seeded before narrowing", "operations": ops}, 200))
	return draft, "/api/admin/topology-changesets/" + draft.ID
}

func TestTopologyChangesetPreviewRequiresDependencyReadPermission(t *testing.T) {
	r, ib, line, _, catalog := topologyDependencyFixture(t)
	draft, path := topologyDependencySave(t, r, topologyDependencyInbound(catalog, ib, "landing"))
	scopes := []string{"topology-changesets:write", "PATCH /api/admin/inbounds/{id}"}
	narrow := accessAuditToken(t, r, 1, "full", scopes)
	for _, p := range []string{"/api/admin/lines", "/api/admin/entries", "/api/admin/topology-changesets/catalog"} {
		topologyDependencyCall(t, narrow, "GET", p, nil, 403)
	}
	topologyDependencyCall(t, narrow, "POST", path+"/preview", map[string]any{"version": draft.Version}, 403)
	reader := accessAuditToken(t, r, 1, "full", append(scopes, "GET /api/admin/lines", "entries:read"))
	preview := mustJSON[store.TopologyPreview](t, topologyDependencyCall(t, reader, "POST", path+"/preview", map[string]any{"version": draft.Version}, 200))
	found := false
	for _, change := range preview.Changes {
		if change.Kind == "line" && change.ID == line.ID {
			found = change.Before != nil && change.After != nil && len(change.Forwards) > 0
		}
	}
	if !found {
		t.Fatal("fixture did not expose dependent line configuration")
	}
	body := map[string]any{"version": draft.Version, "preview_revision": preview.Revision}
	// A previously authorized preview never supplies authority to a narrower commit.
	topologyDependencyCall(t, narrow, "POST", path+"/commit", body, 403)
	stored, err := r.st.InboundByID(context.Background(), ib.ID)
	if err != nil || stored.Port != ib.Port {
		t.Fatal("denied commit changed live inbound", err)
	}
	topologyDependencyCall(t, reader, "POST", path+"/commit", body, 200)
	stored, err = r.st.InboundByID(context.Background(), ib.ID)
	if err != nil || stored.Port != ib.Port+1000 {
		t.Fatal("authorized commit did not apply", err)
	}
}

func TestTopologyChangesetPreviewDependencyKeysCannotGrantRead(t *testing.T) {
	r, ib, line, entry, catalog := topologyDependencyFixture(t)
	narrow := accessAuditToken(t, r, 1, "full", []string{"topology-changesets:write", "PATCH /api/admin/inbounds/{id}"})
	for _, key := range []string{"dependent-line-" + strconv.FormatInt(line.ID, 10), "dependent-entry-" + strconv.FormatInt(entry.ID, 10)} {
		draft, path := topologyDependencySave(t, r, topologyDependencyInbound(catalog, ib, key))
		topologyDependencyCall(t, narrow, "POST", path+"/preview", map[string]any{"version": draft.Version}, 403)
	}
}

func TestTopologyChangesetPreviewRequiresDependentEntryReadPermission(t *testing.T) {
	r, _, line, entry, catalog := topologyDependencyFixture(t)
	draft, path := topologyDependencySave(t, r, topologyDependencyLine(t, catalog, line.ID, true))
	scopes := []string{"topology-changesets:write", "PUT /api/admin/lines/{id}"}
	narrow := accessAuditToken(t, r, 1, "full", scopes)
	topologyDependencyCall(t, narrow, "POST", path+"/preview", map[string]any{"version": draft.Version}, 403)
	reader := accessAuditToken(t, r, 1, "full", append(scopes, "entries:read"))
	preview := mustJSON[store.TopologyPreview](t, topologyDependencyCall(t, reader, "POST", path+"/preview", map[string]any{"version": draft.Version}, 200))
	found := false
	for _, change := range preview.Changes {
		if change.Kind == "entry" && change.ID == entry.ID {
			found = change.Before != nil && change.After != nil
		}
	}
	if !found {
		t.Fatal("fixture did not expose dependent Entry")
	}
	body := map[string]any{"version": draft.Version, "preview_revision": preview.Revision}
	topologyDependencyCall(t, narrow, "POST", path+"/commit", body, 403)
	topologyDependencyCall(t, reader, "POST", path+"/commit", body, 200)
	stored, err := r.st.EntryByID(context.Background(), entry.ID)
	if err != nil || stored.DisplayPort != entry.DisplayPort+1 {
		t.Fatal("authorized endpoint update missing", err)
	}
}

func TestTopologyChangesetExplicitDependencyRequiresItsOwnWriteGrant(t *testing.T) {
	r, ib, line, _, catalog := topologyDependencyFixture(t)
	// Naming the same dependent line as an explicit operation cannot borrow the
	// inbound's authority. Its actual existing endpoint write grant is required.
	draft, path := topologyDependencySave(t, r, topologyDependencyInbound(catalog, ib, "landing"), topologyDependencyLine(t, catalog, line.ID, false))
	scopes := []string{"topology-changesets:write", "PATCH /api/admin/inbounds/{id}"}
	narrow := accessAuditToken(t, r, 1, "full", scopes)
	topologyDependencyCall(t, narrow, "POST", path+"/preview", map[string]any{"version": draft.Version}, 403)
	writer := accessAuditToken(t, r, 1, "full", append(scopes, "PUT /api/admin/lines/{id}"))
	// Preserve the existing explicit line-write preview contract. This grant does
	// not authorize implicit Entries; that case is covered independently above.
	preview := mustJSON[store.TopologyPreview](t, topologyDependencyCall(t, writer, "POST", path+"/preview", map[string]any{"version": draft.Version}, 200))
	body := map[string]any{"version": draft.Version, "preview_revision": preview.Revision}
	topologyDependencyCall(t, narrow, "POST", path+"/commit", body, 403)
	topologyDependencyCall(t, writer, "POST", path+"/commit", body, 200)
}

func TestTopologyChangesetCookieAdminKeepsDependencyPreviewAndCommit(t *testing.T) {
	r, ib, line, entry, catalog := topologyDependencyFixture(t)
	draft, path := topologyDependencySave(t, r, topologyDependencyInbound(catalog, ib, "dependent-entry-"+strconv.FormatInt(entry.ID, 10)), topologyDependencyLine(t, catalog, line.ID, true))
	preview := mustJSON[store.TopologyPreview](t, topologyDependencyCall(t, r.c, "POST", path+"/preview", map[string]any{"version": draft.Version}, 200))
	if len(preview.Changes) != 3 {
		t.Fatal("admin lost combined impact", preview)
	}
	topologyDependencyCall(t, r.c, "POST", path+"/commit", map[string]any{"version": draft.Version, "preview_revision": preview.Revision}, 200)
}
