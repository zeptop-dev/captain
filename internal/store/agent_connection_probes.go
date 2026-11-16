package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/auth"
	"time"
)

type AgentProbeView struct {
	BatchID    string            `json:"batch_id"`
	TargetURL  string            `json:"target_url"`
	ExpiresAt  int64             `json:"expires_at"`
	Nodes      []AgentConnection `json:"nodes"`
	Ready      bool              `json:"ready"`
	Revision   string            `json:"revision"`
	TrustProxy bool              `json:"trust_proxy"`
}

func probeChoice(c AgentConnectionChoice) string {
	v, _ := json.Marshal([]string{c.Mode, c.EndpointID, c.URL})
	return string(v)
}
func (s *Store) StartAgentConnectionProbe(ctx context.Context, ids []int64, choice AgentConnectionChoice, fallback string) (string, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return "", ErrAgentHTTPSInvalid
	}
	choice.VerificationMode = "direct"
	choice.TrustProxy = false
	choice.DetectionID = ""
	target, _, err := s.ResolveAgentConnection(ctx, choice, fallback)
	if err != nil {
		return "", err
	}
	if _, ok := agentproto.ConnectionOrigin(target); !ok {
		return "", ErrAgentHTTPSInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	at := time.Now().Unix()
	batch := auth.Token(24)
	seen := map[int64]bool{}
	// Bounded diagnostic history; no migration/reference history is deleted.
	if _, err = tx.ExecContext(ctx, "DELETE FROM agent_connection_probes WHERE expires_at<?", at-86400); err != nil {
		return "", err
	}
	var active int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_connection_probes WHERE expires_at>=?", at).Scan(&active); err != nil {
		return "", err
	}
	if active+len(ids) > 5000 {
		return "", fmt.Errorf("%w: too many active detection requests", ErrAgentHTTPSInvalid)
	}
	for _, id := range ids {
		if seen[id] {
			return "", ErrAgentHTTPSInvalid
		}
		seen[id] = true
		var token, installed, current string
		var capable bool
		err = tx.QueryRowContext(ctx, `SELECT coalesce(n.token_hash,''),n.agent_url,coalesce(c.current_url,''),coalesce(c.probe_capable,0) FROM nodes n LEFT JOIN agent_node_connections c ON c.node_id=n.id AND c.token_hash=n.token_hash WHERE n.id=?`, id).Scan(&token, &installed, &current, &capable)
		if err != nil {
			return "", wrapNotFound(err)
		}
		source := current
		if source == "" {
			source = installed
		}
		if source == "" {
			source = fallback
		}
		status, reason := "queued", ""
		if token == "" {
			status = "install-only"
		} else if !capable {
			status = "failed"
			reason = "upgrade_required"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_connection_probes(id,batch_id,node_id,token_hash,source_url,target_url,choice_json,proof_key,status,error,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, auth.Token(24), batch, id, token, source, target, probeChoice(choice), auth.Token(32), status, reason, at, at+300); err != nil {
			return "", err
		}
	}
	return batch, tx.Commit()
}
func (s *Store) AgentConnectionProbeControl(ctx context.Context, node int64, hash string) (*agentproto.ConnectionProbe, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	at := time.Now().Unix()
	var p agentproto.ConnectionProbe
	e = tx.QueryRowContext(ctx, `SELECT id,target_url,proof_key,expires_at FROM agent_connection_probes WHERE node_id=? AND token_hash=? AND status IN ('queued','issued') AND expires_at>=? ORDER BY created_at,id LIMIT 1`, node, hash, at).Scan(&p.ID, &p.TargetURL, &p.ProofKey, &p.ExpiresAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if e != nil {
		return nil, e
	}
	var busy int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_connection_probes WHERE status='issued' AND expires_at>=? AND id<>?`, at, p.ID).Scan(&busy); e != nil {
		return nil, e
	}
	if busy >= 5 {
		return nil, tx.Commit()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE agent_connection_probes SET status='issued' WHERE id=?`, p.ID); e != nil {
		return nil, e
	}
	return &p, tx.Commit()
}
func (s *Store) AgentConnectionProbeProof(ctx context.Context, id, challenge string, binding string) (agentproto.ConnectionProof, error) {
	out := agentproto.ConnectionProof{ID: id, Challenge: challenge}
	if len(id) < 20 || len(id) > 64 || len(challenge) < 32 || len(challenge) > 128 {
		return out, ErrNotFound
	}
	var key, target string
	var expires int64
	e := s.db.QueryRowContext(ctx, `SELECT p.proof_key,p.target_url,p.expires_at FROM agent_connection_probes p JOIN nodes n ON n.id=p.node_id AND n.token_hash=p.token_hash WHERE p.id=? AND p.status='issued' AND p.expires_at>=unixepoch()`, id).Scan(&key, &target, &expires)
	if e != nil {
		return out, wrapNotFound(e)
	}
	if binding != "" {
		out.Signature = agentproto.ConnectionSignature(key, id, target, challenge, binding)
	}
	out.ProxySignature = agentproto.ConnectionSignature(key, id, target, challenge, agentproto.ProbeConnectionBinding(expires))
	return out, nil
}
func (s *Store) SaveAgentProbeResult(ctx context.Context, node int64, hash string, v agentproto.ConnectionProbeResult) error {
	if v.Status != "direct" && v.Status != "trusted-proxy" && v.Status != "failed" {
		return ErrAgentHTTPSInvalid
	}
	reason := ""
	if v.Status == "failed" {
		reason = "proof_verification"
		if v.Error == "proof_request" {
			reason = v.Error
		}
	}
	res, e := s.db.ExecContext(ctx, `UPDATE agent_connection_probes SET status=?,error=? WHERE id=? AND node_id=? AND token_hash=? AND status='issued' AND expires_at>=unixepoch()`, v.Status, reason, v.ID, node, hash)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrAgentHTTPSRevision
	}
	return nil
}
func detectedVerification(ctx context.Context, q settingsReader, id int64, hash, source, target string, c AgentConnectionChoice) (string, error) {
	if c.VerificationMode != "auto" {
		return normalizedVerification(c.VerificationMode), nil
	}
	var status, savedSource, savedChoice string
	e := q.QueryRowContext(ctx, `SELECT status,source_url,choice_json FROM agent_connection_probes WHERE batch_id=? AND node_id=? AND token_hash=? AND target_url=? AND expires_at>=unixepoch()`, c.DetectionID, id, hash, target).Scan(&status, &savedSource, &savedChoice)
	if e != nil || savedSource != source || savedChoice != probeChoice(c) {
		return "", ErrAgentHTTPSRevision
	}
	if hash == "" && status == "install-only" {
		return "direct", nil
	}
	if status != "direct" && status != "trusted-proxy" {
		return "", ErrAgentHTTPSInvalid
	}
	if status == "trusted-proxy" && !c.TrustProxy {
		return "", fmt.Errorf("%w: confirm trust in detected CDN/proxy", ErrAgentHTTPSInvalid)
	}
	return status, nil
}
func (s *Store) AgentConnectionProbeView(ctx context.Context, batch string, fallback string) (AgentProbeView, error) {
	out := AgentProbeView{BatchID: batch, Nodes: []AgentConnection{}, Ready: true}
	rows, e := s.db.QueryContext(ctx, `SELECT p.node_id,n.name,coalesce(n.token_hash,''),p.token_hash,p.source_url,p.target_url,p.status,p.error,p.expires_at,p.choice_json FROM agent_connection_probes p JOIN nodes n ON n.id=p.node_id WHERE p.batch_id=? ORDER BY p.node_id`, batch)
	if e != nil {
		return out, e
	}
	ids := []int64{}
	var choice AgentConnectionChoice
	for rows.Next() {
		var v AgentConnection
		var current, saved, source, cj string
		var expires int64
		if e = rows.Scan(&v.NodeID, &v.Name, &current, &saved, &source, &v.TargetURL, &v.Status, &v.Error, &expires, &cj); e != nil {
			rows.Close()
			return out, e
		}
		if expires < time.Now().Unix() || current != saved {
			v.Status = "failed"
			v.Error = "stale_detection"
		}
		if expires < out.ExpiresAt || out.ExpiresAt == 0 {
			out.ExpiresAt = expires
		}
		out.TargetURL = v.TargetURL
		v.URL = source
		v.Paired = saved != ""
		v.VerificationMode = v.Status
		if v.Status == "trusted-proxy" {
			out.TrustProxy = true
		}
		if v.Status != "direct" && v.Status != "trusted-proxy" && v.Status != "install-only" {
			out.Ready = false
		}
		var values []string
		if json.Unmarshal([]byte(cj), &values) != nil || len(values) != 3 {
			rows.Close()
			return out, ErrAgentHTTPSInvalid
		}
		choice = AgentConnectionChoice{Mode: values[0], EndpointID: values[1], URL: values[2], VerificationMode: "auto", DetectionID: batch, TrustProxy: true}
		ids = append(ids, v.NodeID)
		out.Nodes = append(out.Nodes, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(ids) == 0 {
		return out, ErrNotFound
	}
	if out.Ready {
		choice.TrustProxy = out.TrustProxy
		out.Revision, _, e = s.PreviewAgentConnections(ctx, ids, choice, fallback)
		if e != nil {
			out.Ready = false
			out.Revision = ""
		}
	}
	return out, nil
}
