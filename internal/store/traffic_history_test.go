package store

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
)

func trafficSampleFixture(at time.Time) *agentproto.TrafficSample {
	start := at.Add(-time.Minute)
	duration := int64(60000)
	return &agentproto.TrafficSample{Version: 1, CollectionStartedAt: at, CollectionEndedAt: at, Segments: 1, Quality: "observed", Reasons: []string{}, Sources: []agentproto.TrafficSampleSource{{Core: "xray", Counter: "users", StartedAt: &start, EndedAt: at, DurationMS: &duration, Quality: "observed", Reasons: []string{}, Segments: 1}}}
}

func TestTrafficHistoryKeepsAccountingDayAndAtomicReceipt(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 31, 0, 1, 0, 0, time.UTC)
	u := &domain.User{Email: "history@example.com", UUID: "history-user", Status: "active"}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	ib := proxyTrafficInbound(t, s, node, "history")
	if _, err := s.db.Exec(`INSERT INTO plans(id,name,price_cents,period_days,quota_bytes,created_at,updated_at) VALUES(1,'previous',0,0,0,1,1),(2,'current',0,0,0,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO subscriptions(id,user_id,plan_id,starts_at,expires_at,quota_bytes,created_at,updated_at) VALUES(1,?,1,1,?,0,1,1),(2,?,2,1,?,0,1,1)`, u.ID, at.Add(-time.Hour).Unix(), u.ID, at.Add(24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	observation := TrafficObservation{Sample: trafficSampleFixture(at.Add(-24 * time.Hour)), DiscardedSamples: 2}
	epoch := strings.Repeat("a", 32)
	seq := uint64(1_800_000_000_000_000_001)
	samples := []TrafficSample{{UserID: u.ID, InboundID: ib.ID, Up: 300, Down: 700}}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_history BEFORE INSERT ON node_traffic_history BEGIN SELECT RAISE(FAIL,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AcceptNodeTraffic(ctx, node, epoch, seq, samples, nil, nil, at, observation); err == nil {
		t.Fatal("history failure acknowledged accounting")
	}
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM node_traffic_receipts`).Scan(&count)
	if count != 0 {
		t.Fatal("receipt survived rollback")
	}
	s.db.Exec(`DROP TRIGGER reject_history`)
	for i := range 2 {
		_, seen, err := s.AcceptNodeTraffic(ctx, node, epoch, seq, samples, nil, nil, at.Add(time.Duration(i)*time.Hour), observation)
		if err != nil || seen != (i == 1) {
			t.Fatal(seen, err)
		}
	}
	page, err := s.NodeTrafficHistory(ctx, node, 0, 50, at.Add(time.Hour))
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	item := page.Items[0]
	if item.AccountingDay != "2026-10-31" || !item.ReceivedAt.Equal(at) || item.Sequence != "1800000000000000001" || item.UpBytes != 300 || item.DownBytes != 700 || !item.TotalsKnown || item.DiscardedSamples != 2 || item.Sample == nil || !item.Sample.CollectionEndedAt.Equal(observation.Sample.CollectionEndedAt) {
		t.Fatal(item)
	}
	var day, up, down int64
	if err := s.db.QueryRow(`SELECT day,up_bytes,down_bytes FROM traffic_daily WHERE user_id=?`, u.ID).Scan(&day, &up, &down); err != nil || day != at.Truncate(24*time.Hour).Unix() || up != 300 || down != 700 {
		t.Fatal(day, up, down, err)
	}
	var previousUp, currentUp, currentDown int64
	if err := s.db.QueryRow(`SELECT used_up_bytes FROM subscriptions WHERE id=1`).Scan(&previousUp); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT used_up_bytes,used_down_bytes FROM subscriptions WHERE id=2`).Scan(&currentUp, &currentDown); err != nil || previousUp != 0 || currentUp != 300 || currentDown != 700 {
		t.Fatal("metadata changed package selection", previousUp, currentUp, currentDown, err)
	}
	var first int64
	if err := s.db.QueryRow(`SELECT first_connected_at FROM users WHERE id=?`, u.ID).Scan(&first); err != nil || first != at.Unix() {
		t.Fatal("metadata changed first connection", first, err)
	}
	// Invalid provenance neither overrides the first record nor blocks a new
	// valid traffic batch; no raw invalid content is retained.
	observation.Sample.Version = 0
	if _, _, err := s.AcceptNodeTraffic(ctx, node, epoch, seq+1, samples, nil, nil, at, observation); err != nil {
		t.Fatal(err)
	}
	page, _ = s.NodeTrafficHistory(ctx, node, 0, 1, at)
	if page.NextBefore == 0 || page.Items[0].MetadataStatus != "invalid" || page.Items[0].Sample != nil {
		t.Fatal(page)
	}
	older, _ := s.NodeTrafficHistory(ctx, node, page.NextBefore, 1, at)
	if len(older.Items) != 1 || older.Items[0].ID == page.Items[0].ID {
		t.Fatal("unstable cursor", older)
	}
}

func TestTrafficHistoryDeliveryFreshnessIdentityAndRetention(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Millisecond)
	s.db.Exec(`UPDATE nodes SET token_hash='identity-one' WHERE id=?`, node)
	d := &agentproto.TrafficDelivery{SampledAt: at, PendingBytes: 12, JournalBytes: 500}
	write := func(now time.Time) {
		t.Helper()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := recordTrafficDeliveryTx(ctx, tx, node, "identity-one", d, now); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	write(at)
	d.SampledAt = at.Add(24 * time.Hour)
	write(at)
	d.SampledAt = at
	write(at.Add(2 * time.Minute))
	page, err := s.NodeTrafficHistory(ctx, node, 0, 50, at.Add(4*time.Minute))
	if err != nil || page.Delivery == nil || page.Delivery.FreshForMS != 0 || !page.Delivery.ReceivedAt.Equal(at) {
		t.Fatal("retry refreshed old evidence", page, err)
	}
	d.SampledAt = at.Add(4 * time.Minute)
	write(d.SampledAt)
	page, _ = s.NodeTrafficHistory(ctx, node, 0, 50, d.SampledAt)
	if page.Delivery.FreshForMS <= 0 {
		t.Fatal(page)
	}
	s.db.Exec(`UPDATE nodes SET token_hash='identity-two' WHERE id=?`, node)
	page, _ = s.NodeTrafficHistory(ctx, node, 0, 50, at)
	if page.Delivery != nil {
		t.Fatal("old pairing queue escaped", page)
	}
	if _, _, err := s.AcceptNodeTraffic(ctx, node, strings.Repeat("b", 32), 1, nil, nil, nil, at.Add(-8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneStats(ctx, at); err != nil {
		t.Fatal(err)
	}
	var rows, cursors int
	s.db.QueryRow(`SELECT COUNT(*) FROM node_traffic_history`).Scan(&rows)
	s.db.QueryRow(`SELECT COUNT(*) FROM node_traffic_receipts`).Scan(&cursors)
	if rows != 0 || cursors != 1 {
		t.Fatal("retention changed lifetime receipt", rows, cursors)
	}
	if _, seen, err := s.AcceptNodeTraffic(ctx, node, strings.Repeat("b", 32), 1, nil, nil, nil, at); err != nil || !seen {
		t.Fatal(seen, err)
	}
}

func TestTrafficHistoryHardBudgetsAndDeletion(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now()
	// Populate a legal boundary efficiently without running 12,000 billing
	// transactions. The next accepted batch must evict only the oldest row.
	_, err := s.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?)
	INSERT INTO node_traffic_history(node_id,token_hash,epoch,sequence,received_at,up_bytes,down_bytes,totals_known,discarded_samples,metadata_status,sample_json)
	SELECT ?,'','',x,?,0,0,1,0,'legacy','null' FROM n`, trafficHistoryMaxRows, node, at.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AcceptNodeTraffic(ctx, node, strings.Repeat("c", 32), 1, nil, nil, nil, at); err != nil {
		t.Fatal(err)
	}
	var count int
	var minID int64
	s.db.QueryRow(`SELECT COUNT(*),MIN(id) FROM node_traffic_history WHERE node_id=?`, node).Scan(&count, &minID)
	if count != trafficHistoryMaxRows || minID != 2 {
		t.Fatal(count, minID)
	}
	// A high autoincrement position exercises the global bound without
	// assigning old observations a new receipt or changing their timestamps.
	s.db.Exec(`UPDATE sqlite_sequence SET seq=? WHERE name='node_traffic_history'`, trafficHistoryGlobalMaxRows+100)
	if _, _, err = s.AcceptNodeTraffic(ctx, node, strings.Repeat("c", 32), 2, nil, nil, nil, at); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRow(`SELECT MIN(id) FROM node_traffic_history`).Scan(&minID)
	if minID < 4 {
		t.Fatal("global old row not evicted", minID)
	}
	if _, err = s.db.Exec(`DELETE FROM nodes WHERE id=?`, node); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"node_traffic_history", "node_traffic_delivery", "node_traffic_receipts"} {
		s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count)
		if count != 0 {
			t.Fatal(table, count)
		}
	}
	// The public view never exports hashes or identities used to bind records.
	raw, _ := json.Marshal(TrafficHistoryPage{})
	if strings.Contains(string(raw), "token") {
		t.Fatal(string(raw))
	}
}

func TestTrafficHistoryGlobalCapAndReset(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now()
	ids := []int64{node}
	for i := 1; i < 10; i++ {
		ids = append(ids, proxyTrafficNode(t, s, "budget-"+string(rune('a'+i))))
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		_, err = tx.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000)
	INSERT INTO node_traffic_history(node_id,token_hash,epoch,sequence,received_at,up_bytes,down_bytes,totals_known,discarded_samples,metadata_status,sample_json)
	SELECT ?,'','',x,?,0,0,1,0,'legacy','null' FROM n`, id, at.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, _, err := s.AcceptNodeTraffic(ctx, node, strings.Repeat("d", 32), 1, nil, nil, nil, at); err != nil {
		t.Fatal(err)
	}
	t.Log("100,000-row bounded acceptance:", time.Since(start))
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM node_traffic_history`).Scan(&count)
	if count != trafficHistoryGlobalMaxRows {
		t.Fatal(count)
	}
	admin := &domain.User{Email: "reset-history@example.com", Role: domain.RoleAdmin, Status: "active", PasswordHash: "synthetic"}
	if err := s.CreateStaff(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResetSite(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"node_traffic_history", "node_traffic_delivery", "node_traffic_receipts"} {
		s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count)
		if count != 0 {
			t.Fatal(table, count)
		}
	}
}

func TestTrafficHistoryBindsAuthenticatedReporterIdentity(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now()
	if _, err := s.db.Exec(`UPDATE nodes SET token_hash='new-pairing' WHERE id=?`, node); err != nil {
		t.Fatal(err)
	}
	observation := TrafficObservation{Sample: trafficSampleFixture(at), TokenHash: "old-pairing"}
	if _, _, err := s.AcceptNodeTraffic(ctx, node, strings.Repeat("e", 32), 1, nil, nil, nil, at, observation); err != nil {
		t.Fatal(err)
	}
	page, err := s.NodeTrafficHistory(ctx, node, 0, 50, at)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("late old reporter relabeled as current identity", page, err)
	}
	observation.TokenHash = "new-pairing"
	if _, _, err := s.AcceptNodeTraffic(ctx, node, strings.Repeat("f", 32), 1, nil, nil, nil, at, observation); err != nil {
		t.Fatal(err)
	}
	page, err = s.NodeTrafficHistory(ctx, node, 0, 50, at)
	if err != nil || len(page.Items) != 1 || page.Items[0].Epoch != strings.Repeat("f", 32) {
		t.Fatal(page, err)
	}
}

func TestTrafficHistoryPairingSnapshotIsCoherent(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now().UTC()
	for i, token := range []string{"pair-a", "pair-b"} {
		if _, _, err := s.AcceptNodeTraffic(ctx, node, strings.Repeat(string(rune('a'+i)), 32), 1, nil, nil, nil, at, TrafficObservation{TokenHash: token}); err != nil {
			t.Fatal(err)
		}
	}
	write := func(i int) error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		token := []string{"pair-a", "pair-b"}[i%2]
		if _, err := tx.ExecContext(ctx, `UPDATE nodes SET token_hash=? WHERE id=?`, token, node); err != nil {
			return err
		}
		if err := recordTrafficDeliveryTx(ctx, tx, node, token, &agentproto.TrafficDelivery{SampledAt: at, PendingBytes: int64(i%2 + 1), JournalBytes: 100}, at); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := write(0); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := write(i); err != nil {
				errs <- err
				return
			}
		}
	}()
	defer func() {
		close(stop)
		wg.Wait()
		select {
		case err := <-errs:
			t.Error(err)
		default:
		}
	}()
	for range 200 {
		page, err := s.NodeTrafficHistory(ctx, node, 0, 50, at)
		if err != nil || len(page.Items) != 1 || page.Delivery == nil {
			t.Fatalf("pairing snapshot mixed: items=%d delivery=%+v err=%v", len(page.Items), page.Delivery, err)
		}
		want := strings.Repeat(string(rune('a'+page.Delivery.PendingBytes-1)), 32)
		if page.Items[0].Epoch != want {
			t.Fatal("history and current delivery use different pairings", page)
		}
	}
}

func TestTrafficHistoryAggregateOverflowRemainsUnknown(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now()
	// Individually valid report rows can exceed int64 when aggregated across
	// a sufficiently large server. This metadata sum must not wrap to zero.
	samples := make([]TrafficSample, 8192)
	for i := range samples {
		samples[i].Up = 1 << 50
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := recordTrafficHistoryTx(ctx, tx, node, "", 0, samples, TrafficObservation{}, at); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	page, err := s.NodeTrafficHistory(ctx, node, 0, 50, at)
	if err != nil || len(page.Items) != 1 || page.Items[0].TotalsKnown || page.Items[0].UpBytes != 0 || page.Items[0].DownBytes != 0 {
		t.Fatal("overflow claimed measured bytes", page, err)
	}
}

func TestTrafficHistoryServerClockRollbackWithdrawsFreshness(t *testing.T) {
	s, node, _ := monitorRig(t)
	ctx := context.Background()
	at := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := recordTrafficDeliveryTx(ctx, tx, node, "", &agentproto.TrafficDelivery{SampledAt: at, PendingBytes: 42, JournalBytes: 100}, at); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	page, err := s.NodeTrafficHistory(ctx, node, 0, 50, at.Add(-time.Hour))
	if err != nil || page.Delivery == nil || page.Delivery.FreshForMS != 0 {
		t.Fatal("server clock rollback refreshed old queue", page.Delivery, err)
	}
}
