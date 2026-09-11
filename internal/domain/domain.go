// Package domain holds Captain's core types. They mirror the database rows
// closely; behavior lives in service.
package domain

import (
	"time"

	"gitlab.com/boyang-hu/bosun/pkg/spec"
)

type User struct {
	ID           int64
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
	GroupID        *int64
	Sort           int
	Enabled        bool
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
}

// Group is a user group; plans put buyers in a group and inbounds may be
// restricted to one.
type Group struct {
	ID   int64
	Name string
}
