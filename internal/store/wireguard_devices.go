package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/wg"
	"github.com/zeptop-dev/captain/internal/auth"
)

const MaxWireGuardDevices = 16
const WireGuardDeviceTokenPrefix = "wgd_"

var ErrWireGuardDeviceName = errors.New("device name must contain 1-64 printable characters")
var ErrWireGuardDeviceLimit = errors.New("WireGuard device limit reached")
var ErrWireGuardDevicePool = errors.New("WireGuard device address pool exhausted")

// Token is a separate WG-only subscription credential, never the user's general token.
type WireGuardDevice struct {
	ID          int64      `json:"id"`
	AddressSlot int64      `json:"-"`
	UserID      int64      `json:"-"`
	Name        string     `json:"name"`
	UUID        string     `json:"-"`
	Token       string     `json:"-"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
}

const wgDeviceColumns = `id,address_slot,user_id,name,uuid,token,created_at,last_used_at`

func scanWGDevice(row interface{ Scan(...any) error }) (*WireGuardDevice, error) {
	var d WireGuardDevice
	var created int64
	var used sql.NullInt64
	if err := row.Scan(&d.ID, &d.AddressSlot, &d.UserID, &d.Name, &d.UUID, &d.Token, &created, &used); err != nil {
		return nil, wrapNotFound(err)
	}
	d.CreatedAt, d.LastUsedAt = unix(created), unixPtr(used)
	return &d, nil
}

func validWGDeviceName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 64 || !spec.PublicLabel(name) {
		return "", ErrWireGuardDeviceName
	}
	return name, nil
}

func (s *Store) WireGuardDevices(ctx context.Context, userID int64) ([]WireGuardDevice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+wgDeviceColumns+` FROM wireguard_devices WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WireGuardDevice{}
	for rows.Next() {
		d, err := scanWGDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) CreateWireGuardDevice(ctx context.Context, userID int64, name string, limit int) (*WireGuardDevice, error) {
	return s.CreateWireGuardDeviceWithHWID(ctx, userID, name, limit, nil)
}

// sharedLimit includes already registered HWID clients when HWID policy is enabled.
// Native devices reserve their slot here, not at the first subscription fetch.
func (s *Store) CreateWireGuardDeviceWithHWID(ctx context.Context, userID int64, name string, limit int, sharedLimit *int) (*WireGuardDevice, error) {
	name, err := validWGDeviceName(name)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > MaxWireGuardDevices {
		limit = MaxWireGuardDevices
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM wireguard_devices WHERE user_id=?`, userID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= limit {
		return nil, ErrWireGuardDeviceLimit
	}
	if sharedLimit != nil && *sharedLimit > 0 {
		var hwids int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM hwid_devices WHERE user_id=?`, userID).Scan(&hwids); err != nil {
			return nil, err
		}
		if count+hwids >= *sharedLimit {
			return nil, ErrWireGuardDeviceLimit
		}
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id=? AND role='user'`, userID).Scan(&status); err != nil {
		return nil, wrapNotFound(err)
	}
	if status != "active" {
		return nil, ErrNotFound
	}
	d := &WireGuardDevice{UserID: userID, Name: name, UUID: auth.UUID(), Token: WireGuardDeviceTokenPrefix + auth.Token(32), CreatedAt: time.Unix(now(), 0)}
	// The first free slot is either 1 or follows an occupied slot. Allocation and
	// insertion share this transaction; deleting a device does not burn a global ID pool.
	var slot sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT MIN(candidate) FROM (
 SELECT 1 AS candidate UNION ALL SELECT address_slot+1 FROM wireguard_devices WHERE address_slot<65534
 ) WHERE NOT EXISTS(SELECT 1 FROM wireguard_devices WHERE address_slot=candidate)`).Scan(&slot)
	if err != nil {
		return nil, err
	}
	if !slot.Valid {
		return nil, ErrWireGuardDevicePool
	}
	d.AddressSlot = slot.Int64
	if _, err = wg.DeviceAddress(d.AddressSlot); err != nil {
		return nil, ErrWireGuardDevicePool
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO wireguard_devices(address_slot,user_id,name,uuid,token,created_at) VALUES(?,?,?,?,?,?)`, d.AddressSlot, userID, name, d.UUID, d.Token, d.CreatedAt.Unix())
	if err != nil {
		return nil, err
	}
	d.ID, err = res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) RenameWireGuardDevice(ctx context.Context, userID, id int64, name string) (*WireGuardDevice, error) {
	name, err := validWGDeviceName(name)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE wireguard_devices SET name=? WHERE id=? AND user_id=?`, name, id, userID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return scanWGDevice(s.db.QueryRowContext(ctx, `SELECT `+wgDeviceColumns+` FROM wireguard_devices WHERE id=? AND user_id=?`, id, userID))
}

func (s *Store) DeleteWireGuardDevice(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM wireguard_devices WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) WireGuardDeviceByToken(ctx context.Context, token string) (*WireGuardDevice, error) {
	if !strings.HasPrefix(token, WireGuardDeviceTokenPrefix) || len(token) > 128 {
		return nil, ErrNotFound
	}
	return scanWGDevice(s.db.QueryRowContext(ctx, `SELECT `+wgDeviceColumns+` FROM wireguard_devices WHERE token=?`, token))
}

func (s *Store) TouchWireGuardDevice(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE wireguard_devices SET last_used_at=? WHERE id=? AND (last_used_at IS NULL OR last_used_at<?)`, at.Unix(), id, at.Add(-5*time.Minute).Unix())
	return err
}

// WireGuardDevicePeers indexes only active customers by their immutable agent identity.
func (s *Store) WireGuardDevicePeers(ctx context.Context) (map[int64][]spec.WireGuardPeer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.agent_id,d.address_slot,d.uuid FROM wireguard_devices d JOIN users u ON u.id=d.user_id WHERE u.role='user' AND u.status='active' ORDER BY d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]spec.WireGuardPeer{}
	for rows.Next() {
		var agentID, id int64
		var uuid string
		if err = rows.Scan(&agentID, &id, &uuid); err != nil {
			return nil, err
		}
		addr, err := wg.DeviceAddress(id)
		if err != nil {
			return nil, err
		}
		out[agentID] = append(out[agentID], spec.WireGuardPeer{UUID: uuid, Address: addr})
	}
	return out, rows.Err()
}

// WireGuardDevicesCapability distinguishes an unknown/temporarily unavailable
// capability from a binary that explicitly cannot serve independently keyed devices.
// Freshness is required when registering new devices. Existing credentials and agent
// state use the most recent capability for this pairing, surviving brief disconnects.
func (s *Store) WireGuardDevicesCapability(ctx context.Context, nodeID int64, fresh bool) string {
	n, err := s.NodeByID(ctx, nodeID)
	if err != nil || !n.Paired || !NodeProxyAllowed(n) || n.LastSeenAt == nil {
		return "unavailable"
	}
	if !lineVersionOK(n, "v0.77.0") {
		return "upgrade_required"
	}
	if fresh {
		age := time.Since(*n.LastSeenAt)
		if age < 0 || age > 3*time.Minute {
			return "unavailable"
		}
	}
	st, err := s.NodeStatus(ctx, nodeID)
	if err != nil {
		return "unavailable"
	}
	var cores map[string]agentproto.CoreStatus
	if json.Unmarshal(st.Cores, &cores) != nil {
		return "unavailable"
	}
	c, exists := cores["xray"]
	if !exists || c.Capabilities == nil {
		return "unavailable"
	}
	if !c.Capabilities.WireGuardDevices {
		return "upgrade_required"
	}
	return ""
}

func (s *Store) WireGuardDevicesCapable(ctx context.Context, nodeID int64) bool {
	return s.WireGuardDevicesCapability(ctx, nodeID, true) == ""
}

func (s *Store) WireGuardDevicesSupported(ctx context.Context, nodeID int64) bool {
	return s.WireGuardDevicesCapability(ctx, nodeID, false) == ""
}
