package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

var ErrAssetInput = errors.New("invalid infrastructure asset")
var ErrAssetConflict = errors.New("asset changed or payment request key was reused")
var ErrAssetHistory = errors.New("record has assets or payment history; archive the asset instead")

type InfraSupplier struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Contact string `json:"contact"`
	Notes   string `json:"notes"`
}
type InfraAsset struct {
	ID                 int64  `json:"id"`
	SupplierID         int64  `json:"supplier_id"`
	SupplierName       string `json:"supplier_name"`
	NodeID             *int64 `json:"node_id"`
	PrimaryNodeID      *int64 `json:"primary_node_id"`
	AutoRenewalEnabled bool   `json:"auto_renewal_enabled"`
	Name               string `json:"name"`
	ExternalRef        string `json:"external_ref"`
	Currency           string `json:"currency"`
	AmountMinor        int64  `json:"amount_minor"`
	PeriodMonths       int    `json:"period_months"`
	AnchorDay          int    `json:"anchor_day"`
	NextDue            string `json:"next_due"`
	RemindDays         int    `json:"remind_days"`
	Active             bool   `json:"active"`
	Notes              string `json:"notes"`
	Revision           int64  `json:"revision"`
}
type InfraPayment struct {
	ID           int64  `json:"id"`
	AssetID      int64  `json:"asset_id"`
	StaffID      *int64 `json:"staff_id"`
	SupplierName string `json:"supplier_name"`
	AssetName    string `json:"asset_name"`
	Currency     string `json:"currency"`
	AmountMinor  int64  `json:"amount_minor"`
	PaidDate     string `json:"paid_date"`
	PeriodStart  string `json:"period_start"`
	NextDue      string `json:"next_due"`
	Reference    string `json:"reference"`
	Notes        string `json:"notes"`
	DateAction   string `json:"date_action"`
}
type InfraPaymentInput struct {
	RequestKey  string `json:"request_key"`
	Revision    int64  `json:"revision"`
	AmountMinor int64  `json:"amount_minor"`
	PaidDate    string `json:"paid_date"`
	Reference   string `json:"reference"`
	Notes       string `json:"notes"`
	// Keep optional additions at the end: an old empty request must marshal to
	// exactly the original bytes for its persisted idempotency hash.
	DateAction string `json:"date_action,omitempty"`
	NextDue    string `json:"next_due,omitempty"`
}
type InfraCost struct {
	Currency    string `json:"currency"`
	AmountMinor int64  `json:"amount_minor"`
}

func infraText(s string, n int) bool { return len(s) <= n && spec.Plain(s) }
func infraDate(s string) (time.Time, error) {
	t, e := time.Parse("2006-01-02", s)
	if e != nil || len(s) != 10 || t.Year() < 1970 || t.Year() > 9900 {
		return time.Time{}, ErrAssetInput
	}
	return t, nil
}
func (v InfraSupplier) Validate() error {
	if strings.TrimSpace(v.Name) == "" || !infraText(v.Name, 100) || !infraText(v.Contact, 500) || len(v.Notes) > 4000 || !infraText(v.URL, 2000) {
		return ErrAssetInput
	}
	if v.URL != "" {
		u, e := url.Parse(v.URL)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return ErrAssetInput
		}
	}
	return nil
}

var infraCurrency = regexp.MustCompile(`^[A-Z]{3}$`)
var infraRequestKey = regexp.MustCompile(`^[A-Za-z0-9_-]{16,80}$`)

