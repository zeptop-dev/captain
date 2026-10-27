package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/currency"
)

var (
	ErrInfrastructureConflict   = errors.New("server infrastructure changed; reload before saving")
	ErrInfrastructurePermission = errors.New("shared server billing requires infrastructure write permission")
	ErrInfrastructureLinked     = errors.New("unlink the primary asset before deleting or moving it")
	ErrInfrastructureConversion = errors.New("linked billing requires an exact currency amount and a supported whole-month cycle from 1 to 120 months")
)

// This relation never appears in NodeProbeInfo, which can be published.
type InfraSupplierChoice struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}
type NodeInfrastructure struct {
	Revision    string               `json:"revision"`
	Supplier    *InfraSupplierChoice `json:"supplier"`
	Asset       *InfraAsset          `json:"asset"`
	Billing     *NodeCost            `json:"billing"`
	Price       string               `json:"price"`
	ExpiresAt   string               `json:"expires_at"`
	AutoRenewal *NodeAutoRenewal     `json:"auto_renewal"`
}
type NodeInfrastructureChange struct {
	Revision      string `json:"revision"`
	Action        string `json:"action"`
	SupplierID    *int64 `json:"supplier_id"`
	AssetID       int64  `json:"asset_id"`
	AssetRevision int64  `json:"asset_revision"`
	Name          string `json:"name"`
	RemindDays    int    `json:"remind_days"`
	// Accepted for pre-release editor compatibility; linking preserves the
	// server's switch and this old confirmation no longer changes its behavior.
	ConfirmDisableAutoRenewal bool `json:"confirm_disable_auto_renewal"`
}
type NodeInfrastructureGuard struct {
	Revision   *string
	SupplierID *int64
	CanWrite   bool
}
type nodeInfrastructureLink struct {
	SupplierID, AssetID sql.NullInt64
	Revision            int64
}

