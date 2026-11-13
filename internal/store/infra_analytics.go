package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

type InfraPrice struct {
	ID           int64  `json:"id"`
	AssetID      int64  `json:"asset_id"`
	EffectiveAt  int64  `json:"effective_at"`
	Currency     string `json:"currency"`
	AmountMinor  int64  `json:"amount_minor"`
	PeriodMonths int    `json:"period_months"`
	NextDue      string `json:"next_due"`
	AnchorDay    int    `json:"anchor_day"`
	Active       bool   `json:"active"`
}
type InfraExpense struct {
	ID          int64  `json:"id"`
	AssetID     int64  `json:"asset_id"`
	RequestKey  string `json:"request_key"`
	Currency    string `json:"currency"`
	AmountMinor int64  `json:"amount_minor"`
	PaidDate    string `json:"paid_date"`
	Category    string `json:"category"`
	Note        string `json:"note"`
	ReversalOf  *int64 `json:"reversal_of"`
	Voided      bool   `json:"voided"`
}
type InfraForecast struct {
	Currency       string  `json:"currency"`
	MonthAccrued   float64 `json:"month_accrued_minor"`
	MonthlyBudget  float64 `json:"monthly_budget_minor"`
	AnnualBudget   float64 `json:"annual_budget_minor"`
	RemainingValue float64 `json:"remaining_value_minor"`
	Paid           int64   `json:"paid_minor"`
	Extras         int64   `json:"extras_minor"`
	CoverageSince  int64   `json:"coverage_since"`
}
type InfraAnalytics struct {
	Month     string          `json:"month"`
	AsOf      int64           `json:"as_of"`
	Totals    []InfraForecast `json:"totals"`
	Prices    []InfraPrice    `json:"prices"`
	Expenses  []InfraExpense  `json:"expenses"`
	Truncated bool            `json:"truncated"`
}

