package store

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var ErrNodeCost = errors.New("invalid node cost, expiry date or traffic reset policy")
var costAmount = regexp.MustCompile(`^(0|[1-9][0-9]{0,11})(\.[0-9]{1,4})?$`)
var costYears = regexp.MustCompile(`^(0|[1-9][0-9]{0,2})(\.[0-9]{1,4})?$`)

// Decimal strings preserve entered amounts without binary floating-point rounding.
type NodeCost struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Cycle    string `json:"cycle"`
	Years    string `json:"years,omitempty"`
}
type NodeTrafficReset struct {
	Mode string `json:"mode"`
}

func normalizeNodeCost(info *NodeProbeInfo, old NodeProbeInfo) error {
	if info.Billing == nil {
		// Legacy clients can edit their free-text price; unchanged echoes preserve structure.
		if info.Price == old.Price {
			info.Billing = old.Billing
		}
	}
	if info.TrafficReset == nil {
		info.TrafficReset = old.TrafficReset
	}
	if info.ExpiresAt != "" {
		date, err := time.Parse("2006-01-02", info.ExpiresAt)
		if err != nil || date.Year() < 1 {
			return ErrNodeCost
		}
	}
	if b := info.Billing; b != nil {
		b.Currency = strings.ToUpper(b.Currency)
		if !ValidCurrency(b.Currency) {
			return ErrNodeCost
		}
		if b.Amount != "" && !costAmount.MatchString(b.Amount) {
			return ErrNodeCost
		}
		switch b.Cycle {
		case "monthly", "quarterly", "semiannual", "annual":
			b.Years = ""
		case "custom":
			if !costYears.MatchString(b.Years) || strings.Trim(b.Years, "0.") == "" {
				return ErrNodeCost
			}
		default:
			return ErrNodeCost
		}
		info.Price = b.Display()
	}
	if r := info.TrafficReset; r != nil {
		switch r.Mode {
		case "manual":
		case "expiry", "expiry_next":
			if info.ExpiresAt == "" && (old.ExpiresAt != "" || old.TrafficReset == nil || old.TrafficReset.Mode != r.Mode) {
				return ErrNodeCost
			}
		default:
			return ErrNodeCost
		}
	}
	return normalizeNodeAutoRenewal(info, old)
}
func (b NodeCost) Display() string {
	if b.Amount == "" {
		return ""
	}
	cycle := map[string]string{"monthly": "month", "quarterly": "quarter", "semiannual": "6 months", "annual": "year"}[b.Cycle]
	if b.Cycle == "custom" {
		cycle = b.Years + " years"
	}
	return b.Currency + " " + b.Amount + " / " + cycle
}

func monthlyBoundary(at time.Time, offset, day, after int) time.Time {
	month := time.Date(at.Year(), at.Month()+time.Month(offset), 1, 0, 0, 0, 0, at.Location())
	last := month.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return month.AddDate(0, 0, day-1+after)
}
func nodePeriodDay(p *NodeProbe) (day, after int) {
	day = p.ResetDay
	if r := p.Info.TrafficReset; r != nil && r.Mode != "manual" {
		anchor := p.Info.ExpiresAt
		if renewal := p.Info.AutoRenewal; renewal != nil && renewal.AnchorDate != "" {
			anchor = renewal.AnchorDate
		}
		if date, err := time.Parse("2006-01-02", anchor); err == nil {
			day = date.Day()
			if renewal := p.Info.AutoRenewal; renewal != nil && renewal.AnchorDay >= 1 && renewal.AnchorDay <= 31 {
				day = renewal.AnchorDay
			}
			if r.Mode == "expiry_next" {
				after = 1
			}
		}
	}
	if day < 1 || day > 31 {
		day = 1
	}
	return
}

func nodePeriodStart(at time.Time, p *NodeProbe) time.Time {
	day, after := nodePeriodDay(p)
	start := monthlyBoundary(at, 0, day, after)
	if start.After(at) {
		start = monthlyBoundary(at, -1, day, after)
	}
	return start
}

// nodePeriodEnd returns the next scheduled boundary, including after a manual
// reset inside a period. Adding a month to a clamped February date is incorrect.
func nodePeriodEnd(start time.Time, p *NodeProbe) time.Time {
	day, after := nodePeriodDay(p)
	end := monthlyBoundary(start, 0, day, after)
	if !end.After(start) {
		end = monthlyBoundary(start, 1, day, after)
	}
	return end
}
