package store

import (
	"context"
	"fmt"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/agenthttps"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/testdb"
	"testing"
	"time"
)

func connectionNode(t *testing.T, s *Store, i int) (int64, string) {
	t.Helper()
	ctx := context.Background()
	n := &domain.Node{Name: fmt.Sprint("Node ", i), AgentURL: "https://old.example.com:2096"}
	pair := fmt.Sprint("pair", i)
	hash := fmt.Sprint("hash", i)
	if err := s.CreateNode(ctx, n, pair, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemPairCode(ctx, pair, hash, "host", "v0.98.0", "linux"); err != nil {
		t.Fatal(err)
	}
	return n.ID, hash
}
func TestAgentConnectionQueueProofAndConfirmation(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	ids := []int64{}
	hashes := []string{}
	for i := 0; i < 6; i++ {
		id, hash := connectionNode(t, s, i)
		ids = append(ids, id)
		hashes = append(hashes, hash)
		if err := s.ObserveAgentConnection(ctx, id, hash, "https://old.example.com:2096", true); err != nil {
			t.Fatal(err)
		}
	}
	choice := AgentConnectionChoice{Mode: "custom", URL: "https://agent.example.com:2096"}
	revision, preview, err := s.PreviewAgentConnections(ctx, ids, choice, "")
	if err != nil || len(preview) != 6 {
		t.Fatal(revision, preview, err)
	}
	if _, err = s.StartAgentConnectionBatch(ctx, ids, choice, "", revision); err != nil {
		t.Fatal(err)
	}
	var first *agentproto.ConnectionMigration
	for i, id := range ids {
		m, err := s.AgentConnectionControl(ctx, id, hashes[i])
		if err != nil {
			t.Fatal(err)
		}
		if i < 5 && m == nil {
			t.Fatal("slot not issued")
		}
		if i == 5 && m != nil {
			t.Fatal("exceeded five active slots")
		}
		if i == 0 {
			first = m
		}
	}
	challenge := "012345678901234567890123456789012345"
	if _, err = s.AgentConnectionProof(ctx, first.ID, challenge, "wrong.example.com:2096"); err == nil {
		t.Fatal("proof accepted wrong authority")
	}
	proof, err := s.AgentConnectionProof(ctx, first.ID, challenge, "agent.example.com:2096")
	if err != nil || !agentproto.VerifyConnectionProof(first.ProofKey, first.ID, first.TargetURL, challenge, proof.Signature) {
		t.Fatal(proof, err)
	}
	if err = s.CompleteAgentConnection(ctx, ids[0], hashes[0], "old.example.com:2096", first.ID, "completed"); err == nil {
		t.Fatal("completed from old origin")
	}
	if err = s.CompleteAgentConnection(ctx, ids[0], hashes[0], "agent.example.com:2096", first.ID, "verified"); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteAgentConnection(ctx, ids[0], hashes[0], "agent.example.com:2096", first.ID, "completed"); err != nil {
		t.Fatal(err)
	}
	if m, err := s.AgentConnectionControl(ctx, ids[5], hashes[5]); err != nil || m == nil {
		t.Fatal("slot not released", m, err)
	}
	if _, err = s.StartAgentConnectionBatch(ctx, []int64{ids[0]}, choice, "", revision); err == nil {
		t.Fatal("accepted stale preview")
	}
}
func TestAgentConnectionReplacementAndRetirementProtection(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	id, hash := connectionNode(t, s, 1)
	// Existing unknown nodes still block entry retirement, regardless of default changes.
	e, err := s.SaveAgentEndpoint(ctx, AgentEndpoint{Name: "old", Config: agentHTTPSDisabledConfig("https://old.example.com:2096")})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteAgentEndpoint(ctx, e.ID, e.Revision); err == nil {
		t.Fatal("deleted referenced old origin")
	}
	if _, err = s.StartAgentConnectionBatch(ctx, []int64{id}, AgentConnectionChoice{Mode: "custom", URL: "https://agent.example.com:2096"}, ""); err != nil {
		t.Fatal(err)
	}
	m, err := s.AgentConnectionControl(ctx, id, hash)
	if err != nil || m == nil {
		t.Fatal(m, err)
	}
	if err = s.ResetPairCode(ctx, id, "replacement", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RedeemPairCode(ctx, "replacement", "newhash", "newhost", "v0.98.0", "linux"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AgentConnectionProof(ctx, m.ID, "012345678901234567890123456789012345", "agent.example.com:2096"); err == nil {
		t.Fatal("proof survived re-pair")
	}
	if got, err := s.AgentConnectionControl(ctx, id, "newhash"); err != nil || got != nil {
		t.Fatal("old intent delivered to replacement", got, err)
	}
}

func agentHTTPSDisabledConfig(url string) agenthttps.Config {
	return agenthttps.Config{URL: url, Listen: ":2096"}
}

func TestUnusedAgentEntryDoesNotTrapUnconfirmedInstallations(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	connectionNode(t, s, 1)
	unused, err := s.SaveAgentEndpoint(ctx, AgentEndpoint{Name: "Unused", Config: agentHTTPSDisabledConfig("https://unused.example.com:2096")})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteAgentEndpoint(ctx, unused.ID, unused.Revision); err != nil {
		t.Fatal("unrelated old agent blocked a never-used non-default origin", err)
	}
	used, err := s.SaveAgentEndpoint(ctx, AgentEndpoint{Name: "Former default", Config: agentHTTPSDisabledConfig("https://former.example.com:2096")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE agent_endpoints SET was_default=1 WHERE id=?", used.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteAgentEndpoint(ctx, used.ID, used.Revision); err == nil {
		t.Fatal("former default lost unconfirmed-node protection")
	}
}
