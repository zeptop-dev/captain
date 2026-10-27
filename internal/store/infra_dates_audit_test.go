package store

import (
	"testing"
	"time"
)

func TestInfrastructureOnlineAdvanceCrossesWholeCycleAudit(t *testing.T) {
	info := NodeProbeInfo{ExpiresAt: "2027-02-15", Billing: &NodeCost{Currency: "USD", Amount: "10", Cycle: "quarterly"}, AutoRenewal: &NodeAutoRenewal{Enabled: true, AnchorDate: "2027-02-15", AnchorDay: 31}}
	at, _ := time.Parse("2006-01-02", "2027-02-15")
	got, due := nextNodeExpiry(info, at)
	if !due || got != "2027-05-31" {
		t.Fatalf("automatic advance must cross a complete quarter, got %q, due %v", got, due)
	}
}
