package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/agentendpoint"
	"github.com/zeptop-dev/captain/internal/auth"
)

type AgentConnection struct {
	Name        string `json:"name,omitempty"`
	Paired      bool   `json:"paired"`
	NodeID      int64  `json:"node_id"`
	URL         string `json:"url"`
	SeenAt      int64  `json:"seen_at"`
	Capable     bool   `json:"capable"`
	Mode        string `json:"mode"`
	EndpointID  string `json:"endpoint_id"`
	TargetURL   string `json:"target_url"`
	MigrationID string `json:"migration_id,omitempty"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}
type AgentConnectionChoice struct {
	Mode       string `json:"mode"`
	EndpointID string `json:"endpoint_id"`
	URL        string `json:"url"`
}
type AgentConnectionMigration struct {
	Name      string `json:"name,omitempty"`
	ID        string `json:"id"`
	BatchID   string `json:"batch_id"`
	NodeID    int64  `json:"node_id"`
	SourceURL string `json:"source_url"`
	TargetURL string `json:"target_url"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func (s *Store) ResolveAgentConnection(ctx context.Context, c AgentConnectionChoice, fallback string) (string, string, error) {
	switch c.Mode {
	case "default":
		return s.DefaultAgentURL(ctx, fallback), "", nil
	case "managed":
		var u string
		err := s.db.QueryRowContext(ctx, "SELECT url FROM agent_endpoints WHERE id=? AND enabled=1", c.EndpointID).Scan(&u)
		return u, c.EndpointID, wrapNotFound(err)
	case "custom":
		u, err := agentendpoint.NormalizeURL(c.URL)
		if err != nil || u == "" {
			return "", "", fmt.Errorf("%w: invalid custom origin", ErrAgentHTTPSInvalid)
		}
		return u, "", nil
	default:
		return "", "", fmt.Errorf("%w: invalid connection selection", ErrAgentHTTPSInvalid)
	}
}
func (s *Store) AgentConnection(ctx context.Context, id int64) (AgentConnection, error) {
	out := AgentConnection{NodeID: id, Mode: "default", Status: "unknown"}
	n, err := s.NodeByID(ctx, id)
	if err != nil {
		return out, err
	}
	out.Name = n.Name
	out.Paired = n.Paired
	out.TargetURL = n.AgentURL
	if out.TargetURL != "" {
		out.Mode = "custom"
	}
	err = s.db.QueryRowContext(ctx, `SELECT current_url,seen_at,capable,mode,endpoint_id FROM agent_node_connections WHERE node_id=? AND token_hash=(SELECT token_hash FROM nodes WHERE id=?)`, id, id).Scan(&out.URL, &out.SeenAt, &out.Capable, &out.Mode, &out.EndpointID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var source string
	err = s.db.QueryRowContext(ctx, `SELECT id,source_url,target_url,CASE WHEN status IN ('issued','verified') AND expires_at<unixepoch() THEN 'failed' ELSE status END,CASE WHEN status IN ('issued','verified') AND expires_at<unixepoch() THEN 'migration timed out' ELSE error END FROM agent_connection_migrations WHERE node_id=? AND token_hash=(SELECT token_hash FROM nodes WHERE id=?) ORDER BY created_at DESC,rowid DESC LIMIT 1`, id, id).Scan(&out.MigrationID, &source, &out.TargetURL, &out.Status, &out.Error)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if out.MigrationID == "" && out.URL != "" {
		out.Status = "connected"
	}
	if out.Status == "cancelled" {
		out.TargetURL = n.AgentURL
	}
	if out.Status == "completed" && out.URL != "" && out.URL != out.TargetURL {
		out.Status = "different"
	}
	return out, nil
}

// ObserveAgentConnection records only an authenticated report of its own origin.
// Freshness is evidence, not a perpetual guarantee of online reachability.
func (s *Store) ObserveAgentConnection(ctx context.Context, id int64, tokenHash, url string, capable bool) error {
	if url == "" {
		return nil
	}
	u, err := agentendpoint.NormalizeURL(url)
	if err != nil || u == "" {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_node_connections(node_id,token_hash,current_url,seen_at,capable)
 SELECT id,token_hash,?,?,? FROM nodes WHERE id=? AND token_hash=?
 ON CONFLICT(node_id) DO UPDATE SET token_hash=excluded.token_hash,current_url=excluded.current_url,seen_at=excluded.seen_at,capable=excluded.capable`, u, time.Now().Unix(), capable, id, tokenHash)
	return err
}

// StartAgentConnectionBatch freezes identities and source/target origins in one
// transaction. Offline nodes remain queued; only capable nodes claim a slot.
func (s *Store) StartAgentConnectionBatch(ctx context.Context, ids []int64, choice AgentConnectionChoice, fallback string, expected ...string) (string, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return "", fmt.Errorf("%w: select 1 to 500 nodes", ErrAgentHTTPSInvalid)
	}
	target, endpoint, err := s.ResolveAgentConnection(ctx, choice, fallback)
	if err != nil {
		return "", err
	}
	if _, ok := agentproto.ConnectionOrigin(target); !ok {
		return "", fmt.Errorf("%w: target requires HTTPS", ErrAgentHTTPSInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// The selected origin must still exist/enabled at commit, including default changes.
	if choice.Mode == "managed" {
		var current string
		if err = tx.QueryRowContext(ctx, "SELECT url FROM agent_endpoints WHERE id=? AND enabled=1", endpoint).Scan(&current); err != nil || current != target {
			return "", ErrAgentHTTPSRevision
		}
	}
	if choice.Mode == "default" {
		var current string
		e := tx.QueryRowContext(ctx, "SELECT url FROM agent_endpoints WHERE is_default=1 AND enabled=1").Scan(&current)
		if e == nil && current != target {
			return "", ErrAgentHTTPSRevision
		}
	}
	if len(expected) > 0 {
		revision, _, e := connectionPreview(ctx, tx, ids, choice, target, endpoint, fallback)
		if e != nil {
			return "", e
		}
		if expected[0] == "" || revision != expected[0] {
			return "", ErrAgentHTTPSRevision
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE agent_connection_migrations SET status='failed',error='migration timed out',updated_at=? WHERE status IN ('issued','verified') AND expires_at<?", time.Now().Unix(), time.Now().Unix()); err != nil {
		return "", err
	}
	batch := auth.Token(24)
	seen := map[int64]bool{}
	at := time.Now().Unix()
	for _, id := range ids {
		if seen[id] {
			return "", fmt.Errorf("%w: duplicate node", ErrAgentHTTPSInvalid)
		}
		seen[id] = true
		var tokenHash, installed string
		if err = tx.QueryRowContext(ctx, "SELECT coalesce(token_hash,''),agent_url FROM nodes WHERE id=?", id).Scan(&tokenHash, &installed); err != nil {
			return "", wrapNotFound(err)
		}
		// Unpaired nodes only need an installation preference, never a remote command.
		if tokenHash == "" {
			if _, err = tx.ExecContext(ctx, "UPDATE nodes SET agent_url=? WHERE id=?", target, id); err != nil {
				return "", err
			}
			continue
		}
		source := installed
		var observed string
		e := tx.QueryRowContext(ctx, "SELECT current_url FROM agent_node_connections WHERE node_id=? AND token_hash=?", id, tokenHash).Scan(&observed)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return "", e
		}
		if observed != "" {
			source = observed
		}
		if source == "" {
			source = fallback
		}
		var active int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_connection_migrations WHERE node_id=? AND status IN ('queued','issued','verified')", id).Scan(&active); err != nil {
			return "", err
		}
		if active > 0 {
			return "", fmt.Errorf("%w: node already has an active migration", ErrAgentHTTPSInvalid)
		}
		status := "queued"
		if source == target && observed == target {
			status = "completed"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_connection_migrations(id,batch_id,node_id,token_hash,source_url,target_url,endpoint_id,proof_key,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, auth.Token(24), batch, id, tokenHash, source, target, endpoint, auth.Token(32), status, at, at)
		if err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE nodes SET agent_url=? WHERE id=?", target, id); err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_node_connections(node_id,token_hash,mode,endpoint_id) VALUES(?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET mode=excluded.mode,endpoint_id=excluded.endpoint_id`, id, tokenHash, choice.Mode, endpoint)
		if err != nil {
			return "", err
		}
	}
	return batch, tx.Commit()
}
func (s *Store) AgentConnectionMigrations(ctx context.Context) ([]AgentConnectionMigration, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.batch_id,m.node_id,n.name,m.source_url,m.target_url,CASE WHEN m.status IN ('issued','verified') AND m.expires_at<unixepoch() THEN 'failed' ELSE m.status END,CASE WHEN m.status IN ('issued','verified') AND m.expires_at<unixepoch() THEN 'migration timed out' ELSE m.error END FROM agent_connection_migrations m JOIN nodes n ON n.id=m.node_id ORDER BY m.created_at DESC,m.rowid DESC LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentConnectionMigration{}
	for rows.Next() {
		var v AgentConnectionMigration
		if err = rows.Scan(&v.ID, &v.BatchID, &v.NodeID, &v.Name, &v.SourceURL, &v.TargetURL, &v.Status, &v.Error); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) AgentConnectionControl(ctx context.Context, id int64, tokenHash string) (*agentproto.ConnectionMigration, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	at := time.Now().Unix()
	// Re-pairing cannot deliver old authority to a replacement installation.
	_, err = tx.ExecContext(ctx, "UPDATE agent_connection_migrations SET status='failed',error='expired or replaced node identity',updated_at=? WHERE node_id=? AND status IN ('queued','issued','verified') AND (token_hash<>? OR (expires_at>0 AND expires_at<?))", at, id, tokenHash, at)
	if err != nil {
		return nil, err
	}
	var m agentproto.ConnectionMigration
	var status string
	err = tx.QueryRowContext(ctx, `SELECT id,source_url,target_url,proof_key,expires_at,status FROM agent_connection_migrations WHERE node_id=? AND token_hash=? AND status IN ('queued','issued','verified')`, id, tokenHash).Scan(&m.ID, &m.SourceURL, &m.TargetURL, &m.ProofKey, &m.ExpiresAt, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if status == "queued" {
		var actual string
		if e := tx.QueryRowContext(ctx, "SELECT current_url FROM agent_node_connections WHERE node_id=? AND token_hash=?", id, tokenHash).Scan(&actual); e == nil && actual != "" {
			m.SourceURL = actual
			if _, err = tx.ExecContext(ctx, "UPDATE agent_connection_migrations SET source_url=? WHERE id=?", actual, m.ID); err != nil {
				return nil, err
			}
		}

		// Expire abandoned slots before enforcing the global five-at-a-time bound.
		if _, err = tx.ExecContext(ctx, "UPDATE agent_connection_migrations SET status='failed',error='migration timed out',updated_at=? WHERE status IN ('issued','verified') AND expires_at<?", at, at); err != nil {
			return nil, err
		}
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_connection_migrations WHERE status IN ('issued','verified')").Scan(&count); err != nil {
			return nil, err
		}
		if count >= 5 {
			return nil, tx.Commit()
		}
		m.ExpiresAt = at + 300
		if _, err = tx.ExecContext(ctx, "UPDATE agent_connection_migrations SET status='issued',issued_at=?,expires_at=?,updated_at=? WHERE id=?", at, m.ExpiresAt, at, m.ID); err != nil {
			return nil, err
		}
	}
	return &m, tx.Commit()
}
func (s *Store) AgentConnectionProof(ctx context.Context, id, challenge, host string, binding ...string) (agentproto.ConnectionProof, error) {
	out := agentproto.ConnectionProof{ID: id, Challenge: challenge}
	if len(id) < 20 || len(id) > 64 || len(challenge) < 32 || len(challenge) > 128 {
		return out, ErrNotFound
	}
	var key, target string
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT m.proof_key,m.target_url,m.expires_at FROM agent_connection_migrations m JOIN nodes n ON n.id=m.node_id AND n.token_hash=m.token_hash WHERE m.id=? AND m.status IN ('issued','verified')`, id).Scan(&key, &target, &expires)
	if err != nil {
		return out, wrapNotFound(err)
	}
	if expires < time.Now().Unix() || !AgentOriginHost(target, host) {
		return out, ErrNotFound
	}
	out.Signature = agentproto.ConnectionSignature(key, id, target, challenge, binding...)
	return out, nil
}
func (s *Store) CompleteAgentConnection(ctx context.Context, nodeID int64, tokenHash, host, id, status string) error {
	if status != "verified" && status != "completed" && status != "failed" {
		return ErrAgentHTTPSInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var target, source, old string
	var expires int64
	err = tx.QueryRowContext(ctx, `SELECT m.target_url,m.source_url,m.status,m.expires_at FROM agent_connection_migrations m JOIN nodes n ON n.id=m.node_id AND n.token_hash=m.token_hash WHERE m.id=? AND m.node_id=? AND m.token_hash=?`, id, nodeID, tokenHash).Scan(&target, &source, &old, &expires)
	if err != nil {
		return wrapNotFound(err)
	}
	if old == "completed" && status == "failed" {
		var newer int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_connection_migrations WHERE node_id=? AND rowid>(SELECT rowid FROM agent_connection_migrations WHERE id=?)", nodeID, id).Scan(&newer); err != nil {
			return err
		}
		if newer > 0 || !AgentOriginHost(source, host) {
			return ErrAgentHTTPSRevision
		}
		old = "verified"
	}
	if old == "completed" && status == "completed" {
		return tx.Commit()
	}
	if (old != "issued" && old != "verified") || expires < time.Now().Unix() {
		return ErrAgentHTTPSRevision
	}
	if status != "failed" && !AgentOriginHost(target, host) {
		return ErrAgentHTTPSInvalid
	}
	message := ""
	if status == "failed" {
		message = "node could not verify or persist the new connection"
	}
	if _, err = tx.ExecContext(ctx, "UPDATE agent_connection_migrations SET status=?,error=?,updated_at=? WHERE id=?", status, message, time.Now().Unix(), id); err != nil {
		return err
	}
	if status == "completed" {
		if _, err = tx.ExecContext(ctx, "UPDATE agent_node_connections SET current_url=?,seen_at=? WHERE node_id=? AND token_hash=?", target, time.Now().Unix(), nodeID, tokenHash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func connectionPreview(ctx context.Context, tx *sql.Tx, ids []int64, choice AgentConnectionChoice, target, endpoint, fallback string) (string, []AgentConnection, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return "", nil, ErrAgentHTTPSInvalid
	}
	values := []string{choice.Mode, endpoint, target, fallback}
	nodes := []AgentConnection{}
	seen := map[int64]bool{}
	if endpoint != "" {
		var revision, url string
		if err := tx.QueryRowContext(ctx, "SELECT revision,url FROM agent_endpoints WHERE id=? AND enabled=1", endpoint).Scan(&revision, &url); err != nil || url != target {
			return "", nil, ErrAgentHTTPSRevision
		}
		values = append(values, revision)
	}
	for _, id := range ids {
		if seen[id] {
			return "", nil, ErrAgentHTTPSInvalid
		}
		seen[id] = true
		v := AgentConnection{NodeID: id, TargetURL: target}
		var tokenHash, installed, version string
		err := tx.QueryRowContext(ctx, `SELECT coalesce(n.token_hash,''),n.agent_url,n.version,n.name,coalesce(c.current_url,''),coalesce(c.seen_at,0),coalesce(c.capable,0)
   FROM nodes n LEFT JOIN agent_node_connections c ON c.node_id=n.id AND c.token_hash=n.token_hash WHERE n.id=?`, id).Scan(&tokenHash, &installed, &version, &v.Name, &v.URL, &v.SeenAt, &v.Capable)
		if err != nil {
			return "", nil, wrapNotFound(err)
		}
		v.Paired = tokenHash != ""
		nodes = append(nodes, v)
		values = append(values, fmt.Sprint(id), tokenHash, installed, version, v.URL)
	}
	raw, _ := json.Marshal(values)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nodes, nil
}
func (s *Store) PreviewAgentConnections(ctx context.Context, ids []int64, choice AgentConnectionChoice, fallback string) (string, []AgentConnection, error) {
	target, endpoint, err := s.ResolveAgentConnection(ctx, choice, fallback)
	if err != nil {
		return "", nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	revision, nodes, err := connectionPreview(ctx, tx, ids, choice, target, endpoint, fallback)
	if err != nil {
		return "", nil, err
	}
	return revision, nodes, tx.Commit()
}
func (s *Store) AgentConnectionAction(ctx context.Context, id, action string) error {
	if action != "retry" && action != "cancel" {
		return ErrAgentHTTPSInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var nodeID int64
	var tokenHash, source, target, endpoint, status string
	err = tx.QueryRowContext(ctx, `SELECT m.node_id,m.token_hash,m.source_url,m.target_url,m.endpoint_id,m.status FROM agent_connection_migrations m JOIN nodes n ON n.id=m.node_id AND n.token_hash=m.token_hash WHERE m.id=?`, id).Scan(&nodeID, &tokenHash, &source, &target, &endpoint, &status)
	if err != nil {
		return wrapNotFound(err)
	}
	if action == "retry" && (status == "issued" || status == "verified") {
		var expired bool
		if err = tx.QueryRowContext(ctx, "SELECT expires_at<unixepoch() FROM agent_connection_migrations WHERE id=?", id).Scan(&expired); err != nil {
			return err
		}
		if expired {
			if _, err = tx.ExecContext(ctx, "UPDATE agent_connection_migrations SET status='failed',error='migration timed out',updated_at=? WHERE id=?", time.Now().Unix(), id); err != nil {
				return err
			}
			status = "failed"
		}
	}
	if action == "cancel" {
		if status != "queued" {
			return ErrAgentHTTPSRevision
		}
		if _, err = tx.ExecContext(ctx, "UPDATE agent_connection_migrations SET status='cancelled',updated_at=? WHERE id=?", time.Now().Unix(), id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE nodes SET agent_url=? WHERE id=? AND agent_url=?", source, nodeID, target); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE agent_node_connections SET mode='custom',endpoint_id='' WHERE node_id=?", nodeID)
		if err != nil {
			return err
		}
	} else {
		if status != "failed" {
			return ErrAgentHTTPSRevision
		}
		if endpoint != "" {
			var u string
			if err = tx.QueryRowContext(ctx, "SELECT url FROM agent_endpoints WHERE id=? AND enabled=1", endpoint).Scan(&u); err != nil || u != target {
				return ErrAgentHTTPSRevision
			}
		}
		var active int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM agent_connection_migrations WHERE node_id=? AND status IN ('queued','issued','verified')", nodeID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return ErrAgentHTTPSRevision
		}
		var actual string
		if err = tx.QueryRowContext(ctx, "SELECT current_url FROM agent_node_connections WHERE node_id=? AND token_hash=?", nodeID, tokenHash).Scan(&actual); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if actual != "" {
			source = actual
		}
		at := time.Now().Unix()
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_connection_migrations(id,batch_id,node_id,token_hash,source_url,target_url,endpoint_id,proof_key,status,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,'queued',?,?)`, auth.Token(24), auth.Token(24), nodeID, tokenHash, source, target, endpoint, auth.Token(32), at, at)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Entry usage is a bounded read-only view. Unknown legacy agents are explicitly
// included instead of treating their installation preference as runtime proof.
func (s *Store) AgentEndpointNodes(ctx context.Context, id string) ([]AgentConnection, bool, error) {
	var target string
	var wasDefault bool
	if err := s.db.QueryRowContext(ctx, "SELECT url,was_default FROM agent_endpoints WHERE id=?", id).Scan(&target, &wasDefault); err != nil {
		return nil, false, wrapNotFound(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT n.id,n.name,coalesce(c.current_url,''),coalesce(c.seen_at,0),coalesce(c.capable,0),n.agent_url,n.token_hash IS NOT NULL
 FROM nodes n LEFT JOIN agent_node_connections c ON c.node_id=n.id AND c.token_hash=n.token_hash
 LEFT JOIN agent_connection_migrations m ON m.node_id=n.id AND m.status IN ('queued','issued','verified')
 WHERE n.agent_url=? OR c.endpoint_id=? OR c.current_url=? OR (?=1 AND n.token_hash IS NOT NULL AND coalesce(c.current_url,'')='') OR m.source_url=? OR m.target_url=?
 ORDER BY n.id LIMIT 501`, target, id, target, wasDefault, target, target)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []AgentConnection{}
	for rows.Next() {
		var v AgentConnection
		if err = rows.Scan(&v.NodeID, &v.Name, &v.URL, &v.SeenAt, &v.Capable, &v.TargetURL, &v.Paired); err != nil {
			return nil, false, err
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(out) > 500
	if truncated {
		out = out[:500]
	}
	return out, truncated, nil
}
