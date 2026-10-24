package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
)

var ErrNodeExpiryConflict = errors.New("server expiry changed; refresh before changing its date")

// AnchorDate is managed by the server. It survives a disabled switch so that a
// short month's clamped date never silently changes the billing/reset day.
type NodeAutoRenewal struct {
	Enabled    bool   `json:"enabled"`
	AnchorDate string `json:"anchor_date,omitempty"`
}

func nodeBillingMonths(b *NodeCost) int {
	if b == nil {
		return 0
	}
	switch b.Cycle {
	case "monthly":
		return 1
	case "quarterly":
		return 3
	case "semiannual":
		return 6
	case "annual":
		return 12
	case "custom":
		if !costYears.MatchString(b.Years) {
			return 0
		}
		parts := strings.SplitN(b.Years, ".", 2)
		fraction := ""
		if len(parts) == 2 {
			fraction = parts[1]
		}
		n, err := strconv.Atoi(parts[0] + fraction + strings.Repeat("0", 4-len(fraction)))
		if err == nil && n > 0 && n*12%10000 == 0 {
			return n * 12 / 10000
		}
	}
	return 0
}

func sameNodeBillingCycle(a, b *NodeCost) bool {
	if a == nil || b == nil {
		return a == b
	}
	if nodeBillingMonths(a) > 0 {
		return nodeBillingMonths(a) == nodeBillingMonths(b)
	}
	return a.Cycle == b.Cycle && a.Years == b.Years
}

func normalizeNodeAutoRenewal(info *NodeProbeInfo, old NodeProbeInfo) error {
	requested := info.AutoRenewal
	if requested == nil {
		requested = old.AutoRenewal // Old clients may omit or send null.
	}
	if requested == nil {
		return nil
	}
	next := NodeAutoRenewal{Enabled: requested.Enabled}
	if old.AutoRenewal != nil {
		next.AnchorDate = old.AutoRenewal.AnchorDate
	}
	if info.ExpiresAt != old.ExpiresAt || !sameNodeBillingCycle(info.Billing, old.Billing) {
		next.AnchorDate = info.ExpiresAt
	}
	if next.Enabled {
		if info.ExpiresAt == "" || nodeBillingMonths(info.Billing) == 0 {
			return ErrNodeCost
		}
		anchor, err := time.Parse("2006-01-02", next.AnchorDate)
		if err != nil || anchor.Year() < 1 {
			next.AnchorDate = info.ExpiresAt
		}
	}
	info.AutoRenewal = &next // Never trust a client-supplied anchor.
	return nil
}

func nodeExpiryAt(anchor time.Time, months int) time.Time {
	first := time.Date(anchor.Year(), anchor.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(anchor.Day(), last)-1)
}

// Calculate from the original calendar anchor, not the last clamped date or the
// reconnect time. Work is bounded even after many years without a report.
func nextNodeExpiry(info NodeProbeInfo, at time.Time) (string, bool) {
	r := info.AutoRenewal
	if r == nil || !r.Enabled {
		return "", false
	}
	expiry, err := time.Parse("2006-01-02", info.ExpiresAt)
	if err != nil || expiry.Year() < 1 || at.Before(expiry) {
		return "", false
	}
	anchor, err := time.Parse("2006-01-02", r.AnchorDate)
	months := nodeBillingMonths(info.Billing)
	if err != nil || anchor.Year() < 1 || months <= 0 {
		return "", false
	}
	at = at.UTC()
	elapsed := (at.Year()-anchor.Year())*12 + int(at.Month()-anchor.Month())
	periods := max(0, elapsed/months)
	next := nodeExpiryAt(anchor, periods*months)
	if !next.After(at) {
		next = nodeExpiryAt(anchor, (periods+1)*months)
	}
	if next.Year() > 9999 {
		return "", false
	}
	return next.Format("2006-01-02"), true
}

type nodeRenewalEpoch struct {
	Epoch    string `json:"epoch"`
	Sequence uint64 `json:"sequence"`
}

type nodeRenewalCursor struct {
	Identity string             `json:"identity"`
	Sampled  int64              `json:"sampled"`
	Epochs   []nodeRenewalEpoch `json:"epochs,omitempty"`
}

func meaningfulNodeResources(h spec.SystemStatus) bool {
	if h.Valid != nil {
		v := h.Valid
		return v.CPU || v.Memory || v.Swap || v.Disk || v.Network || v.Load || v.Connections || v.Processes || v.Uptime
	}
	// Legacy zero-value status is not evidence of a resource collection. Validity
	// defaults for legacy payloads alone would treat {} as a successful sample.
	return h.MemTotal > 0 || h.DiskTotal > 0 || h.Uptime > 0 || h.CPUPercent > 0 || h.NetTotalUp > 0 || h.NetTotalDown > 0
}

