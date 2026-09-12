// Package domain holds Captain's core types. They mirror the database rows
// closely; behavior lives in service.
package domain

import (
	"errors"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

type User struct {
	ID           int64
	InviteCode   string
	InvitedBy    *int64
	Email        string
	PasswordHash string
	Role         string // "admin" | "user"
	UUID         string
	SubToken     string
	GroupID      *int64
	BalanceCents int64
	Status       string // "active" | "banned"
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (u *User) IsAdmin() bool { return u.Role == "admin" }

type Session struct {
	ID        string
	UserID    int64
	ExpiresAt time.Time
}

type Plan struct {
	ID             int64
	Name           string
	PriceCents     int64
	PeriodDays     int
	QuotaBytes     int64
	DeviceLimit    int
	SpeedLimitMbps int
	ResetDays      int // quota resets every N days within the period (ResetMode "days")
	ResetMode      string // "" never, "days", "monthly" (1st of each month), "yearly" (Jan 1)
	Prices         []PlanPrice // extra periods on top of PeriodDays/PriceCents
	GroupID        *int64
	Sort           int
	Enabled        bool
}

// PlanPrice is one purchasable period of a plan.
type PlanPrice struct {
	PeriodDays int   `json:"period_days"`
	PriceCents int64 `json:"price_cents"`
}

// PriceFor returns the price for a period: the base period, one of the
// extra ones, or false.
func (p *Plan) PriceFor(periodDays int) (int64, bool) {
	if periodDays == 0 || periodDays == p.PeriodDays {
		return p.PriceCents, true
	}
	for _, pp := range p.Prices {
		if pp.PeriodDays == periodDays {
			return pp.PriceCents, true
		}
	}
	return 0, false
}

// EffectiveResetMode maps the legacy reset_days field onto ResetMode.
func (p *Plan) EffectiveResetMode() string {
	if p.ResetMode == "" && p.ResetDays > 0 {
		return "days"
	}
	return p.ResetMode
}

type Subscription struct {
	ID            int64
	UserID        int64
	PlanID        int64
	StartsAt      time.Time
	ExpiresAt     *time.Time
	QuotaBytes    int64
	UsedUpBytes   int64
	UsedDownBytes int64
	ResetAt       *time.Time
	Status        string
}

// Usable reports whether the subscription still grants access at t.
func (s *Subscription) Usable(t time.Time) bool {
	if s.Status != "active" {
		return false
	}
	if s.ExpiresAt != nil && !t.Before(*s.ExpiresAt) {
		return false
	}
	if s.QuotaBytes > 0 && s.UsedUpBytes+s.UsedDownBytes >= s.QuotaBytes {
		return false
	}
	return true
}

type Node struct {
	ID              int64
	Name            string
	PublicAddr      string
	InternalAddr    string
	V6Addr          string
	MonitorURL      string
	Version         string
	Platform        string
	Hostname        string
	LastSeenAt      *time.Time
	AppliedRevision string
	UpgradeTo       string // bosun release the operator asked the node to move to
	Paired          bool
	PairCode        string // only set right after creation
	CreatedAt       time.Time
}

// Inbound is a protocol server on a node. Settings carries the protocol
// specific part of spec.Inbound; the identifying fields are columns.
type Inbound struct {
	ID       int64
	NodeID   int64
	Tag      string
	Protocol spec.Protocol
	Listen   string
	Port     int
	Core     string
	Settings spec.Inbound // only protocol-specific fields are used
	GroupID  *int64
	Enabled  bool
	Sort     int
}

// Spec materializes the full spec.Inbound bosun renders.
func (i *Inbound) Spec() spec.Inbound {
	s := i.Settings
	s.Tag, s.Protocol, s.Listen, s.Port, s.Core = i.Tag, i.Protocol, i.Listen, i.Port, i.Core
	return s
}

// Entry is a line in a user's subscription.
type Entry struct {
	ID          int64
	Name        string
	InboundID   int64
	ChainID     *int64
	DisplayHost string
	DisplayPort int
	Rate        float64
	Sort        int
	Enabled     bool
}

// Order is a purchase of a plan. Status moves pending -> paid or cancelled;
// paid is terminal and idempotent under repeated gateway notifications.
type Order struct {
	ID          int64
	No          string
	UserID      int64
	PlanID      int64
	AmountCents int64
	Gateway     string
	GatewayRef  string
	Status      string
	CreatedAt   time.Time
	PaidAt      *time.Time
	PeriodDays    int    // chosen period, 0 = plan base
	CouponID      *int64 //
	DiscountCents int64  //
}

// Coupon is a discount code applied at checkout.
type Coupon struct {
	ID        int64
	Code      string
	Name      string
	Kind      string // "percent" | "fixed"
	Value     int64
	PlanIDs   []int64 // empty = any plan
	MaxUses   int
	Used      int
	PerUser   int
	StartsAt  *time.Time
	ExpiresAt *time.Time
	Enabled   bool
	CreatedAt time.Time
}

// Discount returns the cents taken off amount.
func (c *Coupon) Discount(amount int64) int64 {
	var d int64
	switch c.Kind {
	case "percent":
		d = amount * c.Value / 100
	case "fixed":
		d = c.Value
	}
	if d > amount {
		d = amount
	}
	if d < 0 {
		d = 0
	}
	return d
}

// Usable reports whether the coupon may be applied now to plan.
func (c *Coupon) Usable(at time.Time, planID int64) error {
	if !c.Enabled {
		return errors.New("coupon disabled")
	}
	if c.StartsAt != nil && at.Before(*c.StartsAt) {
		return errors.New("coupon not active yet")
	}
	if c.ExpiresAt != nil && at.After(*c.ExpiresAt) {
		return errors.New("coupon expired")
	}
	if c.MaxUses > 0 && c.Used >= c.MaxUses {
		return errors.New("coupon fully used")
	}
	if len(c.PlanIDs) > 0 {
		found := false
		for _, id := range c.PlanIDs {
			if id == planID {
				found = true
			}
		}
		if !found {
			return errors.New("coupon does not apply to this plan")
		}
	}
	return nil
}

// Group is a user group; plans put buyers in a group and inbounds may be
// restricted to one.
type Group struct {
	ID   int64
	Name string
}