func nodeInfrastructureLinkTx(ctx context.Context, tx *sql.Tx, nodeID int64) (nodeInfrastructureLink, error) {
	var v nodeInfrastructureLink
	err := tx.QueryRowContext(ctx, `SELECT supplier_id,asset_id,revision FROM node_infrastructure WHERE node_id=?`, nodeID).Scan(&v.SupplierID, &v.AssetID, &v.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return v, err
}
func nodeInfraProbeTx(ctx context.Context, tx *sql.Tx, nodeID int64) (*NodeProbe, error) {
	p, err := scanNodeProbe(tx.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id=?`, nodeID))
	if err != nil {
		return nil, err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT probe_info_json FROM nodes WHERE id=?`, nodeID).Scan(&raw); err != nil {
		return nil, err
	}
	// Preserve stored metadata rather than persisting display-time geolocation.
	p.Info = NodeProbeInfo{}
	if err = json.Unmarshal([]byte(raw), &p.Info); err != nil {
		return nil, err
	}
	return p, nil
}
func nodeInfrastructureTx(ctx context.Context, tx *sql.Tx, nodeID int64, info NodeProbeInfo) (*NodeInfrastructure, nodeInfrastructureLink, error) {
	l, err := nodeInfrastructureLinkTx(ctx, tx, nodeID)
	if err != nil {
		return nil, l, err
	}
	v := &NodeInfrastructure{Billing: info.Billing, Price: info.Price, ExpiresAt: info.ExpiresAt, AutoRenewal: info.AutoRenewal}
	if l.SupplierID.Valid {
		v.Supplier = &InfraSupplierChoice{}
		if err = tx.QueryRowContext(ctx, `SELECT id,name,url FROM infra_suppliers WHERE id=?`, l.SupplierID.Int64).Scan(&v.Supplier.ID, &v.Supplier.Name, &v.Supplier.URL); err != nil {
			return nil, l, err
		}
	}
	if l.AssetID.Valid {
		a, e := scanInfraAsset(tx.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM infra_assets a JOIN infra_suppliers s ON s.id=a.supplier_id WHERE a.id=?`, l.AssetID.Int64))
		if e != nil {
			return nil, l, e
		}
		v.Asset = &a
	}
	// Include current editable finance fields: even an unlinked server may have
	// renewed while the create/link confirmation was open.
	var identity string
	var created int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(token_hash,pair_code,''),created_at FROM nodes WHERE id=?`, nodeID).Scan(&identity, &created); err != nil {
		return nil, l, err
	}
	raw, _ := json.Marshal(struct {
		Node          int64
		Identity      string
		Created       int64
		Link          nodeInfrastructureLink
		View          *NodeInfrastructure
		Provider, URL string
	}{nodeID, identity, created, l, v, info.Provider, info.ProviderURL})
	sum := sha256.Sum256(raw)
	v.Revision = hex.EncodeToString(sum[:])
	return v, l, nil
}
func (s *Store) NodeInfrastructure(ctx context.Context, nodeID int64) (*NodeInfrastructure, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := nodeInfraProbeTx(ctx, tx, nodeID)
	if err != nil {
		return nil, err
	}
	v, _, err := nodeInfrastructureTx(ctx, tx, nodeID, p.Info)
	return v, err
}

// Probe values and their private optimistic revision must describe one read.
func (s *Store) NodeProbeInfrastructure(ctx context.Context, nodeID int64) (*NodeProbe, string, bool, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", false, false, err
	}
	defer tx.Rollback()
	p, err := scanNodeProbe(tx.QueryRowContext(ctx, `SELECT `+nodeProbeCols+` FROM nodes WHERE id=?`, nodeID))
	if err != nil {
		return nil, "", false, false, err
	}
	v, l, err := nodeInfrastructureTx(ctx, tx, nodeID, p.Info)
	if err != nil {
		return nil, "", false, false, err
	}
	return p, v.Revision, l.AssetID.Valid, l.SupplierID.Valid, nil
}

func saveNodeInfrastructureLinkTx(ctx context.Context, tx *sql.Tx, nodeID int64, supplier, asset sql.NullInt64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO node_infrastructure(node_id,supplier_id,asset_id) VALUES(?,?,?) ON CONFLICT(node_id) DO UPDATE SET supplier_id=excluded.supplier_id,asset_id=excluded.asset_id,revision=node_infrastructure.revision+1`, nodeID, supplier, asset)
	return err
}
func sharedInfrastructureAllowed(permission []bool) bool {
	return len(permission) == 0 || permission[0]
}

func costMinor(b *NodeCost) (int64, error) {
	if b == nil || !ValidCurrency(b.Currency) || !costAmount.MatchString(b.Amount) {
		return 0, ErrInfrastructureConversion
	}
	u, _ := currency.ParseISO(b.Currency)
	digits, _ := currency.Standard.Rounding(u)
	parts := strings.SplitN(b.Amount, ".", 2)
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > digits {
		if strings.Trim(fraction[digits:], "0") != "" {
			return 0, ErrInfrastructureConversion
		}
		fraction = fraction[:digits]
	}
	n, err := strconv.ParseInt(parts[0]+fraction+strings.Repeat("0", digits-len(fraction)), 10, 64)
	if err != nil || n > 1_000_000_000_000 {
		return 0, ErrInfrastructureConversion
	}
	return n, nil
}
func assetNodeCost(a InfraAsset) (*NodeCost, error) {
	if !ValidCurrency(a.Currency) || a.PeriodMonths < 1 || a.PeriodMonths > 120 {
		return nil, ErrInfrastructureConversion
	}
	b := &NodeCost{Currency: a.Currency}
	u, _ := currency.ParseISO(a.Currency)
	digits, _ := currency.Standard.Rounding(u)
	minor := strconv.FormatInt(a.AmountMinor, 10)
	if digits > 0 {
		minor = strings.Repeat("0", max(0, digits+1-len(minor))) + minor
		minor = minor[:len(minor)-digits] + "." + minor[len(minor)-digits:]
	}
	b.Amount = minor
	switch a.PeriodMonths {
	case 1:
		b.Cycle = "monthly"
	case 3:
		b.Cycle = "quarterly"
	case 6:
		b.Cycle = "semiannual"
	case 12:
		b.Cycle = "annual"
	default:
		// Existing custom years have at most four decimals; a third cannot be
		// represented exactly. Keep that compatibility boundary explicit.
		if a.PeriodMonths%3 != 0 {
			return nil, ErrInfrastructureConversion
		}
		b.Cycle = "custom"
		b.Years = strconv.Itoa(a.PeriodMonths / 12)
		if rem := a.PeriodMonths % 12; rem != 0 {
			b.Years += "." + fmt.Sprintf("%02d", rem*100/12)
		}
	}
	if _, err := costMinor(b); err != nil {
		return nil, err
	}
	return b, nil
}
func infoAsset(info NodeProbeInfo) (InfraAsset, error) {
	n, err := costMinor(info.Billing)
	if err != nil {
		return InfraAsset{}, err
	}
	months := nodeBillingMonths(info.Billing)
	date, err := infraDate(info.ExpiresAt)
	if err != nil {
		return InfraAsset{}, ErrInfrastructureConversion
	}
	a := InfraAsset{Currency: info.Billing.Currency, AmountMinor: n, PeriodMonths: months, NextDue: info.ExpiresAt, AnchorDay: date.Day(), Active: true}
	if _, err = assetNodeCost(a); err != nil {
		return InfraAsset{}, err
	}
	if info.AutoRenewal != nil {
		if anchor, e := infraDate(info.AutoRenewal.AnchorDate); e == nil {
			a.AnchorDay = anchor.Day()
		}
		if info.AutoRenewal.AnchorDay >= 1 && info.AutoRenewal.AnchorDay <= 31 {
			a.AnchorDay = info.AutoRenewal.AnchorDay
		}
	}
	return a, nil
}
func projectAssetRenewal(info *NodeProbeInfo, a InfraAsset) {
	enabled := info.AutoRenewal != nil && info.AutoRenewal.Enabled
	info.AutoRenewal = &NodeAutoRenewal{Enabled: enabled, AnchorDate: a.NextDue, AnchorDay: a.AnchorDay}
}
func projectAsset(info *NodeProbeInfo, a InfraAsset, supplier InfraSupplierChoice) error {
	b, err := assetNodeCost(a)
	if err != nil {
		return err
	}
	old := *info
	info.Billing = b
	info.Price = b.Display()
	info.ExpiresAt = a.NextDue
	info.Provider = supplier.Name
	info.ProviderURL = supplier.URL
	projectAssetRenewal(info, a)
	// An unchanged bill must not rewrite its equivalent original cycle anchor
	// after an online extension, especially for notes-only legacy asset edits.
	if old.AutoRenewal != nil && old.ExpiresAt == a.NextDue && sameNodeBillingCycle(old.Billing, b) {
		if anchor, e := infraDate(old.AutoRenewal.AnchorDate); e == nil {
			day := anchor.Day()
			if old.AutoRenewal.AnchorDay >= 1 && old.AutoRenewal.AnchorDay <= 31 {
				day = old.AutoRenewal.AnchorDay
			}
			if day == a.AnchorDay {
				r := *old.AutoRenewal
				info.AutoRenewal = &r
			}
		}
	}
	return nil
}
func persistNodeInfraInfoTx(ctx context.Context, tx *sql.Tx, p *NodeProbe, info NodeProbeInfo) error {
	next := *p
	next.Info = info
	at := time.Now()
	if !nodePeriodStart(at, p).Equal(nodePeriodStart(at, &next)) {
		if _, err := tx.ExecContext(ctx, `UPDATE nodes SET traffic_period_start=? WHERE id=?`, nodePeriodStart(at, &next).Unix(), p.NodeID); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET probe_info_json=?,updated_at=? WHERE id=?`, string(raw), now(), p.NodeID)
	return err
}
func supplierChoiceTx(ctx context.Context, tx *sql.Tx, id int64) (InfraSupplierChoice, error) {
	var v InfraSupplierChoice
	err := tx.QueryRowContext(ctx, `SELECT id,name,url FROM infra_suppliers WHERE id=?`, id).Scan(&v.ID, &v.Name, &v.URL)
	return v, wrapNotFound(err)
}