func (s *Store) InfraAnalytics(ctx context.Context, month string, assetID int64, at time.Time) (InfraAnalytics, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start, e := time.Parse("2006-01", month)
	if e != nil || len(month) != 7 || start.Year() < 1970 || start.Year() > 9900 {
		return InfraAnalytics{}, ErrAssetInput
	}
	end := start.AddDate(0, 1, 0)
	cut := at.UTC()
	if end.Before(cut) {
		cut = end
	}
	out := InfraAnalytics{Month: month, AsOf: at.Unix(), Totals: []InfraForecast{}, Prices: []InfraPrice{}, Expenses: []InfraExpense{}}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	totals := map[string]*InfraForecast{}
	total := func(c string) *InfraForecast {
		v := totals[c]
		if v == nil {
			v = &InfraForecast{Currency: c}
			totals[c] = v
		}
		return v
	}
	// Stream all relevant snapshots for correct totals, while bounding displayed history.
	q := `SELECT id,asset_id,effective_at,currency,amount_minor,period_months,next_due,anchor_day,active FROM infra_price_history WHERE (?=0 OR asset_id=?) ORDER BY asset_id,effective_at,id`
	rows, e := tx.QueryContext(ctx, q, assetID, assetID)
	if e != nil {
		return out, e
	}
	prices := []InfraPrice{}
	for rows.Next() {
		var v InfraPrice
		if e = rows.Scan(&v.ID, &v.AssetID, &v.EffectiveAt, &v.Currency, &v.AmountMinor, &v.PeriodMonths, &v.NextDue, &v.AnchorDay, &v.Active); e != nil {
			rows.Close()
			return out, e
		}
		prices = append(prices, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for i, v := range prices {
		t := total(v.Currency)
		if t.CoverageSince == 0 || v.EffectiveAt < t.CoverageSince {
			t.CoverageSince = v.EffectiveAt
		}
		until := at.UTC()
		latest := i+1 == len(prices) || prices[i+1].AssetID != v.AssetID
		if !latest {
			until = time.Unix(prices[i+1].EffectiveAt, 0)
		}
		if v.Active && v.PeriodMonths > 0 {
			rate := float64(v.AmountMinor) / float64(v.PeriodMonths)
			if latest {
				t.MonthlyBudget += rate
				t.AnnualBudget += rate * 12
				due, _ := infraDate(v.NextDue)
				prev := infraPreviousDue(v)
				if due.After(at) && due.After(prev) {
					t.RemainingValue += float64(v.AmountMinor) * due.Sub(maxTime(at, prev)).Seconds() / due.Sub(prev).Seconds()
				}
			}
			from := maxTime(start, time.Unix(v.EffectiveAt, 0))
			to := minTime(cut, until)
			// Monthly normalized operating-cost estimate, split at calendar boundaries.
			if to.After(from) {
				t.MonthAccrued += rate * to.Sub(from).Seconds() / end.Sub(start).Seconds()
			}
		}
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i].ID > prices[j].ID })
	if len(prices) > 100 {
		out.Truncated = true
		prices = prices[:100]
	}
	out.Prices = prices
	rows, e = tx.QueryContext(ctx, `SELECT currency,SUM(amount_minor) FROM infra_payments WHERE paid_date>=? AND paid_date<? AND (?=0 OR asset_id=?) GROUP BY currency`, start.Format("2006-01-02"), end.Format("2006-01-02"), assetID, assetID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var c string
		var n int64
		if e = rows.Scan(&c, &n); e != nil {
			rows.Close()
			return out, e
		}
		total(c).Paid = n
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT currency,SUM(amount_minor) FROM infra_extra_expenses WHERE paid_date>=? AND paid_date<? AND (?=0 OR asset_id=?) GROUP BY currency`, start.Format("2006-01-02"), end.Format("2006-01-02"), assetID, assetID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var c string
		var n int64
		if e = rows.Scan(&c, &n); e != nil {
			rows.Close()
			return out, e
		}
		total(c).Extras = n
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT e.id,e.asset_id,e.request_key,e.currency,e.amount_minor,e.paid_date,e.category,e.note,e.reversal_of,EXISTS(SELECT 1 FROM infra_extra_expenses r WHERE r.reversal_of=e.id) FROM infra_extra_expenses e WHERE (?=0 OR e.asset_id=?) ORDER BY e.id DESC LIMIT 101`, assetID, assetID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v InfraExpense
		if e = rows.Scan(&v.ID, &v.AssetID, &v.RequestKey, &v.Currency, &v.AmountMinor, &v.PaidDate, &v.Category, &v.Note, &v.ReversalOf, &v.Voided); e != nil {
			rows.Close()
			return out, e
		}
		out.Expenses = append(out.Expenses, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(out.Expenses) > 100 {
		out.Expenses = out.Expenses[:100]
		out.Truncated = true
	}
	for _, v := range totals {
		out.Totals = append(out.Totals, *v)
	}
	sort.Slice(out.Totals, func(i, j int) bool { return out.Totals[i].Currency < out.Totals[j].Currency })
	return out, tx.Commit()
}
func infraPreviousDue(v InfraPrice) time.Time {
	due, _ := infraDate(v.NextDue)
	first := time.Date(due.Year(), due.Month()-time.Month(v.PeriodMonths), 1, 0, 0, 0, 0, time.UTC)
	day := min(v.AnchorDay, first.AddDate(0, 1, -1).Day())
	return first.AddDate(0, 0, day-1)
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func (s *Store) AddInfraExpense(ctx context.Context, v *InfraExpense) error {
	if v.AssetID < 1 || !infraRequestKey.MatchString(v.RequestKey) || !infraCurrency.MatchString(v.Currency) || v.AmountMinor < 0 || v.AmountMinor > 1e12 || !infraText(v.Note, 500) || !slicesExpense(v.Category) || v.ReversalOf != nil {
		return ErrAssetInput
	}
	if _, e := infraDate(v.PaidDate); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var old InfraExpense
	e = tx.QueryRowContext(ctx, "SELECT id,asset_id,currency,amount_minor,paid_date,category,note FROM infra_extra_expenses WHERE request_key=?", v.RequestKey).Scan(&old.ID, &old.AssetID, &old.Currency, &old.AmountMinor, &old.PaidDate, &old.Category, &old.Note)
	if e == nil {
		if old.AssetID != v.AssetID || old.Currency != v.Currency || old.AmountMinor != v.AmountMinor || old.PaidDate != v.PaidDate || old.Category != v.Category || old.Note != v.Note {
			return ErrAssetConflict
		}
		v.ID = old.ID
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var asset int64
	if e = tx.QueryRowContext(ctx, "SELECT id FROM infra_assets WHERE id=?", v.AssetID).Scan(&asset); e != nil {
		return wrapNotFound(e)
	}
	r, e := tx.ExecContext(ctx, `INSERT INTO infra_extra_expenses(asset_id,request_key,currency,amount_minor,paid_date,category,note,created_at) VALUES(?,?,?,?,?,?,?,?)`, v.AssetID, v.RequestKey, v.Currency, v.AmountMinor, v.PaidDate, v.Category, v.Note, now())
	if e != nil {
		return e
	}
	v.ID, _ = r.LastInsertId()
	return tx.Commit()
}
func slicesExpense(s string) bool { return s == "ip" || s == "traffic" || s == "other" }
func (s *Store) ReverseInfraExpense(ctx context.Context, id int64, reason string, at time.Time) error {
	if !infraText(reason, 500) || reason == "" {
		return ErrAssetInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var v InfraExpense
	var reversal sql.NullInt64
	e = tx.QueryRowContext(ctx, "SELECT asset_id,currency,amount_minor,reversal_of FROM infra_extra_expenses WHERE id=?", id).Scan(&v.AssetID, &v.Currency, &v.AmountMinor, &reversal)
	if e != nil {
		return wrapNotFound(e)
	}
	if reversal.Valid {
		return ErrAssetInput
	}
	var count int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM infra_extra_expenses WHERE reversal_of=?", id).Scan(&count); e != nil {
		return e
	}
	if count > 0 {
		return nil
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO infra_extra_expenses(asset_id,request_key,currency,amount_minor,paid_date,category,note,created_at,reversal_of) VALUES(?,?,?,?,?,'reversal',?,?,?)`, v.AssetID, "reversal-"+time.Unix(id, 0).Format("20060102150405"), v.Currency, -v.AmountMinor, at.UTC().Format("2006-01-02"), reason, at.Unix(), id)
	if e != nil {
		return e
	}
	return tx.Commit()
}
