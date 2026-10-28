package store

import (
	"context"
	"crypto/sha256"
	"io"
)

// ProbePublicPolicyFingerprint is private read-consistency evidence. It excludes
// changing observations and heartbeat timestamps, and includes identities and
// all stable settings that can change what the public projection may disclose.
// The digest and its inputs are never exposed in a public response.
func (s *Store) ProbePublicPolicyFingerprint(ctx context.Context) ([32]byte, error) {
	var out [32]byte
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM (
 SELECT 'nodes' AS kind,n.id AS position,json_array(n.id,p.uuid,n.token_hash IS NOT NULL AND n.token_hash<>'',n.probe_hidden,n.name,n.monitor_group,n.public_addr,n.v6_addr,n.probe_storage_json,n.probe_info_json,n.region_mode) AS body
 FROM nodes n LEFT JOIN probe_public_ids p ON p.node_id=n.id
 UNION ALL
 SELECT 'settings',key,json_array(key,value_json) FROM settings WHERE key IN ('probe','site')
 UNION ALL
 SELECT 'tasks',id,json_array(id,name,type,target,interval_seconds,node_ids_json,enabled) FROM ping_tasks
 ) ORDER BY kind,position`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	digest := sha256.New()
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			return out, err
		}
		io.WriteString(digest, body)
		io.WriteString(digest, "\n")
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	copy(out[:], digest.Sum(nil))
	return out, nil
}