func (v InfraAsset) Validate() error {
	if v.SupplierID < 1 || strings.TrimSpace(v.Name) == "" || !infraText(v.Name, 150) || !infraText(v.ExternalRef, 200) || !infraCurrency.MatchString(v.Currency) || v.AmountMinor < 0 || v.AmountMinor > 1_000_000_000_000 || v.PeriodMonths < 0 || v.PeriodMonths > 120 || v.AnchorDay < 1 || v.AnchorDay > 31 || v.RemindDays < 0 || v.RemindDays > 90 || len(v.Notes) > 4000 || v.NodeID != nil && *v.NodeID < 1 {
		return ErrAssetInput
	}
	_, e := infraDate(v.NextDue)
	return e
}
func (s *Store) InfraSuppliers(ctx context.Context) ([]InfraSupplier, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,name,url,contact,notes FROM infra_suppliers ORDER BY name,id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []InfraSupplier{}
	for rows.Next() {
		var v InfraSupplier
		if e := rows.Scan(&v.ID, &v.Name, &v.URL, &v.Contact, &v.Notes); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SaveInfraSupplier(ctx context.Context, v *InfraSupplier, sharedWrite ...bool) error {
	if e := v.Validate(); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if v.ID == 0 {
		res, e := tx.ExecContext(ctx, `INSERT INTO infra_suppliers(name,url,contact,notes) VALUES(?,?,?,?)`, v.Name, v.URL, v.Contact, v.Notes)
		if e != nil {
			return e
		}
		v.ID, _ = res.LastInsertId()
		return tx.Commit()
	}
	res, e := tx.ExecContext(ctx, `UPDATE infra_suppliers SET name=?,url=?,contact=?,notes=? WHERE id=?`, v.Name, v.URL, v.Contact, v.Notes, v.ID)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if e = syncInfraSupplierNodesTx(ctx, tx, *v, sharedWrite); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) DeleteInfraSupplier(ctx context.Context, id int64) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM infra_assets WHERE supplier_id=?)+(SELECT COUNT(*) FROM node_infrastructure WHERE supplier_id=?)`, id, id).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return ErrAssetHistory
	}
	res, e := tx.ExecContext(ctx, `DELETE FROM infra_suppliers WHERE id=?`, id)
	if e != nil {
		return e
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

const assetColumns = `a.id,a.supplier_id,s.name,a.node_id,a.name,a.external_ref,a.currency,a.amount_minor,a.period_months,a.anchor_day,a.next_due,a.remind_days,a.active,a.notes,a.revision,(SELECT ni.node_id FROM node_infrastructure ni WHERE ni.asset_id=a.id),COALESCE((SELECT n.probe_info_json FROM node_infrastructure ni JOIN nodes n ON n.id=ni.node_id WHERE ni.asset_id=a.id),'{}')`

func scanInfraAsset(row interface{ Scan(...any) error }) (InfraAsset, error) {
	var v InfraAsset
	var raw string
	e := row.Scan(&v.ID, &v.SupplierID, &v.SupplierName, &v.NodeID, &v.Name, &v.ExternalRef, &v.Currency, &v.AmountMinor, &v.PeriodMonths, &v.AnchorDay, &v.NextDue, &v.RemindDays, &v.Active, &v.Notes, &v.Revision, &v.PrimaryNodeID, &raw)
	if e != nil {
		return v, wrapNotFound(e)
	}
	var info struct {
		AutoRenewal *NodeAutoRenewal `json:"auto_renewal"`
	}
	if e = json.Unmarshal([]byte(raw), &info); e != nil {
		return v, e
	}
	v.AutoRenewalEnabled = info.AutoRenewal != nil && info.AutoRenewal.Enabled
	return v, nil
}
func (s *Store) InfraAssets(ctx context.Context) ([]InfraAsset, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+assetColumns+` FROM infra_assets a JOIN infra_suppliers s ON s.id=a.supplier_id ORDER BY a.active DESC,a.next_due,a.id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []InfraAsset{}
	for rows.Next() {
		v, e := scanInfraAsset(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// sharedWrite carries the infrastructure grant and, optionally, the separate
// service-control grant required to move a protected traffic reset boundary.
func (s *Store) SaveInfraAsset(ctx context.Context, v *InfraAsset, sharedWrite ...bool) error {
	if e := v.Validate(); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	v.PrimaryNodeID = nil // Read-only relation, not an alternate linking API.
	if v.ID > 0 {
		var linked int64
		e = tx.QueryRowContext(ctx, `SELECT node_id FROM node_infrastructure WHERE asset_id=?`, v.ID).Scan(&linked)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			v.PrimaryNodeID = &linked
			if v.NodeID == nil {
				v.NodeID = &linked
			} // Omitted old field preserves the primary relation.
			if *v.NodeID != linked {
				return ErrInfrastructureLinked
			}
		}
	}
	var name string
	if e = tx.QueryRowContext(ctx, `SELECT name FROM infra_suppliers WHERE id=?`, v.SupplierID).Scan(&name); e != nil {
		return wrapNotFound(e)
	}
	v.SupplierName = name
	if v.NodeID != nil {
		var id int64
		if e = tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE id=?`, v.NodeID).Scan(&id); e != nil {
			return wrapNotFound(e)
		}
	}
	args := []any{v.SupplierID, v.NodeID, v.Name, v.ExternalRef, v.Currency, v.AmountMinor, v.PeriodMonths, v.AnchorDay, v.NextDue, v.RemindDays, v.Active, v.Notes}
	if v.ID == 0 {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM infra_assets`).Scan(&n); e != nil {
			return e
		}
		if n >= 10000 {
			return ErrAssetInput
		}
		res, e := tx.ExecContext(ctx, `INSERT INTO infra_assets(supplier_id,node_id,name,external_ref,currency,amount_minor,period_months,anchor_day,next_due,remind_days,active,notes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
		if e != nil {
			return e
		}
		v.ID, _ = res.LastInsertId()
		v.Revision = 1
	} else {
		args = append(args, v.ID, v.Revision)
		res, e := tx.ExecContext(ctx, `UPDATE infra_assets SET supplier_id=?,node_id=?,name=?,external_ref=?,currency=?,amount_minor=?,period_months=?,anchor_day=?,next_due=?,remind_days=?,active=?,notes=?,revision=revision+1 WHERE id=? AND revision=?`, args...)
		if e != nil {
			return e
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrAssetConflict
		}
		v.Revision++
	}
	if e = syncInfraAssetNodeTx(ctx, tx, *v, sharedWrite); e != nil {
		return e
	}
	stored, e := scanInfraAsset(tx.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM infra_assets a JOIN infra_suppliers s ON s.id=a.supplier_id WHERE a.id=?`, v.ID))
	if e != nil {
		return e
	}
	*v = stored
	return tx.Commit()
}
func (s *Store) DeleteInfraAsset(ctx context.Context, id int64, revision int64) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_infrastructure WHERE asset_id=?`, id).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return ErrInfrastructureLinked
	}
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM infra_payments WHERE asset_id=?`, id).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return ErrAssetHistory
	}
	res, e := tx.ExecContext(ctx, `DELETE FROM infra_assets WHERE id=? AND revision=?`, id, revision)
	if e != nil {
		return e
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		return ErrAssetConflict
	}
	return tx.Commit()
}