func requireNodeTokenTx(ctx context.Context, tx *sql.Tx, nodeID int64, tokenHash string) error {
	var current sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT token_hash FROM nodes WHERE id=?`, nodeID).Scan(&current); err != nil {
		return wrapNotFound(err)
	}
	if tokenHash == "" || !current.Valid || current.String != tokenHash {
		return ErrNotFound
	}
	return nil
}

// RecordNodeReport is called after validating the complete agent request. Its
// snapshot and optional renewal commit together against the current identity.
func (s *Store) RecordNodeReport(ctx context.Context, nodeID int64, tokenHash string, rep agentproto.Report, trafficFresh bool, at time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := requireNodeTokenTx(ctx, tx, nodeID, tokenHash); err != nil {
		return false, err
	}
	p, err := scanNodeProbe(tx.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id=?`, nodeID))
	if err != nil {
		return false, err
	}
	renewed, err := renewNodeExpiryTx(ctx, tx, p, tokenHash, rep.Host, at, trafficFresh)
	if err != nil {
		return false, err
	}
	hostJSON, err := json.Marshal(MonitorSnapshot(rep.Host, at))
	if err != nil {
		return false, err
	}
	coresJSON, err := json.Marshal(rep.Cores)
	if err != nil {
		return false, err
	}
	certsJSON, err := json.Marshal(rep.Certs)
	if err != nil {
		return false, err
	}
	if rep.Certs == nil {
		certsJSON = []byte("[]")
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET last_seen_at=?,version=COALESCE(NULLIF(?,''),version),applied_revision=?,host_status_json=?,host_reported_at=?,cores_json=?,certs_json=?,upgrade_to=CASE WHEN upgrade_to=? THEN '' ELSE upgrade_to END,updated_at=? WHERE id=?`,
		at.Unix(), rep.Version, rep.Revision, string(hostJSON), at.Unix(), string(coresJSON), string(certsJSON), rep.Version, at.Unix(), nodeID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return renewed, nil
}

// Called only after accepting an authenticated report/beat, within its write
// transaction. Ordinary pairing/liveness updates never reach this function.
func renewNodeExpiryTx(ctx context.Context, tx *sql.Tx, p *NodeProbe, tokenHash string, h spec.SystemStatus, at time.Time, legacyFresh bool) (bool, error) {
	if !meaningfulNodeResources(h) || p.Info.AutoRenewal == nil || !p.Info.AutoRenewal.Enabled {
		return false, nil
	}
	var raw, infoRaw string
	if err := tx.QueryRowContext(ctx, `SELECT probe_renewal_json,probe_info_json FROM nodes WHERE id=?`, p.NodeID).Scan(&raw, &infoRaw); err != nil {
		return false, err
	}
	// scanNodeProbe resolves an automatic region for display. Read saved metadata
	// before writing so a renewal changes only the expiry, not derived fields.
	var saved NodeProbeInfo
	if err := json.Unmarshal([]byte(infoRaw), &saved); err != nil {
		return false, err
	}
	var cursor nodeRenewalCursor
	if err := json.Unmarshal([]byte(raw), &cursor); err != nil {
		return false, err
	}
	if cursor.Identity != tokenHash {
		cursor = nodeRenewalCursor{Identity: tokenHash}
	}
	fresh := legacyFresh
	if r := h.Resources; r != nil {
		if r.Epoch != "" {
			if len(r.Epoch) > 128 || r.Sequence == 0 {
				return false, nil
			}
			fresh = true
			for i, e := range cursor.Epochs {
				if e.Epoch == r.Epoch {
					if r.Sequence <= e.Sequence {
						return false, nil
					}
					cursor.Epochs = append(cursor.Epochs[:i], cursor.Epochs[i+1:]...)
					break
				}
			}
			cursor.Epochs = append(cursor.Epochs, nodeRenewalEpoch{Epoch: r.Epoch, Sequence: r.Sequence})
			if len(cursor.Epochs) > 8 {
				cursor.Epochs = cursor.Epochs[len(cursor.Epochs)-8:]
			}
		}
		if r.At > 0 {
			// Source time supplements receipt time; it cannot make an old cached
			// resource report count as a post-expiry sample. Clock-skewed agents
			// keep reporting normally but do not automatically move dates.
			if r.At <= cursor.Sampled || r.At < at.Add(-5*time.Minute).UnixMilli() || r.At > at.Add(30*time.Second).UnixMilli() {
				fresh = false
			} else {
				cursor.Sampled = r.At
			}
			if expiry, err := time.Parse("2006-01-02", p.Info.ExpiresAt); err == nil && r.At < expiry.UnixMilli() {
				fresh = false
			}
		}
	}
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET probe_renewal_json=? WHERE id=?`, string(encoded), p.NodeID); err != nil {
		return false, err
	}
	next, due := nextNodeExpiry(saved, at)
	if !fresh || !due {
		return false, nil
	}
	previous := saved.ExpiresAt
	saved.ExpiresAt = next
	p.Info.ExpiresAt = next
	info, err := json.Marshal(saved)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET probe_info_json=?,updated_at=? WHERE id=?`, string(info), at.Unix(), p.NodeID); err != nil {
		return false, err
	}
	// The audit entry commits with the date. No network operation or payment is
	// performed, and a retry after a rollback cannot lose the audit or renew twice.
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_log (at,role,method,path,target,status,via) VALUES (?,?,?,?,?,?,?)`, at.Unix(), "system", "AUTO", fmt.Sprintf("/api/admin/nodes/%d/probe", p.NodeID), previous+" → "+next, 200, "probe_auto_renewal")
	return err == nil, err
}
