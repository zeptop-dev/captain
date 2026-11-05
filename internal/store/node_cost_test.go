package store

import (
	"fmt"
	"testing"
	"time"
)

func TestNodeCostValidationAndLegacy(t *testing.T) {
	old := NodeProbeInfo{Price: "legacy price"}
	info := old
	if err := normalizeNodeCost(&info, old); err != nil || info.Price != old.Price {
		t.Fatal(info, err)
	}
	info.Billing = &NodeCost{Amount: "36.36", Currency: "USD", Cycle: "annual"}
	if err := normalizeNodeCost(&info, old); err != nil || info.Price != "USD 36.36 / year" {
		t.Fatal(info, err)
	}
	old = info
	echo := NodeProbeInfo{Price: old.Price}
	if err := normalizeNodeCost(&echo, old); err != nil || echo.Billing == nil {
		t.Fatal("legacy echo lost billing", err)
	}
	edit := NodeProbeInfo{Price: "new legacy price"}
	if err := normalizeNodeCost(&edit, old); err != nil || edit.Billing != nil {
		t.Fatal("legacy edit ignored", err)
	}
	for _, bad := range []NodeCost{{Amount: "-1", Currency: "USD", Cycle: "annual"}, {Amount: "NaN", Currency: "USD", Cycle: "annual"}, {Amount: "1", Currency: "ZZZ", Cycle: "annual"}, {Amount: "1", Currency: "USD", Cycle: "custom", Years: "0"}, {Amount: "1", Currency: "USD", Cycle: "custom", Years: "-1"}} {
		info = NodeProbeInfo{Billing: &bad}
		if normalizeNodeCost(&info, NodeProbeInfo{}) == nil {
			t.Fatal("accepted invalid cost", bad)
		}
	}
	for _, amount := range []string{"", "0", "36.3600"} {
		info = NodeProbeInfo{Billing: &NodeCost{Amount: amount, Currency: "USD", Cycle: "custom", Years: "1.5"}}
		if err := normalizeNodeCost(&info, NodeProbeInfo{}); err != nil {
			t.Fatal(err)
		}
		if amount == "" && info.Price != "" {
			t.Fatal("empty cost visible")
		}
	}
	info = NodeProbeInfo{ExpiresAt: "2027-02-29"}
	if normalizeNodeCost(&info, NodeProbeInfo{}) == nil {
		t.Fatal("accepted invalid date")
	}
	for _, mode := range []string{"expiry", "expiry_previous", "expiry_next"} {
		info = NodeProbeInfo{TrafficReset: &NodeTrafficReset{Mode: mode}}
		if normalizeNodeCost(&info, NodeProbeInfo{}) == nil {
			t.Fatal("missing expiry accepted", mode)
		}
	}
}

func TestNodeCostPreviousExpiryBoundaries(t *testing.T) {
	for _, tc := range []struct{ at, expiry, start, end string }{
		{"2027-01-29", "2027-01-31", "2026-12-30", "2027-01-30"},
		{"2027-01-30", "2027-01-31", "2027-01-30", "2027-02-27"},
		{"2027-01-31", "2027-01-31", "2027-01-30", "2027-02-27"},
		{"2027-02-26", "2027-01-31", "2027-01-30", "2027-02-27"},
		{"2027-02-27", "2027-01-31", "2027-02-27", "2027-03-30"},
		{"2028-02-28", "2027-01-31", "2028-02-28", "2028-03-30"},
		{"2027-04-29", "2027-01-31", "2027-04-29", "2027-05-30"},
		{"2027-01-01", "2027-01-01", "2026-12-31", "2027-01-31"},
		{"2027-01-30", "2027-01-01", "2026-12-31", "2027-01-31"},
		{"2027-01-31", "2027-01-01", "2027-01-31", "2027-02-28"},
		{"2027-02-28", "2027-01-01", "2027-02-28", "2027-03-31"},
		{"2028-02-29", "2027-01-01", "2028-02-29", "2028-03-31"},
		{"2027-12-31", "2027-01-01", "2027-12-31", "2028-01-31"},
		{"2027-06-08", "2027-06-09", "2027-06-08", "2027-07-08"},
	} {
		t.Run(tc.at+"/"+tc.expiry, func(t *testing.T) {
			at, _ := time.Parse("2006-01-02", tc.at)
			p := NodeProbe{ResetDay: 15, Info: NodeProbeInfo{ExpiresAt: tc.expiry, TrafficReset: &NodeTrafficReset{Mode: "expiry_previous"}}}
			start := nodePeriodStart(at, &p)
			if got := start.Format("2006-01-02"); got != tc.start {
				t.Fatalf("start = %s; want %s", got, tc.start)
			}
			for _, from := range []time.Time{start, at.Add(12 * time.Hour)} {
				if got := nodePeriodEnd(from, &p).Format("2006-01-02"); got != tc.end {
					t.Fatalf("end from %s = %s; want %s", from, got, tc.end)
				}
			}
		})
	}
}

