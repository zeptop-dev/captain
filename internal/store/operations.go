package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

var ErrOperationQuery = errors.New("invalid operation query")

var OperationSources = []string{"line_rollout", "path_rollout", "node_job", "topology_changeset", "node_upgrade"}

// OperationAccess is a server-built projection of the caller's existing read
// permissions. Filtering happens before pagination, counts or links are exposed.
type OperationAccess struct {
	LineRollouts, PathRollouts, NodeJobs, TopologyChangesets, NodeUpgrades bool
	TopologyInbounds, TopologyLines, TopologyEntries                       bool
}

type OperationQuery struct {
	Source, Status, Before string
	NodeID, LineID         int64
	Limit                  int
}

type Operation struct {
	ID                string              `json:"id"`
	Source            string              `json:"source"`
	SourceID          string              `json:"source_id"`
	Kind              string              `json:"kind"`
	Status            string              `json:"status"`
	Evidence          string              `json:"evidence"`
	Name              string              `json:"name,omitempty"`
	Actor             string              `json:"actor,omitempty"`
	Stage             string              `json:"stage,omitempty"`
	NodeID            int64               `json:"node_id,omitempty"`
	LineID            int64               `json:"line_id,omitempty"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         *time.Time          `json:"updated_at,omitempty"`
	CompletedAt       *time.Time          `json:"completed_at,omitempty"`
	Action            string              `json:"action,omitempty"`
	Outcome           string              `json:"outcome,omitempty"`
	AttemptsTruncated bool                `json:"attempts_truncated,omitempty"`
	TargetVersion     string              `json:"target_version,omitempty"`
	ErrorCode         string              `json:"error_code,omitempty"`
	Steps             []OperationStep     `json:"steps,omitempty"`
	Attempts          []OperationAttempt  `json:"attempts,omitempty"`
	Related           []OperationRelation `json:"related,omitempty"`
}

type OperationStep struct {
	ID             string     `json:"id"`
	NodeID         int64      `json:"node_id"`
	NodeName       string     `json:"node_name"`
	Phase          string     `json:"phase"`
	Order          int        `json:"order"`
	Attempt        int        `json:"attempt"`
	Status         string     `json:"status"`
	IssuedAt       *time.Time `json:"issued_at,omitempty"`
	ConfirmedAt    *time.Time `json:"confirmed_at,omitempty"`
	ObservedAt     *time.Time `json:"observed_at,omitempty"`
	LastReceiptAt  *time.Time `json:"last_receipt_at,omitempty"`
	FreshForMS     int64      `json:"fresh_for_ms"`
	ReceiptApplied bool       `json:"receipt_applied"`
}

type OperationAttempt struct {
	StepID     string    `json:"step_id"`
	Attempt    int       `json:"attempt"`
	ObservedAt time.Time `json:"observed_at"`
	ReceivedAt time.Time `json:"received_at"`
	Applied    bool      `json:"applied"`
}

type OperationRelation struct {
	Relation    string `json:"relation"`
	OperationID string `json:"operation_id"`
	Available   bool   `json:"available"`
}

type OperationPage struct {
	Items      []Operation `json:"items"`
	NextBefore string      `json:"next_before,omitempty"`
}

type operationCursor struct {
	Version int    `json:"v"`
	Created int64  `json:"t"`
	Source  string `json:"s"`
	ID      string `json:"i"`
}

func ParseOperationID(id string) (string, string, error) {
	source, key, ok := strings.Cut(id, ":")
	if !ok || !slices.Contains(OperationSources, source) || key == "" || len(key) > 160 || strings.ContainsAny(key, "/\\\x00\r\n") {
		return "", "", ErrOperationQuery
	}
	return source, key, nil
}

func validateOperationQuery(q OperationQuery) (*operationCursor, error) {
	if q.Limit < 1 || q.Limit > 100 || q.NodeID < 0 || q.LineID < 0 || (q.Source != "" && !slices.Contains(OperationSources, q.Source)) {
		return nil, ErrOperationQuery
	}
	if q.Status != "" && !slices.Contains([]string{"pending", "running", "waiting", "paused", "completed", "compensated", "failed", "expired", "draft", "committed", "observed", "superseded", "cancelled", "identity_changed", "unknown"}, q.Status) {
		return nil, ErrOperationQuery
	}
	if q.Before == "" {
		return nil, nil
	}
	if len(q.Before) > 512 {
		return nil, ErrOperationQuery
	}
	raw, e := base64.RawURLEncoding.DecodeString(q.Before)
	if e != nil {
		return nil, ErrOperationQuery
	}
	var c operationCursor
	if json.Unmarshal(raw, &c) != nil || c.Version != 1 || c.Created < 0 {
		return nil, ErrOperationQuery
	}
	if _, _, e = ParseOperationID(c.Source + ":" + c.ID); e != nil {
		return nil, ErrOperationQuery
	}
	return &c, nil
}

// All projections omit input/result blobs, arbitrary errors and credentials.
// No source reader that expires jobs or advances a scheduler is called here.
func operationUnion(a OperationAccess, at time.Time) (string, []any) {
	jobsExpiry := `CASE WHEN json_valid(j.params_json) AND json_type(j.params_json,'$.expires_at')='integer' AND json_extract(j.params_json,'$.expires_at')>0 THEN json_extract(j.params_json,'$.expires_at') WHEN j.kind='host_manage' THEN j.created_at+300 WHEN j.kind='core_manage' THEN j.created_at+900 WHEN j.kind='network_diagnostic' THEN j.created_at+120 WHEN j.kind='wireguard_setup' THEN j.created_at+300 ELSE NULL END`
	jobStatus := `CASE WHEN j.done_at IS NOT NULL AND j.error<>'' THEN CASE WHEN j.error IN ('host task expired; inspect actual state before retry','core operation expired','diagnostic request expired','removal request expired','readiness check expired','wireguard setup expired') THEN 'expired' ELSE 'failed' END WHEN j.done_at IS NOT NULL AND j.kind='host_manage' THEN CASE WHEN json_valid(j.result_json) THEN CASE json_extract(j.result_json,'$.outcome') WHEN 'waiting' THEN 'waiting' WHEN 'failed' THEN 'failed' WHEN 'invalid' THEN 'failed' WHEN 'unsupported' THEN 'failed' WHEN 'recovery_failed' THEN 'failed' WHEN 'ok' THEN 'completed' WHEN 'complete' THEN 'completed' WHEN 'restored' THEN 'completed' ELSE 'unknown' END ELSE 'unknown' END WHEN j.done_at IS NOT NULL THEN 'completed' WHEN j.kind='node_remove' AND json_valid(j.result_json) AND json_extract(j.result_json,'$.phase')='running' THEN 'running' WHEN (` + jobsExpiry + `)<=? THEN 'expired' ELSE 'pending' END`
	query := `SELECT CASE WHEN p.snapshot_version='path-relay-v1' THEN 'path_rollout' ELSE 'line_rollout' END source,p.id source_id,p.kind,p.status,p.created_at,p.updated_at,NULL completed_at,0 node_id,p.line_id,
 CASE WHEN json_valid(p.after_json) AND json_type(p.after_json,'$.name')='text' THEN json_extract(p.after_json,'$.name') WHEN json_valid(p.before_json) AND json_type(p.before_json,'$.name')='text' THEN json_extract(p.before_json,'$.name') ELSE '' END name,p.actor,p.stage,p.error_code,'' target_version
 FROM line_rollouts p WHERE (p.snapshot_version='path-relay-v1' AND ?) OR (p.snapshot_version='line-relay-v1' AND ?)
 UNION ALL SELECT 'node_job',j.id,j.kind,` + jobStatus + `,j.created_at*1000,COALESCE(j.done_at,j.created_at)*1000,j.done_at*1000,j.node_id,0,n.name,'','','','' FROM node_jobs j JOIN nodes n ON n.id=j.node_id WHERE ?
 UNION ALL SELECT 'topology_changeset',d.id,'topology',d.status,d.created_at*1000,d.updated_at*1000,d.committed_at*1000,0,0,d.name,d.actor,'','','' FROM topology_changesets d WHERE ? AND json_valid(d.operations_json) AND NOT EXISTS(SELECT 1 FROM json_each(d.operations_json) op WHERE NOT ((json_extract(op.value,'$.kind')='inbound' AND ?) OR (json_extract(op.value,'$.kind')='line' AND ?) OR (json_extract(op.value,'$.kind')='entry' AND ?)) OR json_extract(op.value,'$.kind') IS NULL)
 UNION ALL SELECT 'node_upgrade',u.id,'upgrade',u.status,u.created_at,u.updated_at,u.completed_at,u.node_id,0,u.node_name,u.actor,'','',u.target_version FROM node_upgrade_operations u WHERE ?`
	return query, []any{a.PathRollouts, a.LineRollouts, at.Unix(), a.NodeJobs, a.TopologyChangesets, a.TopologyInbounds, a.TopologyLines, a.TopologyEntries, a.NodeUpgrades}
}

func scanOperation(row rowScanner) (Operation, error) {
	var o Operation
	var created int64
	var updated, done sql.NullInt64
	err := row.Scan(&o.Source, &o.SourceID, &o.Kind, &o.Status, &created, &updated, &done, &o.NodeID, &o.LineID, &o.Name, &o.Actor, &o.Stage, &o.ErrorCode, &o.TargetVersion)
	o.ID = o.Source + ":" + o.SourceID
	o.Evidence = "historical"
	o.CreatedAt = time.UnixMilli(created).UTC()
	o.UpdatedAt = lineMillisPtr(updated)
	o.CompletedAt = lineMillisPtr(done)
	if o.Source == "node_job" {
		if o.Status == "expired" {
			o.ErrorCode = "request_expired"
		} else if o.Status == "failed" {
			o.ErrorCode = "job_failed"
		}
	}
	return o, err
}

func (s *Store) Operations(ctx context.Context, q OperationQuery, a OperationAccess, at time.Time) (*OperationPage, error) {
	cursor, err := validateOperationQuery(q)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	union, args := operationUnion(a, at)
	where := ` WHERE (?='' OR source=?) AND (?='' OR status=?) AND (?=0 OR node_id=? OR (source IN ('line_rollout','path_rollout') AND EXISTS(SELECT 1 FROM line_rollout_nodes n WHERE n.plan_id=source_id AND n.node_id=?))) AND (?=0 OR line_id=?)`
	args = append(args, q.Source, q.Source, q.Status, q.Status, q.NodeID, q.NodeID, q.NodeID, q.LineID, q.LineID)
	if cursor != nil {
		where += ` AND (created_at,source,source_id)<(?,?,?)`
		args = append(args, cursor.Created, cursor.Source, cursor.ID)
	}
	args = append(args, q.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT * FROM (`+union+`)`+where+` ORDER BY created_at DESC,source DESC,source_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	out := &OperationPage{Items: []Operation{}}
	for rows.Next() {
		o, e := scanOperation(rows)
		if e != nil {
			err = e
			break
		}
		out.Items = append(out.Items, o)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(out.Items) > q.Limit {
		out.Items = out.Items[:q.Limit]
		last := out.Items[q.Limit-1]
		raw, _ := json.Marshal(operationCursor{1, last.CreatedAt.UnixMilli(), last.Source, last.SourceID})
		out.NextBefore = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, tx.Commit()
}

func (s *Store) Operation(ctx context.Context, id string, a OperationAccess, at time.Time) (*Operation, error) {
	source, key, err := ParseOperationID(id)
	if err != nil {
		return nil, ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	union, args := operationUnion(a, at)
	args = append(args, source, key)
	o, err := scanOperation(tx.QueryRowContext(ctx, `SELECT * FROM (`+union+`) WHERE source=? AND source_id=?`, args...))
	if err != nil {
		return nil, wrapNotFound(err)
	}
	switch source {
	case "line_rollout", "path_rollout":
		p, e := readLineRolloutTx(ctx, tx, key, at)
		if e != nil {
			return nil, e
		}
		for _, t := range p.Targets {
			o.Steps = append(o.Steps, OperationStep{t.ID, t.NodeID, t.NodeName, t.Phase, t.Order, t.Attempt, t.Status, t.IssuedAt, t.ConfirmedAt, t.ObservedAt, t.LastReceiptAt, t.FreshForMS, t.ReceiptApplied})
		}
		rows, e := tx.QueryContext(ctx, `SELECT a.step_id,a.attempt,a.observed_at,a.received_at,a.applied FROM line_rollout_attempts a JOIN line_rollout_steps s ON s.id=a.step_id WHERE s.plan_id=? ORDER BY s.ordinal,a.attempt DESC LIMIT 1001`, key)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var v OperationAttempt
			var observed, received int64
			if e = rows.Scan(&v.StepID, &v.Attempt, &observed, &received, &v.Applied); e != nil {
				break
			}
			v.ObservedAt = time.UnixMilli(observed).UTC()
			v.ReceivedAt = time.UnixMilli(received).UTC()
			o.Attempts = append(o.Attempts, v)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(o.Attempts) > 1000 {
			o.Attempts = o.Attempts[:1000]
			o.AttemptsTruncated = true
		}
		if e = operationRelations(ctx, tx, &o, "line_rollouts", "compensates", p.Compensates, "compensates", "compensated_by"); e != nil {
			return nil, e
		}
	case "node_job":
		var params, result string
		if e := tx.QueryRowContext(ctx, `SELECT params_json,result_json FROM node_jobs WHERE id=?`, key).Scan(&params, &result); e != nil {
			return nil, e
		}
		var p struct {
			Action string `json:"action"`
		}
		var v struct {
			Outcome string `json:"outcome"`
		}
		if len(params) <= 1<<20 && json.Unmarshal([]byte(params), &p) == nil && slices.Contains([]string{"inspect", "confirm", "rollback", "apply", "download", "activate", "check", "install"}, p.Action) {
			o.Action = p.Action
		}
		if o.Kind == "host_manage" && len(result) <= 1<<20 && json.Unmarshal([]byte(result), &v) == nil && slices.Contains([]string{"invalid", "ok", "unavailable", "failed", "unsupported", "unknown", "restored", "complete", "waiting", "applying", "restoring", "recovery_failed", "journal_invalid"}, v.Outcome) {
			o.Outcome = v.Outcome
		}
	case "node_upgrade":
		var previous string
		if e := tx.QueryRowContext(ctx, `SELECT supersedes FROM node_upgrade_operations WHERE id=?`, key).Scan(&previous); e != nil {
			return nil, e
		}
		if e := operationRelations(ctx, tx, &o, "node_upgrade_operations", "supersedes", previous, "supersedes", "superseded_by"); e != nil {
			return nil, e
		}
	}
	return &o, tx.Commit()
}

func operationRelations(ctx context.Context, tx *sql.Tx, o *Operation, table, column, previous, forward, reverse string) error {
	constraint := ""
	if table == "line_rollouts" {
		if o.Source == "path_rollout" {
			constraint = " AND snapshot_version='path-relay-v1'"
		} else {
			constraint = " AND snapshot_version='line-relay-v1'"
		}
	}
	if previous != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=?`+constraint+`)`, previous).Scan(&exists); err != nil {
			return err
		}
		// A reference to a retained record of another source must not leak its
		// identity under this source's permissions. A genuinely pruned reference
		// remains useful as historical provenance, explicitly unavailable.
		var otherSource bool
		if !exists && constraint != "" {
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=?)`, previous).Scan(&otherSource); err != nil {
				return err
			}
		}
		if !otherSource {
			o.Related = append(o.Related, OperationRelation{forward, o.Source + ":" + previous, exists})
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM `+table+` WHERE `+column+`=?`+constraint+` ORDER BY created_at DESC,id DESC LIMIT 100`, o.SourceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return err
		}
		o.Related = append(o.Related, OperationRelation{reverse, o.Source + ":" + id, true})
	}
	return rows.Err()
}
