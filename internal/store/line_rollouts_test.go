package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zeptop-dev/captain/internal/db"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

type rolloutRig struct {
	s     *Store
	nodes []*domain.Node
	line  *ForwardLine
	ib    *domain.Inbound
	at    time.Time
	seq   map[int64]uint64
}

func newRolloutRig(t *testing.T) *rolloutRig {
	t.Helper()
	s, ns, ib, l := lineFixture(t)
	l.Groups = []LineGroup{{Strategy: "failover", Members: []LineMember{{NodeID: ns[0].ID, Port: 18443}}}, {Strategy: "failover", Members: []LineMember{{NodeID: ns[1].ID, Port: 19443}}}, {Strategy: "failover", Members: []LineMember{{NodeID: ns[2].ID, Port: 20443}}}}
	r := &rolloutRig{s: s, nodes: ns, line: l, ib: ib, at: time.Now().UTC().Truncate(time.Millisecond), seq: map[int64]uint64{}}
	for _, n := range ns {
		token := r.token(n.ID)
		if _, err := s.db.Exec(`UPDATE nodes SET token_hash=? WHERE id=?`, token, n.ID); err != nil {
			t.Fatal(err)
		}
		d, err := s.PrepareNodeConfigDelivery(context.Background(), n.ID, token, configSessionA, "base", "base", r.at)
		if err != nil {
			t.Fatal(err)
		}
		r.receipt(t, n.ID, d, "base", "base", true)
	}
	return r
}
func (r *rolloutRig) token(id int64) string { return fmt.Sprintf("rollout-identity-%d", id) }
func (r *rolloutRig) receipt(t *testing.T, id int64, d *agentproto.ConfigDelivery, cfg, state string, applied bool) {
	t.Helper()
	r.seq[id]++
	rep := agentproto.Report{AppliedRevision: state, ConfigReceipt: &agentproto.ConfigReceipt{DeliveryID: d.ID, Session: d.Session, ConfigRevision: cfg, StateRevision: state, Sequence: r.seq[id], ObservedAt: r.at, Applied: applied}}
	if _, err := r.s.RecordNodeReport(context.Background(), id, r.token(id), rep, false, r.at); err != nil {
		t.Fatal(err)
	}
}
func (r *rolloutRig) read(t *testing.T, id string) *LineRollout {
	t.Helper()
	p, err := r.s.LineRolloutByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (r *rolloutRig) advance(t *testing.T) {
	t.Helper()
	if _, err := r.s.AdvanceLineRollouts(context.Background(), r.at, 20); err != nil {
		t.Fatal(err)
	}
}
func (r *rolloutRig) wire(t *testing.T, id int64) []spec.Forward {
	t.Helper()
	base := lineChangeWire(t, r.s, id)
	fs, err := r.s.ApplyLineRolloutForwards(context.Background(), id, base)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}
func (r *rolloutRig) ack(t *testing.T, step LineRolloutTarget, applied bool) *agentproto.ConfigDelivery {
	t.Helper()
	fs := r.wire(t, step.NodeID)
	hash := lineChangeDigest(lineChangeProjection(fs))
	d, err := r.s.PrepareNodeConfigDeliveryForLines(context.Background(), step.NodeID, r.token(step.NodeID), configSessionA, hash, hash, fs, r.at)
	if err != nil {
		t.Fatal(err)
	}
	r.receipt(t, step.NodeID, d, hash, hash, applied)
	return d
}
func (r *rolloutRig) drain(t *testing.T, id string) *LineRollout {
	t.Helper()
	for i := 0; i < 30; i++ {
		p := r.read(t, id)
		if p.Status == "completed" {
			return p
		}
		if p.Status != "running" {
			t.Fatalf("unexpected pause: %+v", p)
		}
		found := false
		for _, step := range p.Targets {
			if step.Phase == p.Stage && step.Status == "released" {
				r.ack(t, step, true)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("no released step: %+v", p)
		}
		r.advance(t)
	}
	t.Fatal("rollout did not finish")
	return nil
}
func TestLineRolloutCreateDependencyOrderAndPublication(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	preview, err := r.s.PreviewLineRollout(ctx, r.line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = r.s.db.QueryRow(`SELECT count(*) FROM line_rollouts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("preview wrote plans", count, err)
	}
	draft := preview.Draft
	draft.PreviewRevision = preview.PreviewRevision
	p, err := r.s.PlanLineRollout(ctx, &draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "running" || p.Stage != "expand" || len(p.Targets) != 3 || p.Targets[0].NodeID != r.nodes[2].ID || p.Targets[0].Status != "released" {
		t.Fatal(p)
	}
	for i, n := range r.nodes[:3] {
		fs := r.wire(t, n.ID)
		if (i == 2) != (len(fs) == 1) {
			t.Fatal("unreleased listener leaked", i, fs)
		}
	}
	entry := &domain.Entry{Name: "Must wait", LineID: &p.LineID, Enabled: true}
	if err = r.s.CreateEntry(ctx, entry); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("published unfinished line", err)
	}
	done := r.drain(t, p.ID)
	if done.PublishedAt == nil || done.Status != "completed" {
		t.Fatal(done)
	}
	if err = r.s.CreateEntry(ctx, entry); err != nil {
		t.Fatal("confirmed line remained unpublished", err)
	}
	if entry.DisplayPort != 18443 {
		t.Fatal(entry)
	}
}
func TestLineRolloutEditExpandPublishCleanupAndReservations(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	entry := &domain.Entry{Name: "Published", LineID: &r.line.ID, Enabled: true}
	if err := r.s.CreateEntry(ctx, entry); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 1000
	}
	p, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Targets) != 6 {
		t.Fatal(p)
	}
	// Future ports are reserved against all ordinary write paths.
	manual := NodeForward{Forward: spec.Forward{Tag: "manual", Protocol: "tcp", Port: 21443, Target: "example.com:443"}}
	if err = r.s.SetNodeForwards(ctx, r.nodes[2].ID, []NodeForward{manual}); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("manual forward stole future port", err)
	}
	ib := &domain.Inbound{NodeID: r.nodes[2].ID, Tag: "reserved", Protocol: spec.VLESS, Port: 21443, Enabled: true}
	if err = r.s.CreateInbound(ctx, ib); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("inbound stole future port", err)
	}
	other := cloneLine(r.line)
	other.ID = 0
	other.Version = 0
	other.Name = "independent"
	for i := range other.Groups {
		other.Groups[i].Members[0].Port += 5000
		other.Groups[i].Members[0].ID = ""
	}
	if err = r.s.SaveLine(ctx, other); err != nil {
		t.Fatal("unrelated line was blocked", err)
	}
	if err = r.s.SaveLine(ctx, cloneLine(r.line)); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("immediate save bypassed plan", err)
	}
	for i := 0; i < 3; i++ {
		p = r.read(t, p.ID)
		step := p.Targets[i]
		r.ack(t, step, true)
		old, _ := r.s.EntryByID(ctx, entry.ID)
		if old.DisplayPort != 18443 {
			t.Fatal("subscription published before full expansion", old)
		}
		r.advance(t)
	}
	p = r.read(t, p.ID)
	if p.Stage != "cleanup" || p.PublishedAt == nil || p.Targets[3].NodeID != r.nodes[0].ID {
		t.Fatal(p)
	}
	switched, _ := r.s.EntryByID(ctx, entry.ID)
	if switched.DisplayPort != 19443 {
		t.Fatal("publication did not atomically switch Entry", switched)
	}
	// The old downstream remains until old upstream cleanup has been confirmed.
	owned := 0
	for _, f := range r.wire(t, r.nodes[2].ID) {
		if f.Port == 20443 || f.Port == 21443 {
			owned++
		}
	}
	if owned != 2 {
		t.Fatal("downstream old listener removed early")
	}
	r.drain(t, p.ID)
	for _, n := range r.nodes[:3] {
		fs := r.wire(t, n.ID)
		for _, f := range fs {
			if f.Tag == "" {
				t.Fatal("lost owned tag")
			}
		}
	}
}
func TestLineRolloutPauseRetryFreshAttemptAndLateReceipt(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	p, err := r.s.PlanLineRollout(ctx, r.line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first := p.Targets[0]
	old := r.ack(t, first, false)
	p = r.read(t, p.ID)
	if p.Status != "paused" || p.ErrorCode != "apply_not_confirmed" {
		t.Fatal(p)
	}
	resumed, err := r.s.RetryLineRollout(ctx, p.ID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Targets[0].Attempt != 2 {
		t.Fatal(resumed)
	}
	fresh := r.ack(t, resumed.Targets[0], true)
	if fresh.ID == old.ID {
		t.Fatal("retry reused failed attempt delivery")
	}
	// The previous attempt cannot overwrite the current success even with a larger sequence.
	fs := r.wire(t, first.NodeID)
	hash := lineChangeDigest(lineChangeProjection(fs))
	r.receipt(t, first.NodeID, old, hash, hash, false)
	p = r.read(t, p.ID)
	if p.Status != "running" || p.Targets[0].Status != "applied" {
		t.Fatal("late previous attempt paused resumed plan", p)
	}
	r.advance(t)
	r.drain(t, p.ID)
}
func TestLineRolloutTimeoutIdentityAndUnchangedAccess(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 1000
	}
	p, err := r.s.PlanLineRollout(ctx, draft, false, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// Authorization writes remain live, without invalidating the frozen structure.
	if _, err = r.s.db.Exec(`INSERT INTO user_groups(id,name) VALUES(1,'current access')`); err != nil {
		t.Fatal(err)
	}
	group := int64(1)
	r.ib.GroupID = &group
	if err = r.s.UpdateInbound(ctx, r.ib); err != nil {
		t.Fatal("authorization blocked by rollout", err)
	}
	line, _ := r.s.LineByID(ctx, r.line.ID)
	if line.Version != r.line.Version {
		t.Fatal("authorization recompile bumped frozen line version")
	}
	n, _ := r.s.NodeByID(ctx, r.nodes[1].ID)
	n.PublicAddr = "192.0.2.10"
	if err = r.s.UpdateNode(ctx, n); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("structural dependency edit was not rejected", err)
	}
	r.at = r.at.Add(31 * time.Second)
	r.advance(t)
	p = r.read(t, p.ID)
	if p.Status != "paused" || p.ErrorCode != "deadline_exceeded" {
		t.Fatal(p)
	}
	if err = r.s.ResetPairCode(ctx, r.nodes[2].ID, "repaired", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = r.s.RetryLineRollout(ctx, p.ID, p.Version); !errors.Is(err, ErrLineRolloutConflict) {
		t.Fatal("retry adopted changed identity", err)
	}
	if fs := r.wire(t, r.nodes[2].ID); len(fs) != 0 {
		t.Fatal("new identity inherited held old listeners", fs)
	}
}
func TestLineRolloutCompensationRecompilesAndPreservesCurrentTopology(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 1000
	}
	p, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r.ack(t, p.Targets[0], true)
	r.advance(t)
	p = r.read(t, p.ID)
	p, err = r.s.PauseLineRollout(ctx, p.ID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	undo, err := r.s.CompensateLineRollout(ctx, p.ID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if undo.ID == p.ID || undo.Compensates != p.ID {
		t.Fatal(undo)
	}
	r.drain(t, undo.ID)
	line, _ := r.s.LineByID(ctx, r.line.ID)
	if !reflect.DeepEqual(line.Groups, r.line.Groups) {
		t.Fatal("compensation did not recompile original intent", line)
	}
	old := r.read(t, p.ID)
	if old.Status != "compensated" {
		t.Fatal(old)
	}
}

func TestLineRolloutDeleteAndCompensatingCreate(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	p, err := r.s.PlanLineRollout(ctx, cloneLine(r.line), true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if p.Stage != "cleanup" || p.Targets[0].NodeID != r.nodes[0].ID {
		t.Fatal("delete was not upstream first", p)
	}
	if _, err = r.s.LineByID(ctx, r.line.ID); err != nil {
		t.Fatal("line deleted before cleanup confirmation", err)
	}
	done := r.drain(t, p.ID)
	if _, err = r.s.LineByID(ctx, r.line.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("confirmed deletion retained line", err)
	}
	restore, err := r.s.CompensateLineRollout(ctx, done.ID, done.Version)
	if err != nil {
		t.Fatal(err)
	}
	if restore.Kind != "create" || restore.LineID == r.line.ID {
		t.Fatal("compensation reused deleted scope", restore)
	}
	r.drain(t, restore.ID)
}
func TestLineRolloutRepairedIdentityNeedsOwnFreshObservation(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 1000
	}
	p, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	id := p.Targets[0].NodeID
	r.ack(t, p.Targets[0], true)
	p, err = r.s.PauseLineRollout(ctx, p.ID, r.read(t, p.ID).Version)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.s.ResetPairCode(ctx, id, "new-identity", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = r.s.RedeemPairCode(ctx, "new-identity", "replacement-token", "example", "v0.83.0", "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.s.CompensateLineRollout(ctx, p.ID, p.Version); !errors.Is(err, ErrLineRolloutUnsafe) {
		t.Fatal("new identity borrowed old released snapshot", err)
	}
	fs := r.wire(t, id)
	if len(fs) != 0 {
		t.Fatal("repaired process inherited old rules", fs)
	}
	d, err := r.s.PrepareNodeConfigDeliveryForLines(ctx, id, "replacement-token", configSessionB, "empty", "empty", fs, r.at)
	if err != nil {
		t.Fatal(err)
	}
	rep := agentproto.Report{AppliedRevision: "empty", ConfigReceipt: configReceipt(d, "empty", "empty", 1, r.at)}
	if _, err = r.s.RecordNodeReport(ctx, id, "replacement-token", rep, false, r.at); err != nil {
		t.Fatal(err)
	}
	restore, err := r.s.CompensateLineRollout(ctx, p.ID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range restore.Nodes {
		if n.NodeID == id {
			found = true
			if len(n.Before) != 0 || len(n.After) != 1 {
				t.Fatal("new identity baseline was not its confirmed empty state", n)
			}
		}
	}
	if !found {
		t.Fatal("restoration dropped repaired node")
	}
	if restore.Targets[0].Attempt != 1 || restore.Targets[0].DeliveryID != "" {
		t.Fatal("new plan inherited attempt evidence", restore)
	}
}
func TestLineRolloutCreationRollbackAndUnsupportedOverlap(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if _, err := r.s.db.Exec(`CREATE TRIGGER rollout_fail BEFORE INSERT ON line_rollout_steps BEGIN SELECT RAISE(ABORT,'injected rollout storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.PlanLineRollout(ctx, r.line, false, time.Minute); err == nil {
		t.Fatal("partial plan committed")
	}
	var count int
	for _, table := range []string{"forward_lines", "line_rollouts", "line_changes", "line_change_scopes"} {
		if err := r.s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed plan left orphan rows", table, count, err)
		}
	}
	if _, err := r.s.db.Exec(`DROP TRIGGER rollout_fail`); err != nil {
		t.Fatal(err)
	}
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	draft.Groups[1].Members[0].Port++
	if _, err := r.s.PreviewLineRollout(ctx, draft, false, time.Minute); !errors.Is(err, ErrLineRolloutUnsafe) {
		t.Fatal("in-place upstream listener replacement accepted", err)
	}
	draft.Protocol = "udp"
	if _, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute); !errors.Is(err, ErrLineRolloutUnsupported) {
		t.Fatal("unsupported transport accepted", err)
	}
}

func TestLineRolloutRestartDurableSnapshotsAndLateDynamicReceipts(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	p, err := r.s.PlanLineRollout(ctx, r.line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first := r.ack(t, p.Targets[0], true)
	r.advance(t)
	before := r.read(t, p.ID)
	var sequence int
	var name, path string
	if err = r.s.db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = r.s.db.Close(); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	r.s = New(conn)
	after := r.read(t, p.ID)
	before.Targets[0].FreshForMS = 0
	after.Targets[0].FreshForMS = 0
	if !reflect.DeepEqual(before, after) {
		t.Fatal("restart lost pointer or receipt", before, after)
	}
	step := after.Targets[0]
	fs := r.wire(t, step.NodeID)
	hash := lineChangeDigest(lineChangeProjection(fs))
	next, err := r.s.PrepareNodeConfigDeliveryForLines(ctx, step.NodeID, r.token(step.NodeID), configSessionA, hash, "new-live-users", fs, r.at)
	if err != nil {
		t.Fatal(err)
	}
	r.receipt(t, step.NodeID, first, hash, hash, true)
	got := r.read(t, p.ID)
	if got.Targets[0].DeliveryID != next.ID || got.Targets[0].Status != "released" || got.Targets[0].Fresh {
		t.Fatal("old complete State confirmed current dynamic revision", got.Targets[0])
	}
	r.receipt(t, step.NodeID, next, hash, "new-live-users", true)
	raw, err := json.Marshal(r.read(t, p.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{r.token(step.NodeID), "token_identity", "dependencies_json", "private_key", "password"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("snapshot DTO leaked credential material", secret)
		}
	}
	r.drain(t, p.ID)
}
func TestLineRolloutCompensationCanReleaseDeletedFutureMember(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	r.line.Groups = r.line.Groups[:2]
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 1000
	}
	draft.Groups = append(draft.Groups, LineGroup{Strategy: "failover", Members: []LineMember{{NodeID: r.nodes[2].ID, Port: 21443}}})
	p, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.s.DeleteNode(ctx, r.nodes[2].ID); err != nil {
		t.Fatal(err)
	}
	r.advance(t)
	p = r.read(t, p.ID)
	if p.Status != "paused" {
		t.Fatal(p)
	}
	undo, err := r.s.CompensateLineRollout(ctx, p.ID, p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if undo.Status != "completed" {
		r.drain(t, undo.ID)
	}
	if err = r.s.SaveLine(ctx, mustRolloutLine(t, r.s, r.line.ID)); err != nil {
		t.Fatal("deleted future member left scope permanently locked", err)
	}
}
func mustRolloutLine(t *testing.T, s *Store, id int64) *ForwardLine {
	t.Helper()
	l, err := s.LineByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLineRolloutReadsDoNotChangeSchedulerOrder(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	p, err := r.s.PlanLineRollout(ctx, r.line, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	checked := func() int64 {
		t.Helper()
		var v int64
		if e := r.s.db.QueryRow(`SELECT checked_at FROM line_rollouts WHERE id=?`, p.ID).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := checked()
	if _, err = r.s.ListLineRollouts(ctx, p.LineID, 0, 20); err != nil {
		t.Fatal(err)
	}
	r.read(t, p.ID)
	if got := checked(); got != before {
		t.Fatalf("read-only history request changed scheduler priority: before=%d after=%d", before, got)
	}
	r.at = r.at.Add(time.Second)
	r.advance(t)
	scheduled := checked()
	if scheduled == before {
		t.Fatal("scheduler did not record fairness checkpoint")
	}
	if _, err = r.s.ListLineRollouts(ctx, p.LineID, 0, 20); err != nil {
		t.Fatal(err)
	}
	r.read(t, p.ID)
	if checked() != scheduled {
		t.Fatal("GET changed scheduler checkpoint")
	}
}

func TestLineRolloutIngressChangesRecheckReservedListeners(t *testing.T) {
	for _, kind := range []string{"inbound", "manual"} {
		for _, op := range []string{"update", "delete"} {
			t.Run(kind+"/"+op, func(t *testing.T) {
				ctx := context.Background()
				r := newRolloutRig(t)
				target := r.nodes[1].ID
				path := &Ingress{NodeID: target, Name: "path listener", BindIP: "127.0.0.1", LineIP: "127.0.0.1"}
				other := &Ingress{NodeID: target, Name: "unrelated listener", BindIP: "127.0.0.2", LineIP: "127.0.0.2"}
				for _, g := range []*Ingress{path, other} {
					if err := r.s.CreateIngress(ctx, g); err != nil {
						t.Fatal(err)
					}
				}
				r.line.Groups[1].Members[0].IngressID = &path.ID
				if err := r.s.SaveLine(ctx, r.line); err != nil {
					t.Fatal(err)
				}
				if kind == "inbound" {
					ib := &domain.Inbound{NodeID: target, Tag: "unrelated", Protocol: spec.VLESS, Port: 21443, IngressID: &other.ID, Enabled: true}
					if err := r.s.CreateInbound(ctx, ib); err != nil {
						t.Fatal(err)
					}
				} else {
					f := NodeForward{Forward: spec.Forward{Tag: "unrelated", Protocol: "tcp", Port: 21443, IngressID: fmt.Sprint(other.ID), Target: "example.com:443"}}
					if err := r.s.SetNodeForwards(ctx, target, []NodeForward{f}); err != nil {
						t.Fatal(err)
					}
				}
				draft := cloneLine(r.line)
				for i := range draft.Groups {
					draft.Groups[i].Members[0].Port += 2000
				}
				if _, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute); err != nil {
					t.Fatal(err)
				}
				var err error
				if op == "update" {
					other.BindIP = "127.0.0.1"
					err = r.s.UpdateIngress(ctx, other)
				} else {
					err = r.s.DeleteIngress(ctx, other.ID)
				}
				if !errors.Is(err, ErrLineRolloutConflict) {
					t.Fatalf("ingress %s let %s consume reserved future listener: %v", op, kind, err)
				}
				saved, err := r.s.IngressByID(ctx, other.ID)
				if err != nil || saved.BindIP != "127.0.0.2" {
					t.Fatal("rejected ingress change was not atomic", saved, err)
				}
			})
		}
	}
}

func TestReviewRolloutUnchangedRelayMustStayApplied(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	draft.Groups[0].Members[0].Port += 1000
	p, err := r.s.PlanLineRollout(ctx, draft, false, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.s.PrepareNodeConfigDelivery(ctx, r.nodes[1].ID, r.token(r.nodes[1].ID), configSessionA, "base", "base", r.at)
	if err != nil {
		t.Fatal(err)
	}
	r.receipt(t, r.nodes[1].ID, d, "base", "base", false)
	r.ack(t, p.Targets[0], true)
	r.advance(t)
	p = r.read(t, p.ID)
	if p.Status != "paused" || p.PublishedAt != nil {
		t.Fatalf("published through unchanged relay after current apply failure: status=%s published=%v", p.Status, p.PublishedAt)
	}
}

func TestLineRolloutExpansionOnlyMemberMustStayFreshDuringCleanup(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	// Add C to an existing A -> B line. C has no old listener to clean up.
	r.line.Groups = r.line.Groups[:2]
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 2000
	}
	draft.Groups = append(draft.Groups, LineGroup{Strategy: "failover", Members: []LineMember{{NodeID: r.nodes[2].ID, Port: 23443}}})
	p, err := r.s.PlanLineRollout(ctx, draft, false, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for p.Stage == "expand" {
		for _, step := range p.Targets {
			if step.Phase == p.Stage && step.Status == "released" {
				r.ack(t, step, true)
				break
			}
		}
		r.advance(t)
		p = r.read(t, p.ID)
	}
	// Refresh every current dependency except the expansion-only member C.
	r.at = r.at.Add(4 * time.Minute)
	for _, n := range r.nodes {
		if n.ID == r.nodes[2].ID {
			continue
		}
		r.ack(t, LineRolloutTarget{NodeID: n.ID}, true)
	}
	r.advance(t)
	p = r.read(t, p.ID)
	if p.Status != "paused" || p.ErrorCode != "confirmation_stale" {
		t.Fatalf("cleanup progressed through stale expansion-only prerequisite: status=%s error=%s", p.Status, p.ErrorCode)
	}
}

func TestLineRolloutHistoricalExpansionDoesNotOverrideCurrentCleanup(t *testing.T) {
	ctx := context.Background()
	r := newRolloutRig(t)
	if err := r.s.SaveLine(ctx, r.line); err != nil {
		t.Fatal(err)
	}
	draft := cloneLine(r.line)
	for i := range draft.Groups {
		draft.Groups[i].Members[0].Port += 2000
	}
	p, err := r.s.PlanLineRollout(ctx, draft, false, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for p.Stage == "expand" {
		for _, step := range p.Targets {
			if step.Phase == p.Stage && step.Status == "released" {
				r.ack(t, step, true)
				break
			}
		}
		r.advance(t)
		p = r.read(t, p.ID)
	}
	var current LineRolloutTarget
	for _, step := range p.Targets {
		if step.Phase == "cleanup" && step.Status == "released" {
			current = step
			break
		}
	}
	r.ack(t, current, true)
	if _, err = r.s.db.Exec(`UPDATE line_rollout_steps SET last_receipt_at=?,observed_at=?,receipt_applied=0 WHERE plan_id=? AND node_id=? AND phase='expand'`, r.at.Add(-4*time.Minute).UnixMilli(), r.at.Add(-4*time.Minute).UnixMilli(), p.ID, current.NodeID); err != nil {
		t.Fatal(err)
	}
	r.advance(t)
	p = r.read(t, p.ID)
	if p.Status != "running" {
		t.Fatalf("obsolete expansion poisoned current cleanup evidence: %+v", p)
	}
	var next bool
	for _, step := range p.Targets {
		if step.Phase == "cleanup" && step.NodeID != current.NodeID && step.Status == "released" {
			next = true
		}
	}
	if !next {
		t.Fatal("did not release next cleanup after fresh current proof")
	}
}
