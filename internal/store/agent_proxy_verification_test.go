package store

import (
	"context"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/testdb"
	"strings"
	"testing"
)

func TestTrustedProxyMigrationAuthorizationCapabilityAndRetry(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.Open(t))
	id, hash := connectionNode(t, s, 1)
	choice := AgentConnectionChoice{Mode: "custom", URL: "https://proxy.example.com:2096", VerificationMode: "trusted-proxy"}
	if _, _, e := s.PreviewAgentConnections(ctx, []int64{id}, choice, ""); e == nil {
		t.Fatal("trust acknowledgement not required")
	}
	choice.TrustProxy = true
	s.ObserveAgentConnection(ctx, id, hash, "https://old.example.com:2096", true)
	if _, e := s.StartAgentConnectionBatch(ctx, []int64{id}, choice, ""); e == nil {
		t.Fatal("old capability accepted")
	}
	s.ObserveAgentConnection(ctx, id, hash, "https://old.example.com:2096", true, true)
	rev, _, e := s.PreviewAgentConnections(ctx, []int64{id}, choice, "")
	if e != nil {
		t.Fatal(e)
	}
	changed := choice
	changed.VerificationMode = "direct"
	if _, e = s.StartAgentConnectionBatch(ctx, []int64{id}, changed, "", rev); e == nil {
		t.Fatal("mode not bound to preview")
	}
	if _, e = s.StartAgentConnectionBatch(ctx, []int64{id}, choice, "", rev); e != nil {
		t.Fatal(e)
	}
	if m, e := s.AgentConnectionControl(ctx, id, hash); e != nil || m != nil {
		t.Fatal("old process received proxy command", m, e)
	}
	m, e := s.AgentConnectionControl(ctx, id, hash, true)
	if e != nil || m == nil || m.VerificationMode != "trusted-proxy" {
		t.Fatal(m, e)
	}
	challenge := strings.Repeat("a", 64)
	p, e := s.AgentConnectionProof(ctx, m.ID, challenge, "internal-upstream:8080")
	if e != nil || !agentproto.VerifyConnectionProof(m.ProofKey, m.ID, m.TargetURL, challenge, p.Signature, agentproto.ProxyConnectionBinding(m.ExpiresAt)) {
		t.Fatal(p, e)
	}
	if agentproto.VerifyConnectionProof(m.ProofKey, m.ID, m.TargetURL, challenge, p.Signature, "some-tls-session") {
		t.Fatal("proxy proof accepted as direct")
	}
	if e = s.CompleteAgentConnection(ctx, id, hash, "internal-upstream:8080", m.ID, "verified", m.TargetURL); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteAgentConnection(ctx, id, hash, "internal-upstream:8080", m.ID, "failed", m.SourceURL, "proof_verification"); e != nil {
		t.Fatal(e)
	}
	if e = s.AgentConnectionAction(ctx, m.ID, "retry"); e != nil {
		t.Fatal(e)
	}
	next, e := s.AgentConnectionControl(ctx, id, hash, true)
	if e != nil || next == nil || next.ID == m.ID || next.ProofKey == m.ProofKey || next.VerificationMode != "trusted-proxy" {
		t.Fatal(next, e)
	}
	if _, e = s.AgentConnectionProof(ctx, m.ID, challenge, "internal-upstream:8080"); e == nil {
		t.Fatal("failed proof replay")
	}
	if e = s.CompleteAgentConnection(ctx, id, hash, "upstream", next.ID, "completed", "https://wrong.example.com"); e == nil {
		t.Fatal("reported wrong target accepted")
	}
	if e = s.CompleteAgentConnection(ctx, id, hash, "upstream", next.ID, "completed", next.TargetURL); e != nil {
		t.Fatal(e)
	}
}
