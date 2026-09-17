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
	RegisterIP   string
	Email        string
	PasswordHash string
	Role         string // "admin" | "user"
	UUID         string
	SubToken     string
	GroupID      *int64
	BalanceCents int64
	Status       string // "active" | "banned"
	// HwidLimit overrides the plan's device limit for HWID-identified
	// clients: nil = plan limit, 0 = unlimited for this user.
	HwidLimit *int
	// FirstConnectedAt is when the panel first saw traffic for the user.
	FirstConnectedAt *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Staff roles. "admin" can do everything; "operator" runs the business but
// cannot change settings, system or staff; "support" only handles tickets
// and looks at users and orders.
const (
	RoleUser     = "user"
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleSupport  = "support"
)

// IsAdmin reports the full-access role.
func (u *User) IsAdmin() bool { return u.Role == RoleAdmin }

// IsStaff reports any console role.
func (u *User) IsStaff() bool {
	return u.Role == RoleAdmin || u.Role == RoleOperator || u.Role == RoleSupport
}

// ValidStaffRole reports whether r names a console role.
func ValidStaffRole(r string) bool { return r == RoleAdmin || r == RoleOperator || r == RoleSupport }

type Session struct {
	ID        string
	UserID    int64
	ExpiresAt time.Time
	// Admin is set only by the admin login (password + authenticator);
	// portal, OIDC and password-reset sessions never reach /api/admin.
	Admin bool
}

type Plan struct {
	ID             int64
	Name           string
	PriceCents     int64
	PeriodDays     int
	QuotaBytes     int64
	DeviceLimit    int
	SpeedLimitMbps int
	ResetDays      int         // quota resets every N days within the period (ResetMode "days")
	ResetMode      string      // "" never, "days", "monthly" (1st of each month), "yearly" (Jan 1)
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
	Status        string // "active" | "queued" | "expired" | "cancelled"
	PeriodDays    int    // what a queued row starts with (0 = plan base)
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
	ID           int64
	Name         string
	PublicAddr   string
	InternalAddr string
	V6Addr       string
	Domain       string // host name under a registered domain, e.g. jp1.example.com
	MonitorURL   string
	// DecoyEnabled makes the node serve Domain itself on loopback for
	// REALITY inbounds to steal; DecoyUpstream optionally proxies a site.
	DecoyEnabled  bool
	DecoyUpstream string
	// UserSpeedLimitMbps caps every user on this node without a plan limit.
	UserSpeedLimitMbps int
	// MitaQuotas also writes each user's allowance into mita's own quotas
	// so the core enforces it when the panel is unreachable.
	MitaQuotas bool
	// EgressByIngress makes inbounds bound to a specific address exit
	// from that address (multi-IP hosts).
	EgressByIngress bool
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
	ID        int64
	NodeID    int64
	Tag       string
	Protocol  spec.Protocol
	Listen    string
	Port      int
	Core      string
	Settings  spec.Inbound // only protocol-specific fields are used
	GroupID   *int64
	Enabled   bool
	Sort      int
	IngressID *int64 // nil = the node's direct entry
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
	Tags        []string // free-form labels shown to users and used for filtering
	Region      string   // ISO 3166-1 alpha-2; "" = detect from the name when auto flags are on
	// ClientExtra is merged into this entry's proxy in the map-shaped
	// client formats (mihomo/Clash, Stash, sing-box): tfo, smux, dialer-proxy…
	ClientExtra map[string]any
}

// Order is a purchase of a plan. Status moves pending -> paid or cancelled;
// paid is terminal and idempotent under repeated gateway notifications.
type Order struct {
	ID            int64
	No            string
	UserID        int64
	PlanID        int64
	AmountCents   int64
	Gateway       string
	GatewayRef    string
	Status        string
	CreatedAt     time.Time
	PaidAt        *time.Time
	PeriodDays    int    // chosen period, 0 = plan base
	CouponID      *int64 //
	DiscountCents int64  //
	SurplusCents  int64  // credit from the replaced plan's unused remainder
	Activation    string // "" starts on payment (stack / renew), "queue" waits for the current plans to lapse
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

// Ticket is a support conversation between a user and the operator.
type Ticket struct {
	ID        int64
	UserID    int64
	Subject   string
	Status    string // open | replied | closed
	Priority  string // low | normal | high
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TicketMessage is one message in a ticket.
type TicketMessage struct {
	ID        int64
	TicketID  int64
	FromAdmin bool
	Body      string
	CreatedAt time.Time
}

// GiftCode is a single-use redeem code.
type GiftCode struct {
	ID         int64
	Code       string
	Batch      string
	Kind       string // balance | plan | traffic | days
	Value      int64  // cents, bytes or days depending on Kind
	PlanID     *int64
	PeriodDays int
	ExpiresAt  *time.Time
	RedeemedBy *int64
	RedeemedAt *time.Time
	CreatedAt  time.Time
}

// Article is a knowledge-base page written in Markdown.
type Article struct {
	ID        int64
	Title     string
	Category  string
	Body      string
	Lang      string // "" any, "zh-CN", "en"
	Sort      int
	Published bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