func (s *Store) ChangeNodeInfrastructure(ctx context.Context, nodeID int64, in NodeInfrastructureChange) (*NodeInfrastructure, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := nodeInfraProbeTx(ctx, tx, nodeID)
	if err != nil {
		return nil, err
	}
	v, l, err := nodeInfrastructureTx(ctx, tx, nodeID, p.Info)
	if err != nil {
		return nil, err
	}
	if in.Revision == "" || in.Revision != v.Revision {
		return nil, ErrInfrastructureConflict
	}
	info := p.Info
	switch in.Action {
	case "supplier":
		if in.SupplierID == nil {
			return nil, ErrAssetInput
		}
		if err = changeNodeSupplierTx(ctx, tx, nodeID, &info, &l, *in.SupplierID); err != nil {
			return nil, err
		}
	case "unlink_asset":
		if err = bumpDetachedInfraAssetTx(ctx, tx, nodeID); err != nil {
			return nil, err
		}
		l.AssetID = sql.NullInt64{}
	case "create_asset", "link_asset":
		if l.AssetID.Valid {
			return nil, ErrInfrastructureLinked
		}
		var a InfraAsset
		if in.Action == "create_asset" {
			if in.SupplierID != nil {
				l.SupplierID = sql.NullInt64{Int64: *in.SupplierID, Valid: *in.SupplierID > 0}
			}
			if !l.SupplierID.Valid {
				return nil, ErrAssetInput
			}
			if _, err = supplierChoiceTx(ctx, tx, l.SupplierID.Int64); err != nil {
				return nil, err
			}
			a, err = infoAsset(info)
			if err != nil {
				return nil, err
			}
			a.SupplierID = l.SupplierID.Int64
			a.NodeID = &nodeID
			a.Name = in.Name
			a.RemindDays = in.RemindDays
			if err = a.Validate(); err != nil {
				return nil, err
			}
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM infra_assets`).Scan(&count); err != nil {
				return nil, err
			}
			if count >= 10000 {
				return nil, ErrAssetInput
			}
			res, e := tx.ExecContext(ctx, `INSERT INTO infra_assets(supplier_id,node_id,name,currency,amount_minor,period_months,anchor_day,next_due,remind_days,active) VALUES(?,?,?,?,?,?,?,?,?,1)`, a.SupplierID, nodeID, a.Name, a.Currency, a.AmountMinor, a.PeriodMonths, a.AnchorDay, a.NextDue, a.RemindDays)
			if e != nil {
				return nil, e
			}
			a.ID, _ = res.LastInsertId()
			a.Revision = 1
		} else {
			a, err = scanInfraAsset(tx.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM infra_assets a JOIN infra_suppliers s ON s.id=a.supplier_id WHERE a.id=?`, in.AssetID))
			if err != nil {
				return nil, err
			}
			if a.Revision != in.AssetRevision {
				return nil, ErrInfrastructureConflict
			}
			if !a.Active {
				return nil, ErrAssetInput
			}
			if a.PrimaryNodeID != nil || a.NodeID != nil && *a.NodeID != nodeID {
				return nil, ErrInfrastructureLinked
			}
			if _, err = assetNodeCost(a); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE infra_assets SET node_id=?,revision=revision+1 WHERE id=?`, nodeID, a.ID); err != nil {
				return nil, err
			}
		}
		sup, e := supplierChoiceTx(ctx, tx, a.SupplierID)
		if e != nil {
			return nil, e
		}
		if err = projectAsset(&info, a, sup); err != nil {
			return nil, err
		}
		l.SupplierID = sql.NullInt64{Int64: a.SupplierID, Valid: true}
		l.AssetID = sql.NullInt64{Int64: a.ID, Valid: true}
	default:
		return nil, ErrAssetInput
	}
	if err = saveNodeInfrastructureLinkTx(ctx, tx, nodeID, l.SupplierID, l.AssetID); err != nil {
		return nil, err
	}
	if err = persistNodeInfraInfoTx(ctx, tx, p, info); err != nil {
		return nil, err
	}
	v, _, err = nodeInfrastructureTx(ctx, tx, nodeID, info)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return v, nil
}

func changeNodeSupplierTx(ctx context.Context, tx *sql.Tx, nodeID int64, info *NodeProbeInfo, l *nodeInfrastructureLink, id int64) error {
	if id < 0 {
		return ErrAssetInput
	}
	if id == 0 {
		if l.AssetID.Valid {
			return ErrInfrastructureLinked
		}
		l.SupplierID = sql.NullInt64{}
		return nil // Keep the previous display snapshot.
	}
	sup, err := supplierChoiceTx(ctx, tx, id)
	if err != nil {
		return err
	}
	info.Provider = sup.Name
	info.ProviderURL = sup.URL
	l.SupplierID = sql.NullInt64{Int64: id, Valid: true}
	if l.AssetID.Valid {
		_, err = tx.ExecContext(ctx, `UPDATE infra_assets SET supplier_id=?,revision=revision+1 WHERE id=?`, id, l.AssetID.Int64)
	}
	return err
}

func guardNodeInfrastructureTx(ctx context.Context, tx *sql.Tx, nodeID int64, info *NodeProbeInfo, old NodeProbeInfo, guard NodeInfrastructureGuard) error {
	v, l, err := nodeInfrastructureTx(ctx, tx, nodeID, old)
	if err != nil {
		return err
	}
	providerChanged := info.Provider != old.Provider || info.ProviderURL != old.ProviderURL
	billingChanged := !sameInfraBilling(*info, old)
	change := guard.SupplierID != nil || l.SupplierID.Valid && providerChanged || l.AssetID.Valid && billingChanged
	if change && !guard.CanWrite {
		return ErrInfrastructurePermission
	}
	if change && (guard.Revision == nil || *guard.Revision != v.Revision) || guard.Revision != nil && *guard.Revision != v.Revision {
		return ErrInfrastructureConflict
	}
	if l.SupplierID.Valid && providerChanged && guard.SupplierID == nil {
		return ErrInfrastructureConflict
	}
	if guard.SupplierID != nil {
		if *guard.SupplierID == 0 {
			info.Provider, info.ProviderURL = old.Provider, old.ProviderURL
		}
		if err = changeNodeSupplierTx(ctx, tx, nodeID, info, &l, *guard.SupplierID); err != nil {
			return err
		}
	}
	if l.AssetID.Valid {
		if billingChanged {
			a, e := infoAsset(*info)
			if e != nil {
				return e
			}
			// Normalization anchors an intentional expiry/cycle edit to the new
			// date. Unchanged dates retain the ledger's original billing day.
			if info.ExpiresAt == old.ExpiresAt && sameNodeBillingCycle(info.Billing, old.Billing) {
				a.AnchorDay = v.Asset.AnchorDay
			}
			_, err = tx.ExecContext(ctx, `UPDATE infra_assets SET currency=?,amount_minor=?,period_months=?,next_due=?,anchor_day=?,revision=revision+1 WHERE id=?`, a.Currency, a.AmountMinor, a.PeriodMonths, a.NextDue, a.AnchorDay, l.AssetID.Int64)
			if err != nil {
				return err
			}
			projectAssetRenewal(info, a)
		}
	}
	if change {
		return saveNodeInfrastructureLinkTx(ctx, tx, nodeID, l.SupplierID, l.AssetID)
	}
	return nil
}

func sameInfraBilling(a, b NodeProbeInfo) bool {
	costSame := reflect.DeepEqual(a.Billing, b.Billing)
	if !costSame && a.Billing != nil && b.Billing != nil {
		x, e1 := costMinor(a.Billing)
		y, e2 := costMinor(b.Billing)
		costSame = e1 == nil && e2 == nil && x == y && a.Billing.Currency == b.Billing.Currency && sameNodeBillingCycle(a.Billing, b.Billing)
	}
	priceSame := a.Price == b.Price || costSame && a.Billing != nil
	return costSame && priceSame && a.ExpiresAt == b.ExpiresAt && reflect.DeepEqual(a.AutoRenewal, b.AutoRenewal)
}

// Synchronize only explicitly selected primary assets, never loose node_id.
func syncInfraAssetNodeTx(ctx context.Context, tx *sql.Tx, a InfraAsset, permission []bool) error {
	var nodeID int64
	err := tx.QueryRowContext(ctx, `SELECT node_id FROM node_infrastructure WHERE asset_id=?`, a.ID).Scan(&nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if a.NodeID == nil || *a.NodeID != nodeID {
		return ErrInfrastructureLinked
	}
	p, err := nodeInfraProbeTx(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	sup, err := supplierChoiceTx(ctx, tx, a.SupplierID)
	if err != nil {
		return err
	}
	info := p.Info
	if err = projectAsset(&info, a, sup); err != nil {
		return err
	}
	// An old settings-scoped editor may still change an asset's independent
	// notes/name. Only a write to shared values needs the additional scope.
	l, err := nodeInfrastructureLinkTx(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	if l.SupplierID.Int64 == a.SupplierID && sameInfraBilling(info, p.Info) && info.Provider == p.Info.Provider && info.ProviderURL == p.Info.ProviderURL {
		return nil
	}
	if !sharedInfrastructureAllowed(permission) {
		return ErrInfrastructurePermission
	}
	if err = persistNodeInfraInfoTx(ctx, tx, p, info); err != nil {
		return err
	}
	return saveNodeInfrastructureLinkTx(ctx, tx, nodeID, sql.NullInt64{Int64: a.SupplierID, Valid: true}, sql.NullInt64{Int64: a.ID, Valid: true})
}

func syncInfraSupplierNodesTx(ctx context.Context, tx *sql.Tx, supplier InfraSupplier, permission []bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT node_id FROM node_infrastructure WHERE supplier_id=?`, supplier.ID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		p, e := nodeInfraProbeTx(ctx, tx, id)
		if e != nil {
			return e
		}
		if p.Info.Provider == supplier.Name && p.Info.ProviderURL == supplier.URL {
			continue
		}
		if !sharedInfrastructureAllowed(permission) {
			return ErrInfrastructurePermission
		}
		info := p.Info
		info.Provider = supplier.Name
		info.ProviderURL = supplier.URL
		if e = persistNodeInfraInfoTx(ctx, tx, p, info); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE node_infrastructure SET revision=revision+1 WHERE node_id=?`, id); e != nil {
			return e
		}
	}
	return nil
}

// Accepted online samples move the same due date as the server. This is a
// scheduling update only: never insert a payment or reactivate an archived bill.
func renewNodeInfrastructureTx(ctx context.Context, tx *sql.Tx, nodeID int64, info NodeProbeInfo) error {
	l, err := nodeInfrastructureLinkTx(ctx, tx, nodeID)
	if err != nil || !l.AssetID.Valid {
		return err
	}
	a, err := infoAsset(info)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE infra_assets SET next_due=?,anchor_day=?,revision=revision+1 WHERE id=?`, a.NextDue, a.AnchorDay, l.AssetID.Int64); err != nil {
		return err
	}
	return saveNodeInfrastructureLinkTx(ctx, tx, nodeID, l.SupplierID, l.AssetID)
}

// Removing a primary association changes the legacy empty payment action from
// record-only to period advancement when online renewal was enabled. Invalidate
// open payment drafts even though the retained asset's due date is unchanged.
func bumpDetachedInfraAssetTx(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE infra_assets SET revision=revision+1 WHERE id IN (SELECT asset_id FROM node_infrastructure WHERE node_id=?)`, nodeID)
	return err
}