// NextInfraDue keeps the original day across short months (Jan 31 -> Feb 28
// -> Mar 31), rather than letting time.AddDate skip into March.
func NextInfraDue(due string, months, anchor int) (string, error) {
	t, e := infraDate(due)
	if e != nil || months < 1 || months > 120 || anchor < 1 || anchor > 31 {
		return "", ErrAssetInput
	}
	first := time.Date(t.Year(), t.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if anchor > last {
		anchor = last
	}
	out := time.Date(first.Year(), first.Month(), anchor, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	_, e = infraDate(out)
	return out, e
}

const paymentColumns = `id,asset_id,staff_id,supplier_name,asset_name,currency,amount_minor,paid_date,period_start,next_due,reference,notes,date_action`

func scanInfraPayment(row interface{ Scan(...any) error }) (InfraPayment, error) {
	var v InfraPayment
	e := row.Scan(&v.ID, &v.AssetID, &v.StaffID, &v.SupplierName, &v.AssetName, &v.Currency, &v.AmountMinor, &v.PaidDate, &v.PeriodStart, &v.NextDue, &v.Reference, &v.Notes, &v.DateAction)
	return v, wrapNotFound(e)
}

// sharedWrite uses the same infrastructure/control grants as SaveInfraAsset.
func (s *Store) RecordInfraPayment(ctx context.Context, assetID, staffID int64, in InfraPaymentInput, sharedWrite ...bool) (InfraPayment, error) {
	empty := InfraPayment{}
	if !infraRequestKey.MatchString(in.RequestKey) || in.Revision < 1 || in.AmountMinor < 0 || in.AmountMinor > 1_000_000_000_000 || !infraText(in.Reference, 200) || len(in.Notes) > 4000 {
		return empty, ErrAssetInput
	}
	if _, e := infraDate(in.PaidDate); e != nil {
		return empty, e
	}
	switch in.DateAction {
	case "", "record_only":
		if in.NextDue != "" {
			return empty, ErrAssetInput
		}
	case "set_due_date":
		if _, e := infraDate(in.NextDue); e != nil {
			return empty, e
		}
	default:
		return empty, ErrAssetInput
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	var oldID int64
	var oldHash string
	e = tx.QueryRowContext(ctx, `SELECT id,request_hash FROM infra_payments WHERE asset_id=? AND request_key=?`, assetID, in.RequestKey).Scan(&oldID, &oldHash)
	if e == nil {
		if hash != oldHash {
			return empty, ErrAssetConflict
		}
		return scanInfraPayment(tx.QueryRowContext(ctx, `SELECT `+paymentColumns+` FROM infra_payments WHERE id=?`, oldID))
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return empty, e
	}
	a, e := scanInfraAsset(tx.QueryRowContext(ctx, `SELECT `+assetColumns+` FROM infra_assets a JOIN infra_suppliers s ON s.id=a.supplier_id WHERE a.id=?`, assetID))
	if e != nil {
		return empty, e
	}
	if !a.Active || a.Revision != in.Revision {
		return empty, ErrAssetConflict
	}
	action := in.DateAction
	if action == "" {
		action = "advance_period"
		if a.AutoRenewalEnabled {
			action = "record_only"
		}
	}
	if action != "record_only" && a.PrimaryNodeID != nil && !sharedInfrastructureAllowed(sharedWrite) {
		return empty, ErrInfrastructurePermission
	}
	next, active, anchor := a.NextDue, a.Active, a.AnchorDay
	switch action {
	case "advance_period":
		active = false
		if a.PeriodMonths > 0 {
			next, e = NextInfraDue(a.NextDue, a.PeriodMonths, a.AnchorDay)
			if e != nil {
				return empty, e
			}
			active = true
		}
	case "set_due_date":
		if in.NextDue < a.NextDue {
			return empty, ErrAssetInput
		}
		next = in.NextDue
		anchor = paymentDateAnchor(a, next)
	}
	v := InfraPayment{AssetID: a.ID, StaffID: &staffID, SupplierName: a.SupplierName, AssetName: a.Name, Currency: a.Currency, AmountMinor: in.AmountMinor, PaidDate: in.PaidDate, PeriodStart: a.NextDue, NextDue: next, Reference: in.Reference, Notes: in.Notes, DateAction: action}
	if action == "record_only" {
		v.PeriodStart, v.NextDue = "", ""
	}
	res, e := tx.ExecContext(ctx, `INSERT INTO infra_payments(asset_id,staff_id,request_key,request_hash,supplier_name,asset_name,currency,amount_minor,paid_date,period_start,next_due,reference,notes,created_at,date_action) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, assetID, staffID, in.RequestKey, hash, v.SupplierName, v.AssetName, v.Currency, v.AmountMinor, v.PaidDate, v.PeriodStart, v.NextDue, v.Reference, v.Notes, now(), action)
	if e != nil {
		return empty, e
	}
	v.ID, _ = res.LastInsertId()
	if _, e = tx.ExecContext(ctx, `UPDATE infra_assets SET next_due=?,active=?,anchor_day=?,revision=revision+1 WHERE id=?`, next, active, anchor, a.ID); e != nil {
		return empty, e
	}
	a.NextDue, a.Active, a.AnchorDay = next, active, anchor
	if action != "record_only" {
		if e = syncInfraAssetNodeTx(ctx, tx, a, sharedWrite); e != nil {
			return empty, e
		}
	}
	if e = tx.Commit(); e != nil {
		return empty, e
	}
	return v, nil
}

// A selected date on the existing billing cadence retains the unclamped day.
// An off-cadence manual date deliberately starts a new calendar anchor.
func paymentDateAnchor(a InfraAsset, target string) int {
	if target == a.NextDue {
		return a.AnchorDay
	}
	date, _ := infraDate(target)
	due, _ := infraDate(a.NextDue)
	months := (date.Year()-due.Year())*12 + int(date.Month()-due.Month())
	if a.PeriodMonths > 0 && months >= 0 && months%a.PeriodMonths == 0 && nodeExpiryAt(due, months, a.AnchorDay).Equal(date) {
		return a.AnchorDay
	}
	return date.Day()
}
func (s *Store) InfraPayments(ctx context.Context, assetID int64, offset int) ([]InfraPayment, int, error) {
	if offset < 0 || offset > 1000000 {
		return nil, 0, ErrAssetInput
	}
	where := ""
	args := []any{}
	if assetID > 0 {
		where = " WHERE asset_id=?"
		args = append(args, assetID)
	}
	var total int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM infra_payments`+where, args...).Scan(&total); e != nil {
		return nil, 0, e
	}
	args = append(args, offset)
	rows, e := s.db.QueryContext(ctx, `SELECT `+paymentColumns+` FROM infra_payments`+where+` ORDER BY paid_date DESC,id DESC LIMIT 100 OFFSET ?`, args...)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []InfraPayment{}
	for rows.Next() {
		v, e := scanInfraPayment(rows)
		if e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
func (s *Store) InfraCosts(ctx context.Context) ([]InfraCost, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT currency,SUM(amount_minor) FROM infra_payments GROUP BY currency ORDER BY currency`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []InfraCost{}
	for rows.Next() {
		var v InfraCost
		if e := rows.Scan(&v.Currency, &v.AmountMinor); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ClaimInfraReminder(ctx context.Context, a InfraAsset, at time.Time) (bool, error) {
	due, e := infraDate(a.NextDue)
	if e != nil {
		return false, e
	}
	today := at.UTC().Format("2006-01-02")
	start := due.AddDate(0, 0, -a.RemindDays).Format("2006-01-02")
	if !a.Active || today < start {
		return false, nil
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	var revision int64
	var active bool
	if e = tx.QueryRowContext(ctx, `SELECT revision,active FROM infra_assets WHERE id=?`, a.ID).Scan(&revision, &active); e != nil {
		return false, e
	}
	if !active || revision != a.Revision {
		return false, nil
	}
	res, e := tx.ExecContext(ctx, `INSERT INTO infra_reminders(asset_id,due_date,notice_date,lease_until) VALUES(?,?,?,?) ON CONFLICT(asset_id,due_date,notice_date) DO UPDATE SET lease_until=excluded.lease_until WHERE infra_reminders.sent_at IS NULL AND infra_reminders.lease_until<=?`, a.ID, a.NextDue, today, at.Add(5*time.Minute).Unix(), at.Unix())
	if e != nil {
		return false, e
	}
	n, _ := res.RowsAffected()
	return n > 0, tx.Commit()
}
func (s *Store) FinishInfraReminder(ctx context.Context, a InfraAsset, at time.Time, sent bool) error {
	var mark any
	if sent {
		mark = at.Unix()
	}
	_, e := s.db.ExecContext(ctx, `UPDATE infra_reminders SET sent_at=?,lease_until=? WHERE asset_id=? AND due_date=? AND notice_date=?`, mark, at.Unix(), a.ID, a.NextDue, at.UTC().Format("2006-01-02"))
	return e
}
func (s *Store) PruneInfraReminders(ctx context.Context, at time.Time) error {
	_, e := s.db.ExecContext(ctx, `DELETE FROM infra_reminders WHERE notice_date<?`, at.UTC().AddDate(0, 0, -90).Format("2006-01-02"))
	return e
}
