package store

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/testdb"
	"strings"
	"testing"
)

func TestAgentProbeRequiresFinalConsentAndFrozenIdentity(t *testing.T) {
	for _, kind := range []string{"direct", "trusted-proxy"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s := New(testdb.Open(t))
			id, hash := connectionNode(t, s, 1)
			source := "https://old.example.com:2096"
			s.ObserveAgentConnection(ctx, id, hash, source, true, true, true)
			choice := AgentConnectionChoice{Mode: "custom", URL: "https://new.example.com:2096", VerificationMode: "auto"}
			batch, e := s.StartAgentConnectionProbe(ctx, []int64{id}, choice, "")
			if e != nil {
				t.Fatal(e)
			}
			v, e := s.AgentConnectionProbeView(ctx, batch, "")
			if e != nil || v.Ready {
				t.Fatal(v, e)
			}
			n, _ := s.NodeByID(ctx, id)
			if n.AgentURL != source {
				t.Fatal("detection changed target")
			}
			var count int
			s.db.QueryRow(`SELECT count(*) FROM agent_connection_migrations`).Scan(&count)
			if count != 0 {
				t.Fatal("detection created migration")
			}
			p, e := s.AgentConnectionProbeControl(ctx, id, hash)
			if e != nil || p == nil {
				t.Fatal(e)
			}
			challenge := strings.Repeat("b", 64)
			proof, e := s.AgentConnectionProbeProof(ctx, p.ID, challenge, "local-session")
			if e != nil || !agentproto.VerifyConnectionProof(p.ProofKey, p.ID, p.TargetURL, challenge, proof.ProxySignature, agentproto.ProbeConnectionBinding(p.ExpiresAt)) {
				t.Fatal(e, proof)
			}
			if e = s.SaveAgentProbeResult(ctx, id, "wrong-token", agentproto.ConnectionProbeResult{ID: p.ID, Status: kind}); e == nil {
				t.Fatal("wrong identity updated probe")
			}
			if e = s.SaveAgentProbeResult(ctx, id, hash, agentproto.ConnectionProbeResult{ID: p.ID, Status: kind}); e != nil {
				t.Fatal(e)
			}
			v, e = s.AgentConnectionProbeView(ctx, batch, "")
			if e != nil || !v.Ready || v.TrustProxy != (kind == "trusted-proxy") {
				t.Fatal(v, e)
			}
			choice.DetectionID = batch
			if kind == "trusted-proxy" {
				if _, e = s.StartAgentConnectionBatch(ctx, []int64{id}, choice, "", v.Revision); e == nil {
					t.Fatal("proxy migrated without consent")
				}
				choice.TrustProxy = true
			}
			changed := choice
			changed.URL = "https://elsewhere.example.com"
			if _, e = s.StartAgentConnectionBatch(ctx, []int64{id}, changed, "", v.Revision); e == nil {
				t.Fatal("changed target reused detection")
			}
			if _, e = s.StartAgentConnectionBatch(ctx, []int64{id}, choice, "", v.Revision); e != nil {
				t.Fatal(e)
			}
			m, e := s.AgentConnectionControl(ctx, id, hash, true)
			if e != nil || m == nil || m.VerificationMode != kind || m.ID == p.ID || m.ProofKey == p.ProofKey {
				t.Fatal(m, e)
			}
		})
	}
}
func TestAgentProbeExpiresAndNeverConvertsFailureToProxy(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	id, hash := connectionNode(t, s, 1)
	s.ObserveAgentConnection(ctx, id, hash, "https://old.example.com:2096", true, true, true)
	choice := AgentConnectionChoice{Mode: "custom", URL: "https://new.example.com:2096", VerificationMode: "auto"}
	batch, _ := s.StartAgentConnectionProbe(ctx, []int64{id}, choice, "")
	p, _ := s.AgentConnectionProbeControl(ctx, id, hash)
	s.SaveAgentProbeResult(ctx, id, hash, agentproto.ConnectionProbeResult{ID: p.ID, Status: "failed", Error: "proof_request"})
	v, e := s.AgentConnectionProbeView(ctx, batch, "")
	if e != nil || v.Ready || v.TrustProxy {
		t.Fatal(v, e)
	}
	batch, _ = s.StartAgentConnectionProbe(ctx, []int64{id}, choice, "")
	p, _ = s.AgentConnectionProbeControl(ctx, id, hash)
	s.SaveAgentProbeResult(ctx, id, hash, agentproto.ConnectionProbeResult{ID: p.ID, Status: "direct"})
	v, _ = s.AgentConnectionProbeView(ctx, batch, "")
	if !v.Ready {
		t.Fatal(v)
	}
	choice.DetectionID = batch
	s.db.Exec(`UPDATE agent_connection_probes SET expires_at=0 WHERE batch_id=?`, batch)
	if _, e = s.StartAgentConnectionBatch(ctx, []int64{id}, choice, "", v.Revision); e == nil {
		t.Fatal("expired detection authorized migration")
	}
}

func TestAgentProbeFiveConcurrentLimitAndUnknownCapability(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	ids := []int64{}
	hashes := []string{}
	for i := 0; i < 6; i++ {
		id, h := connectionNode(t, s, i)
		ids = append(ids, id)
		hashes = append(hashes, h)
		s.ObserveAgentConnection(ctx, id, h, "https://old.example.com:2096", true, true, true)
	}
	c := AgentConnectionChoice{Mode: "custom", URL: "https://target.example.com"}
	b, e := s.StartAgentConnectionProbe(ctx, ids, c, "")
	if e != nil {
		t.Fatal(e)
	}
	var first *agentproto.ConnectionProbe
	for i, id := range ids {
		p, e := s.AgentConnectionProbeControl(ctx, id, hashes[i])
		if e != nil {
			t.Fatal(e)
		}
		if i < 5 && p == nil {
			t.Fatal("missing slot")
		}
		if i == 5 && p != nil {
			t.Fatal("more than five probes")
		}
		if i == 0 {
			first = p
		}
	}
	s.SaveAgentProbeResult(ctx, ids[0], hashes[0], agentproto.ConnectionProbeResult{ID: first.ID, Status: "direct"})
	if p, e := s.AgentConnectionProbeControl(ctx, ids[5], hashes[5]); e != nil || p == nil {
		t.Fatal(p, e)
	}
	view, _ := s.AgentConnectionProbeView(ctx, b, "")
	if view.Ready {
		t.Fatal("unfinished batch ready")
	}
	id, h := connectionNode(t, s, 9)
	s.ObserveAgentConnection(ctx, id, h, "https://old.example.com:2096", true, true)
	b, e = s.StartAgentConnectionProbe(ctx, []int64{id}, c, "")
	if e != nil {
		t.Fatal(e)
	}
	view, e = s.AgentConnectionProbeView(ctx, b, "")
	if e != nil || view.Ready || view.Nodes[0].Error != "upgrade_required" {
		t.Fatal(view, e)
	}
}
