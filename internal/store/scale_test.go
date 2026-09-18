package store

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

// TestScale measures the one thing the single SQLite connection can run
// out of: write time on the hot paths at a given fleet size. It is skipped
// unless CAPTAIN_SCALE=1, because a full run takes minutes.
//
//	CAPTAIN_SCALE=1 SCALE_USERS=5000 SCALE_NODES=10 go test ./internal/store/ -run TestScale -v -timeout 30m
//
// What it does, on the real schema:
//   - fills users, plans and subscriptions;
//   - times one node report's traffic transaction (the panel's hottest
//     write: one row per user per report, each charged to a subscription);
//   - times online-device upserts, connection-log inserts, subscription
//     fetch records and the hourly cleanup;
//   - reads the dashboard and one user page while the writes run, which is
//     what an operator feels when the panel is busy.
func TestScale(t *testing.T) {
	if os.Getenv("CAPTAIN_SCALE") != "1" {
		t.Skip("set CAPTAIN_SCALE=1 to run the scale measurement")
	}
	users := envInt("SCALE_USERS", 5000)
	nodes := envInt("SCALE_NODES", 10)
	activePct := envInt("SCALE_ACTIVE_PCT", 20) // share of users with traffic in one report
	connPerNode := envInt("SCALE_CONN_PER_REPORT", 3000)

	ctx := context.Background()
	dir := t.TempDir()
	conn, err := db.Open("sqlite", filepath.Join(dir, "scale.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)

	t.Logf("filling %d users, %d nodes", users, nodes)
	fill := time.Now()
	plan := &domain.Plan{Name: "scale", PriceCents: 100, PeriodDays: 30, QuotaBytes: 1 << 40, Enabled: true}
	if err := s.CreatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}
	nodeIDs := make([]int64, 0, nodes)
	for i := 0; i < nodes; i++ {
		n := &domain.Node{Name: fmt.Sprintf("n%d", i), PublicAddr: fmt.Sprintf("198.51.100.%d", i+1)}
		if err := s.CreateNode(ctx, n, "code"+strconv.Itoa(i), time.Hour); err != nil {
			t.Fatal(err)
		}
		nodeIDs = append(nodeIDs, n.ID)
	}
	ids := make([]int64, 0, users)
	for i := 0; i < users; i++ {
		u := &domain.User{Email: fmt.Sprintf("u%d@scale.test", i), PasswordHash: "x", Role: "user",
			UUID: fmt.Sprintf("uuid-%d", i), SubToken: fmt.Sprintf("tok-%d", i), Status: "active"}
		if err := s.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GrantSubscription(ctx, u.ID, plan, time.Now()); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, u.ID)
	}
	t.Logf("  filled in %s (db %s)", time.Since(fill).Round(time.Millisecond), fileSize(filepath.Join(dir, "scale.db")))

	active := users * activePct / 100
	perNode := active / nodes
	if perNode == 0 {
		perNode = 1
	}
	rnd := rand.New(rand.NewSource(1))

	// One report from every node, as the agent handler does it.
	report := func(nodeIdx int) time.Duration {
		samples := make([]TrafficSample, 0, perNode)
		for i := 0; i < perNode; i++ {
			samples = append(samples, TrafficSample{UserID: ids[rnd.Intn(len(ids))], Up: 1 << 20, Down: 4 << 20})
		}
		start := time.Now()
		if _, err := s.AddTrafficSamples(ctx, samples, time.Now()); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}

	measure := func(name string, rounds int, f func(i int) time.Duration) {
		var all []time.Duration
		for i := 0; i < rounds; i++ {
			all = append(all, f(i))
		}
		sort.Slice(all, func(a, b int) bool { return all[a] < all[b] })
		var sum time.Duration
		for _, d := range all {
			sum += d
		}
		t.Logf("%-28s n=%-4d avg %-9s p50 %-9s p99 %s", name, rounds,
			(sum / time.Duration(len(all))).Round(time.Millisecond),
			all[len(all)/2].Round(time.Millisecond),
			all[len(all)*99/100].Round(time.Millisecond))
	}

	measure(fmt.Sprintf("report traffic (%d users)", perNode), nodes*3, func(i int) time.Duration { return report(i % nodes) })

	measure("online devices (1 ip/user)", nodes, func(i int) time.Duration {
		start := time.Now()
		for j := 0; j < perNode; j++ {
			if err := s.UpsertOnline(ctx, ids[rnd.Intn(len(ids))], nodeIDs[i%nodes], []string{fmt.Sprintf("203.0.113.%d", j%250)}, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		return time.Since(start)
	})

	measure(fmt.Sprintf("connection log (%d rows)", connPerNode), nodes, func(i int) time.Duration {
		rows := make([]ConnRow, 0, connPerNode)
		for j := 0; j < connPerNode; j++ {
			rows = append(rows, ConnRow{UserID: ids[rnd.Intn(len(ids))], NodeID: nodeIDs[i%nodes], At: time.Now(),
				ClientIP: "203.0.113.7", Host: "example.com", Port: 443, Network: "tcp"})
		}
		start := time.Now()
		if err := s.AddConnEvents(ctx, rows); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	})

	measure("subscription fetch record", 200, func(i int) time.Duration {
		start := time.Now()
		if err := s.RecordSubRequest(ctx, ids[i%len(ids)], SubRequest{RequestIP: "203.0.113.9", UserAgent: "clash", Response: "clash"}); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	})

	// Reads an operator waits for, while the writers keep going.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				report(0)
			}
		}
	}()
	measure("dashboard (under write load)", 20, func(int) time.Duration {
		start := time.Now()
		if _, err := s.Dashboard(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	})
	measure("user list page 1 (under load)", 20, func(int) time.Duration {
		start := time.Now()
		if _, _, err := s.ListUsers(ctx, "", 50, 0, time.Now()); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	})
	close(stop)
	wg.Wait()

	// The hourly cleanup, on a table with a day's worth of rows.
	t.Logf("db before cleanup: %s", fileSize(filepath.Join(dir, "scale.db")))
	measure("trim connection log", 1, func(int) time.Duration {
		start := time.Now()
		if _, err := s.TrimConnLog(ctx, 1000); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	})
	measure("prune connection log", 1, func(int) time.Duration {
		start := time.Now()
		if _, err := s.PruneConnLog(ctx, time.Now().Add(-7*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	})
	measure("backup (VACUUM INTO)", 1, func(int) time.Duration {
		start := time.Now()
		if err := s.Backup(ctx, filepath.Join(dir, "backup.db")); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	})
	t.Logf("db %s, backup %s", fileSize(filepath.Join(dir, "scale.db")), fileSize(filepath.Join(dir, "backup.db")))
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func fileSize(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%.1f MiB", float64(fi.Size())/(1<<20))
}
