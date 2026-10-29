package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

func totalLineWrites(t *testing.T, s *Store) int64 {
	t.Helper()
	var n int64
	if err := s.db.QueryRow(`SELECT total_changes()`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestLinePreviewReadOnlyAndSaveParity(t *testing.T) {
	ctx := context.Background()
	s, _, _, l := lineFixture(t)
	original, _ := json.Marshal(l)
	writes := totalLineWrites(t, s)
	p, err := s.PreviewLine(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if totalLineWrites(t, s) != writes {
		t.Fatal("preview wrote database")
	}
	got, _ := json.Marshal(l)
	if string(got) != string(original) {
		t.Fatal("preview mutated input")
	}
	if p.Revision == "" || p.Draft.ID != 0 || len(p.Changes) != 3 || len(p.Dependencies) != 4 || len(p.Edges) != 4 || len(p.Endpoints) != 1 {
		t.Fatalf("bad preview: %+v", p)
	}
	if p.Draft.Groups[0].Members[0].ID == "" {
		t.Fatal("missing canonical member identity")
	}
	again, err := s.PreviewLine(ctx, &p.Draft)
	if err != nil || again.Revision != p.Revision {
		t.Fatal("canonical preview unstable", err)
	}
	canonical := p.Draft
	canonical.PreviewRevision = p.Revision
	if err = s.SaveLine(ctx, &canonical); err != nil {
		t.Fatal(err)
	}
	if canonical.ID <= 0 || canonical.PreviewRevision != "" {
		t.Fatal("bad saved identity", canonical)
	}
	for _, c := range p.Changes {
		actual, err := s.NodeForwards(ctx, c.NodeID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(expectedLineForwards(actual), c.After) {
			t.Fatalf("preview/save differ: %+v %+v", actual, c)
		}
	}
	renamed := canonical
	renamed.Name = "Renamed only"
	p, err = s.PreviewLine(ctx, &renamed)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Changes) != 0 {
		t.Fatal("name change should not alter runtime", p.Changes)
	}
}
func TestLinePreviewRejectsStaleImpactBeforeWriting(t *testing.T) {
	ctx := context.Background()
	s, ns, _, l := lineFixture(t)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	l.Groups[0].Members[0].Port++
	p, err := s.PreviewLine(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE nodes SET public_addr='example.com' WHERE id=?`, ns[1].ID); err != nil {
		t.Fatal(err)
	}
	checked := p.Draft
	checked.PreviewRevision = p.Revision
	writes := totalLineWrites(t, s)
	if err = s.SaveLine(ctx, &checked); !errors.Is(err, ErrLinePreview) {
		t.Fatal("stale impact accepted", err)
	}
	if totalLineWrites(t, s) != writes {
		t.Fatal("stale save wrote database")
	}
	fresh, err := s.PreviewLine(ctx, &checked)
	if err != nil {
		t.Fatal(err)
	}
	checked = fresh.Draft
	checked.PreviewRevision = fresh.Revision
	if err = s.SaveLine(ctx, &checked); err != nil {
		t.Fatal(err)
	}
	// Legacy callers still save without a preview token.
	checked.Name = "Legacy edit"
	if err = s.SaveLine(ctx, &checked); err != nil {
		t.Fatal(err)
	}
}
func TestLinePreviewUsesAndPrivateLandingChanges(t *testing.T) {
	ctx := context.Background()
	s, _, ib, l := lineFixture(t)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	root := &domain.Entry{Name: "Via line", LineID: &l.ID, Enabled: true}
	if err := s.CreateEntry(ctx, root); err != nil {
		t.Fatal(err)
	}
	child := &domain.Entry{Name: "Child", InboundID: ib.ID, DisplayHost: "example.com", DisplayPort: 443, Enabled: false, ClientDialerEntryID: &root.ID}
	if err := s.CreateEntry(ctx, child); err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewLine(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Uses) != 2 || p.Uses[0].Kind != "line" || p.Uses[1].Kind != "client_chain" || p.Uses[1].Enabled {
		t.Fatal("missing uses", p.Uses)
	}
	view, err := s.LineDependencies(ctx, l.ID)
	for i := range p.Uses {
		p.Uses[i].After = nil
	}
	if err != nil || !reflect.DeepEqual(view.Uses, p.Uses) {
		t.Fatal("dependency view differs", err)
	}
	secret := "private-value-never-in-preview"
	if _, err = s.db.Exec(`UPDATE inbounds SET settings_json=? WHERE id=?`, `{"tls":{"reality":{"private_key":"`+secret+`"}}}`, ib.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.PreviewLine(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fresh)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "private_key") {
		t.Fatal("preview leaks private spec")
	}
	if fresh.Revision == p.Revision {
		t.Fatal("landing change did not invalidate review")
	}
	checked := p.Draft
	checked.PreviewRevision = p.Revision
	if err = s.SaveLine(ctx, &checked); !errors.Is(err, ErrLinePreview) {
		t.Fatal("changed private landing accepted", err)
	}
}
func TestLinePreviewValidationMatchesSave(t *testing.T) {
	for _, scenario := range []string{"manual_collision", "published_member", "old_version", "removed_server", "udp_failover"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			s, ns, _, l := lineFixture(t)
			switch scenario {
			case "manual_collision":
				if err := s.SetNodeForwards(ctx, ns[0].ID, []NodeForward{{Forward: spec.Forward{Tag: "manual", Port: 18443, Protocol: "tcp", Target: "example.com:443"}}}); err != nil {
					t.Fatal(err)
				}
			case "published_member":
				if err := s.SaveLine(ctx, l); err != nil {
					t.Fatal(err)
				}
				if err := s.CreateEntry(ctx, &domain.Entry{Name: "Published", LineID: &l.ID, Enabled: true}); err != nil {
					t.Fatal(err)
				}
				l.Groups[0].Members[0].ID = "replacement"
			case "old_version":
				if err := s.SaveLine(ctx, l); err != nil {
					t.Fatal(err)
				}
				l.Version--
			case "removed_server":
				if err := s.QueueNodeRemoval(ctx, "remove", ns[0].ID, NodeRemovalParams{Mode: "standalone"}); err != nil {
					t.Fatal(err)
				}
			case "udp_failover":
				l.Protocol = "both"
				l.Groups[1].Strategy = "failover"
			}
			writes := totalLineWrites(t, s)
			_, previewErr := s.PreviewLine(ctx, l)
			if previewErr == nil {
				t.Fatal("invalid preview accepted")
			}
			if totalLineWrites(t, s) != writes {
				t.Fatal("failed preview wrote")
			}
			saveErr := s.SaveLine(ctx, l)
			if saveErr == nil || previewErr.Error() != saveErr.Error() {
				t.Fatalf("validation mismatch: preview %v save %v", previewErr, saveErr)
			}
		})
	}
}
func TestLinePreviewDraftAndReferenceChangesInvalidate(t *testing.T) {
	ctx := context.Background()
	s, _, _, l := lineFixture(t)
	if err := s.SaveLine(ctx, l); err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewLine(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	checked := p.Draft
	checked.PreviewRevision = p.Revision
	checked.Name = "Different draft"
	if err = s.SaveLine(ctx, &checked); !errors.Is(err, ErrLinePreview) {
		t.Fatal("changed draft accepted", err)
	}
	if err = s.CreateEntry(ctx, &domain.Entry{Name: "New use", LineID: &l.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	checked = p.Draft
	checked.PreviewRevision = p.Revision
	if err = s.SaveLine(ctx, &checked); !errors.Is(err, ErrLinePreview) {
		t.Fatal("new use not guarded", err)
	}
}

func TestLinePreviewReverseExitDependency(t *testing.T) {
	for _, kind := range []string{"wireguard", "vless_reverse"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s, ns, _, l := lineFixture(t)
			l.Groups = l.Groups[:1]
			user := domain.Inbound{NodeID: ns[3].ID, Tag: "reverse-user", Protocol: spec.VLESS, Port: 24443, Core: "xray", Enabled: true}
			entry := domain.Entry{Name: "reverse", DisplayHost: "example.com", DisplayPort: 24443, Enabled: true}
			if kind == "wireguard" {
				w := WireGuardWrite{Link: WireGuardLink{ID: "wg", TransitID: ns[3].ID, ExitID: ns[2].ID, Endpoint: "192.0.2.10:51820", ListenPort: 51820, MTU: 1380}, User: user, Entry: entry}
				if err := s.SaveWireGuardLink(ctx, &w); err != nil {
					t.Fatal(err)
				}
				l.TargetInboundID = w.User.ID
			} else {
				receiver := user
				receiver.Tag, receiver.Port = "receiver", 25443
				writes := []ReverseWrite{{Link: ReverseLink{ID: "reverse", TransitID: ns[3].ID, ExitID: ns[2].ID}, User: user, Receiver: receiver, Entry: entry}}
				if err := s.SaveReverseLinks(ctx, ns[2].ID, map[string]int64{}, writes); err != nil {
					t.Fatal(err)
				}
				l.TargetInboundID = writes[0].User.ID
			}
			p, err := s.PreviewLine(ctx, l)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Dependencies) != 3 || p.Dependencies[2].NodeID != ns[2].ID || p.Dependencies[2].Role != "reverse_exit" || len(p.Edges) != 2 || p.Edges[1].Kind != kind {
				t.Fatal("wrong reverse ownership", p.Dependencies, p.Edges)
			}
		})
	}
}