func TestNodeCostPreviousExpiryPreservesRenewalAnchor(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, anchorDay := range []int{0, 31} {
			t.Run(fmt.Sprintf("enabled=%v/day=%d", enabled, anchorDay), func(t *testing.T) {
				p := NodeProbe{ResetDay: 1, Info: NodeProbeInfo{
					ExpiresAt: "2027-02-28", TrafficReset: &NodeTrafficReset{Mode: "expiry_previous"},
					AutoRenewal: &NodeAutoRenewal{Enabled: enabled, AnchorDate: "2027-01-31", AnchorDay: anchorDay},
				}}
				if anchorDay != 0 {
					p.Info.AutoRenewal.AnchorDate = "2027-02-28"
				}
				at := time.Date(2027, 3, 30, 0, 0, 0, 0, time.UTC)
				if got := nodePeriodStart(at, &p); !got.Equal(at) {
					t.Fatalf("clamped expiry lost original anchor: %s", got)
				}
			})
		}
	}
}

func TestNodeCostPreviousExpiryUsesCalendarDay(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	// Spring/autumn DST changes must not move a reset away from midnight.
	for _, date := range []string{"2027-03-14", "2027-11-07"} {
		want, _ := time.ParseInLocation("2006-01-02", date, loc)
		p := NodeProbe{Info: NodeProbeInfo{ExpiresAt: want.AddDate(0, 0, 1).Format("2006-01-02"), TrafficReset: &NodeTrafficReset{Mode: "expiry_previous"}}}
		if got := nodePeriodStart(want.Add(12*time.Hour), &p); !got.Equal(want) || got.Location() != loc {
			t.Fatalf("reset = %s; want %s", got, want)
		}
		if got := nodePeriodEnd(want.Add(-time.Second), &p); !got.Equal(want) {
			t.Fatalf("upcoming reset = %s; want %s", got, want)
		}
	}
}
func TestNodeCostMonthlyBoundaries(t *testing.T) {
	for _, c := range []struct {
		at, expiry, mode, want string
		day                    int
	}{
		{"2027-02-28", "2027-01-31", "expiry", "2027-02-28", 1},
		{"2028-02-29", "2027-01-31", "expiry", "2028-02-29", 1},
		{"2027-02-28", "2027-01-31", "expiry_next", "2027-02-01", 1},
		{"2027-03-01", "2027-01-31", "expiry_next", "2027-03-01", 1},
		{"2027-01-01", "2027-01-31", "expiry_next", "2027-01-01", 1},
		{"2027-06-09", "2027-06-09", "expiry_next", "2027-05-10", 1},
		{"2027-06-10", "2027-06-09", "expiry_next", "2027-06-10", 1},
		{"2027-02-28", "", "manual", "2027-02-28", 31},
		{"2027-03-01", "", "manual", "2027-02-28", 31},
	} {
		t.Run(c.at+c.mode+c.want, func(t *testing.T) {
			at, _ := time.Parse("2006-01-02", c.at)
			p := NodeProbe{ResetDay: c.day, Info: NodeProbeInfo{ExpiresAt: c.expiry, TrafficReset: &NodeTrafficReset{Mode: c.mode}}}
			if got := nodePeriodStart(at, &p).Format("2006-01-02"); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}
