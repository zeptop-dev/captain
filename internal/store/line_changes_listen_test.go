package store

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"testing"
	"time"
)

func TestColdReviewIngressChangeSnapshotsPersistedBeforeListen(t *testing.T) {
	for _, initial := range []string{"", "198.51.100.20"} {
		t.Run("initial="+initial, func(t *testing.T) {
			ctx := context.Background()
			s, ns, _, l := lineFixture(t)
			g := &Ingress{NodeID: ns[0].ID, Name: "NAT", Kind: "nat", BindIP: initial, EntryHost: "203.0.113.30", PortMappings: []spec.PortMapping{{LocalFrom: 18443, LocalTo: 18444, PublicFrom: 30000}}}
			if err := s.CreateIngress(ctx, g); err != nil {
				t.Fatal(err)
			}
			l.Groups[0].Members[0].IngressID = &g.ID
			if err := s.SaveLine(ctx, l); err != nil {
				t.Fatal(err)
			}
			old, err := s.NodeForwards(ctx, ns[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(old) != 1 || old[0].Listen != initial {
				t.Fatalf("initial persisted forwards = %+v", old)
			}
			g.BindIP = "10.10.0.2"
			if err = s.UpdateIngress(ctx, g); err != nil {
				t.Fatal(err)
			}
			change := lineChangeLatest(t, s, l.ID, time.Now())
			got := lineChangeTarget(t, change, ns[0].ID)
			if len(got.Before) != 1 || len(got.After) != 1 {
				t.Fatalf("unexpected targets: %+v", got)
			}
			if got.Before[0].Listen != old[0].Listen {
				t.Errorf("history before Listen=%q, actual previously persisted/delivered=%q", got.Before[0].Listen, old[0].Listen)
			}
			if got.After[0].Listen != g.BindIP {
				t.Errorf("history after Listen=%q want%q", got.After[0].Listen, g.BindIP)
			}
			if got.Before[0].Tag != old[0].Tag || got.After[0].Tag == old[0].Tag {
				t.Fatalf("runtime tag boundary wrong: %+v", got)
			}
		})
	}
}
