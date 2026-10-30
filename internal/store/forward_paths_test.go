package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func pathFixture(t *testing.T) (*Store, []*domain.Node, *domain.Inbound, *PathRoute) {
	t.Helper()
	ctx := context.Background()
	s, ns, ib, _ := lineFixture(t)
	b := &ForwardPath{Name: "Complete B", Steps: []PathStep{{NodeID: ns[1].ID}, {NodeID: ns[2].ID}}}
	c := &ForwardPath{Name: "Complete C", Steps: []PathStep{{NodeID: ns[2].ID}}}
	for _, p := range []*ForwardPath{b, c} {
		if e := s.SaveForwardPath(ctx, p); e != nil {
			t.Fatal(e)
		}
	}
	r := &PathRoute{Name: "A chooses complete B or C", Entry: LineMember{NodeID: ns[0].ID, Port: 22000}, TargetInboundID: ib.ID, Strategy: "roundrobin", Branches: []PathBranch{{PathID: b.ID, PathRevision: b.Version, Weight: 3, Ports: []int{22001, 22002}}, {PathID: c.ID, PathRevision: c.Version, Weight: 1, Ports: []int{22003}}}}
	return s, ns, ib, r
}
func pathOwned(t *testing.T, s *Store, node, line int64) []NodeForward {
	t.Helper()
	fs, e := s.NodeForwards(context.Background(), node)
	if e != nil {
		t.Fatal(e)
	}
	out := []NodeForward{}
	for _, f := range fs {
		if f.LineID == line {
			out = append(out, f)
		}
	}
	return out
}
func TestPathRouteSelectsCompleteUnequalPathsAndPreservesSources(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, r := pathFixture(t)
	source := &ForwardLine{Name: "Existing source", Protocol: "tcp", Backend: "relay", TargetInboundID: ib.ID, Groups: []LineGroup{{Members: []LineMember{{NodeID: ns[1].ID, Port: 19000}}}}}
	if e := s.SaveLine(ctx, source); e != nil {
		t.Fatal(e)
	}
	before := pathOwned(t, s, ns[1].ID, source.ID)
	preview, e := s.PreviewPathRoute(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	var count int
	if e = s.db.QueryRow(`SELECT count(*) FROM forward_path_routes`).Scan(&count); e != nil || count != 0 {
		t.Fatal("preview wrote route", count, e)
	}
	*r = preview.Draft
	r.PreviewRevision = preview.Revision
	if e = s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	if r.ChangeID == "" {
		t.Fatal("route did not atomically record change")
	}
	entry := pathOwned(t, s, ns[0].ID, r.ID)
	b := pathOwned(t, s, ns[1].ID, r.ID)
	shared := pathOwned(t, s, ns[2].ID, r.ID)
	if len(entry) != 1 || len(b) != 1 || len(shared) != 2 {
		t.Fatalf("wrong listener expansion: %+v %+v %+v", entry, b, shared)
	}
	if entry[0].Target != "198.51.100.20:22001" || len(entry[0].Targets) != 1 || entry[0].Targets[0].Target != "203.0.113.30:22003" {
		t.Fatal("root not selecting complete paths", entry)
	}
	if b[0].Target != "203.0.113.30:22002" || len(b[0].Targets) != 0 {
		t.Fatal("branch crossed into another complete path", b)
	}
	for _, f := range shared {
		if f.Target != "10.10.0.2:443" || len(f.Targets) != 0 {
			t.Fatal(f)
		}
	}
	if !reflect.DeepEqual(before, pathOwned(t, s, ns[1].ID, source.ID)) {
		t.Fatal("reuse modified existing source rules")
	}
	line, e := s.LineByID(ctx, r.ID)
	if e != nil || !line.PathRoute {
		t.Fatal(line, e)
	}
	if len(PathRouteAdjacency(line)[r.Entry.ID]) != 2 {
		t.Fatal("route adjacency missing alternatives")
	}
	if _, e = s.SaveLineWithChange(ctx, line); !errors.Is(e, ErrPathRouteOwned) {
		t.Fatal("ordinary save changed composed route", e)
	}
	if _, e = s.PreviewLine(ctx, line); !errors.Is(e, ErrPathRouteOwned) {
		t.Fatal("ordinary preview misinterpreted route", e)
	}
	if e = s.DeleteLine(ctx, line.ID, line.Version); !errors.Is(e, ErrPathRouteOwned) {
		t.Fatal("ordinary deletion bypassed route owner", e)
	}
	manual := NodeForward{Forward: spec.Forward{Tag: "manual", Port: 22500, Target: "example.com:443"}}
	if e = s.SetNodeForwards(ctx, ns[2].ID, []NodeForward{manual}); e != nil {
		t.Fatal(e)
	}
	if len(pathOwned(t, s, ns[2].ID, r.ID)) != 2 {
		t.Fatal("manual edit erased owned branch")
	}
	entryNode := &domain.Entry{Name: "Composed subscription", LineID: &r.ID, LineMemberID: r.Entry.ID, Enabled: true}
	if e = s.CreateEntry(ctx, entryNode); e != nil {
		t.Fatal(e)
	}
	if entryNode.InboundID != ib.ID || entryNode.DisplayPort != 22000 {
		t.Fatal("wrong subscription endpoint", entryNode)
	}
	if _, e = s.DeletePathRoute(ctx, r.ID, r.Version); !errors.Is(e, ErrLineInUse) {
		t.Fatal("published route was deletable", e)
	}
	if e = s.DeleteEntry(ctx, entryNode.ID); e != nil {
		t.Fatal(e)
	}
	change, e := s.DeletePathRoute(ctx, r.ID, r.Version)
	if e != nil || change == "" {
		t.Fatal(change, e)
	}
	for _, n := range ns[:3] {
		if len(pathOwned(t, s, n.ID, r.ID)) != 0 {
			t.Fatal("route cleanup left owned listener")
		}
	}
	if !reflect.DeepEqual(before, pathOwned(t, s, ns[1].ID, source.ID)) {
		t.Fatal("route cleanup damaged source")
	}
}
func TestPathPinnedReferencesCyclesUsesAndDeletion(t *testing.T) {
	ctx := context.Background()
	s, ns, _, r := pathFixture(t)
	b, e := s.ForwardPath(ctx, r.Branches[0].PathID, 1)
	if e != nil {
		t.Fatal(e)
	}
	nested := &ForwardPath{Name: "Nested fixed B", Steps: []PathStep{{PathID: b.ID, PathRevision: 1}}}
	if e = s.SaveForwardPath(ctx, nested); e != nil {
		t.Fatal(e)
	}
	r.Branches = r.Branches[:1]
	r.Branches[0].PathID = nested.ID
	if e = s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	original := pathOwned(t, s, ns[1].ID, r.ID)
	b.Steps = []PathStep{{NodeID: ns[2].ID}}
	if e = s.SaveForwardPath(ctx, b); e != nil {
		t.Fatal(e)
	}
	old, e := s.ForwardPath(ctx, b.ID, 1)
	if e != nil || len(old.Hops) != 2 {
		t.Fatal("old immutable revision changed", old, e)
	}
	if !reflect.DeepEqual(original, pathOwned(t, s, ns[1].ID, r.ID)) {
		t.Fatal("template edit followed into pinned route")
	}
	uses, e := s.ForwardPathUses(ctx, b.ID)
	if e != nil || len(uses) != 2 {
		t.Fatal("transitive uses incomplete", uses, e)
	}
	if e = s.DeleteForwardPath(ctx, b.ID, b.Version); !errors.Is(e, ErrPathInUse) {
		t.Fatal("referenced revision deleted", e)
	}
	b.Steps = []PathStep{{PathID: nested.ID, PathRevision: nested.Version}}
	if e = s.SaveForwardPath(ctx, b); e == nil {
		t.Fatal("transitive path cycle accepted")
	}
	if e = s.DeleteNode(ctx, ns[1].ID); !errors.Is(e, ErrLineInUse) {
		t.Fatal("path/route node removal allowed", e)
	}
	stale := *nested
	stale.Version = 0
	if e = s.SaveForwardPath(ctx, &stale); !errors.Is(e, ErrPathConflict) {
		t.Fatal("stale template edit accepted", e)
	}
}
func TestPathImportLinearLineAndKeepItsLandingUntouched(t *testing.T) {
	ctx := context.Background()
	s, ns, ib, l := lineFixture(t)
	if e := s.SaveLine(ctx, l); e != nil {
		t.Fatal(e)
	}
	p := &ForwardPath{Name: "Imported", Steps: []PathStep{{LineID: l.ID, LineVersion: l.Version, IncludeLanding: true}}}
	if e := s.SaveForwardPath(ctx, p); e == nil {
		t.Fatal("Cartesian source accepted as complete path")
	}
	l.Groups = l.Groups[:1]
	if e := s.SaveLine(ctx, l); e != nil {
		t.Fatal(e)
	}
	p.Steps[0].LineVersion = l.Version
	if e := s.SaveForwardPath(ctx, p); e != nil {
		t.Fatal(e)
	}
	if len(p.Hops) != 2 || p.Hops[1].NodeID != ib.NodeID {
		t.Fatal("source landing not captured as new relay", p)
	}
	before := pathOwned(t, s, ns[0].ID, l.ID)
	l.Name = "Source renamed"
	if e := s.SaveLine(ctx, l); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, pathOwned(t, s, ns[0].ID, l.ID)) {
		t.Fatal("source metadata changed runtime")
	}
	if e := s.DeleteLine(ctx, l.ID, l.Version); !errors.Is(e, ErrPathInUse) {
		t.Fatal("referenced source line deleted", e)
	}
	p.Name = "Imported renamed"
	if e := s.SaveForwardPath(ctx, p); e != nil {
		t.Fatal("editing metadata required changing an immutable source pin", e)
	}
	copied := &ForwardPath{Name: "Reuse old captured version", Steps: append([]PathStep(nil), p.Steps...)}
	if e := s.SaveForwardPath(ctx, copied); e != nil {
		t.Fatal("captured old source version was unusable", e)
	}
	fixed, e := s.ForwardPath(ctx, p.ID, 1)
	if e != nil || fixed.Steps[0].LineVersion == l.Version {
		t.Fatal("fixed source version followed current", fixed, e)
	}
}
func TestPathRouteCollisionRollbackAndPreviewCAS(t *testing.T) {
	ctx := context.Background()
	s, ns, _, r := pathFixture(t)
	r.Branches[1].Ports[0] = 22002
	if e := s.SavePathRoute(ctx, r); e == nil {
		t.Fatal("overlapping dedicated listeners accepted")
	}
	var lines, changes int
	s.db.QueryRow(`SELECT count(*) FROM forward_lines`).Scan(&lines)
	s.db.QueryRow(`SELECT count(*) FROM line_changes`).Scan(&changes)
	if lines != 0 || changes != 0 || r.ID != 0 {
		t.Fatal("failed route left transaction writes", lines, changes, r.ID)
	}
	r.Branches[1].Ports[0] = 22003
	preview, e := s.PreviewPathRoute(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	*r = preview.Draft
	r.PreviewRevision = preview.Revision
	n, e := s.NodeByID(ctx, ns[1].ID)
	if e != nil {
		t.Fatal(e)
	}
	n.PublicAddr = "192.0.2.10"
	if e = s.UpdateNode(ctx, n); e != nil {
		t.Fatal(e)
	}
	if e = s.SavePathRoute(ctx, r); !errors.Is(e, ErrLinePreview) {
		t.Fatal("stale impact accepted", e)
	}
	r.PreviewRevision = ""
	if e = s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	before := pathOwned(t, s, ns[2].ID, r.ID)
	if _, e = s.db.Exec(`CREATE TRIGGER fail_route_history BEFORE INSERT ON line_changes BEGIN SELECT RAISE(ABORT,'injected'); END`); e != nil {
		t.Fatal(e)
	}
	r.Branches[1].Ports[0]++
	if e = s.SavePathRoute(ctx, r); e == nil {
		t.Fatal("history failure not atomic")
	}
	if !reflect.DeepEqual(before, pathOwned(t, s, ns[2].ID, r.ID)) {
		t.Fatal("failed route replaced old forwards")
	}
}
func TestPathRouteAddressRecompileAndStableIdentity(t *testing.T) {
	ctx := context.Background()
	s, ns, _, r := pathFixture(t)
	if e := s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	before := pathOwned(t, s, ns[0].ID, r.ID)[0]
	renamed := *r
	renamed.Name = "Metadata"
	if e := s.SavePathRoute(ctx, &renamed); e != nil {
		t.Fatal(e)
	}
	if pathOwned(t, s, ns[0].ID, r.ID)[0].Tag != before.Tag {
		t.Fatal("rename changed listener identity")
	}
	n, e := s.NodeByID(ctx, ns[2].ID)
	if e != nil {
		t.Fatal(e)
	}
	n.PublicAddr = "198.51.100.20"
	if e = s.UpdateNode(ctx, n); e != nil {
		t.Fatal(e)
	}
	after := pathOwned(t, s, ns[0].ID, r.ID)[0]
	if after.Targets[0].Target != "198.51.100.20:22003" || after.LineMemberID != before.LineMemberID {
		t.Fatal("address recompile lost branch identity", after)
	}
	live, e := s.PathRoute(ctx, r.ID)
	if e != nil || live.Version != renamed.Version+1 {
		t.Fatal(live, e)
	}
	deps, e := s.LineDependencies(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(deps.Edges) != 5 {
		t.Fatalf("false Cartesian edges: %+v", deps.Edges)
	}
	for _, edge := range deps.Edges {
		if edge.From == ns[1].ID && edge.To != ns[2].ID {
			t.Fatal("branch crossed", edge)
		}
	}
}

func TestPathRouteLandingAndAddressModeEditsCompileNewIntent(t *testing.T) {
	ctx := context.Background()
	s, ns, _, r := pathFixture(t)
	if e := s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	entry := &domain.Entry{Name: "Published", LineID: &r.ID, LineMemberID: r.Entry.ID, Enabled: true}
	if e := s.CreateEntry(ctx, entry); e != nil {
		t.Fatal(e)
	}
	n := &domain.Node{Name: "New landing", PublicAddr: "192.0.2.77", InternalAddr: "10.10.0.77"}
	if e := s.CreateNode(ctx, n, "new-landing", time.Hour); e != nil {
		t.Fatal(e)
	}
	ib := &domain.Inbound{NodeID: n.ID, Tag: "new", Protocol: spec.VLESS, Port: 8443, Enabled: true}
	if e := s.CreateInbound(ctx, ib); e != nil {
		t.Fatal(e)
	}
	r.TargetInboundID = ib.ID
	if e := s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	for _, f := range pathOwned(t, s, ns[2].ID, r.ID) {
		if f.Target != "192.0.2.77:8443" {
			t.Errorf("new landing compiled old destination: %+v", f)
		}
	}
	published, e := s.EntryByID(ctx, entry.ID)
	if e != nil || published.InboundID != ib.ID {
		t.Fatal("subscription points at old landing", published, e)
	}
	r.TargetAddressMode = AddressModeInternal
	if e = s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	for _, f := range pathOwned(t, s, ns[2].ID, r.ID) {
		if f.Target != "10.10.0.77:8443" {
			t.Errorf("new address mode compiled old destination: %+v", f)
		}
	}
}
func TestPathLimitsAndReset(t *testing.T) {
	ctx := context.Background()
	s, ns, _, r := pathFixture(t)
	prev, e := s.ForwardPath(ctx, r.Branches[1].PathID, 1)
	if e != nil {
		t.Fatal(e)
	}
	for depth := 2; depth <= 8; depth++ {
		p := &ForwardPath{Name: fmt.Sprintf("depth %d", depth), Steps: []PathStep{{PathID: prev.ID, PathRevision: 1}}}
		if e = s.SaveForwardPath(ctx, p); e != nil {
			t.Fatal(e)
		}
		prev = p
	}
	excessive := &ForwardPath{Name: "too deep", Steps: []PathStep{{PathID: prev.ID, PathRevision: 1}}}
	if e = s.SaveForwardPath(ctx, excessive); e == nil {
		t.Fatal("depth limit ignored")
	}
	duplicate := &ForwardPath{Name: "loop", Steps: []PathStep{{NodeID: ns[1].ID}, {NodeID: ns[1].ID}}}
	if e = s.SaveForwardPath(ctx, duplicate); e == nil {
		t.Fatal("physical loop accepted")
	}
	admin := &domain.User{Email: "admin@example.com", PasswordHash: "unused", Role: domain.RoleAdmin, Status: "active"}
	if e = s.CreateStaff(ctx, admin); e != nil {
		t.Fatal(e)
	}
	if e = s.SavePathRoute(ctx, r); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResetSite(ctx, admin.ID); e != nil {
		t.Fatal(e)
	}
	paths, e := s.ListForwardPaths(ctx)
	if e != nil || len(paths) != 0 {
		t.Fatal(paths, e)
	}
	routes, e := s.ListPathRoutes(ctx)
	if e != nil || len(routes) != 0 {
		t.Fatal(routes, e)
	}
}
