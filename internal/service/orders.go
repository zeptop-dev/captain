package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/payment"
	"github.com/zeptop-dev/captain/internal/store"
)

// Orders creates orders and settles them through gateways or balance.
type Orders struct {
	Store    *store.Store
	Gateways map[string]payment.Gateway // by name
	// OnPaid runs after an order is settled (notifications).
	OnPaid func(ctx context.Context, o *domain.Order)
}

// ErrGateway means the requested gateway is not configured.
var ErrGateway = errors.New("orders: gateway not available")

// Create makes a pending order for plan and starts the payment. For
// "balance" the order settles immediately and Checkout is nil.
// Quote prices a plan period for a user with an optional coupon.
type Quote struct {
	PlanID        int64  `json:"plan_id"`
	PeriodDays    int    `json:"period_days"`
	ListCents     int64  `json:"list_cents"`
	DiscountCents int64  `json:"discount_cents"`
	AmountCents   int64  `json:"amount_cents"`
	CouponID      *int64 `json:"-"`
	CouponName    string `json:"coupon,omitempty"`
}

// Price computes what a user pays for plan/period with couponCode ("" for none).
func (o *Orders) Price(ctx context.Context, user *domain.User, planID int64, periodDays int, couponCode string) (*Quote, *domain.Plan, error) {
	plan, err := o.Store.PlanByID(ctx, planID)
	if err != nil || !plan.Enabled {
		return nil, nil, fmt.Errorf("orders: plan not available")
	}
	list, ok := plan.PriceFor(periodDays)
	if !ok {
		return nil, nil, fmt.Errorf("orders: period not offered for this plan")
	}
	if periodDays == 0 {
		periodDays = plan.PeriodDays
	}
	q := &Quote{PlanID: plan.ID, PeriodDays: periodDays, ListCents: list, AmountCents: list}
	if code := strings.TrimSpace(couponCode); code != "" {
		c, err := o.Store.CouponByCode(ctx, code)
		if err != nil {
			return nil, nil, fmt.Errorf("orders: unknown coupon")
		}
		if err := c.Usable(time.Now(), plan.ID); err != nil {
			return nil, nil, fmt.Errorf("orders: %w", err)
		}
		if c.PerUser > 0 {
			if n, _ := o.Store.CouponUsesByUser(ctx, c.ID, user.ID); n >= c.PerUser {
				return nil, nil, fmt.Errorf("orders: coupon already used")
			}
		}
		q.DiscountCents = c.Discount(list)
		q.AmountCents = list - q.DiscountCents
		q.CouponID, q.CouponName = &c.ID, c.Name
		if q.CouponName == "" {
			q.CouponName = c.Code
		}
	}
	return q, plan, nil
}

func (o *Orders) Create(ctx context.Context, user *domain.User, planID int64, periodDays int, couponCode, gateway, clientIP string) (*domain.Order, *payment.Checkout, error) {
	q, plan, err := o.Price(ctx, user, planID, periodDays, couponCode)
	if err != nil {
		return nil, nil, err
	}
	order := &domain.Order{No: newOrderNo(), UserID: user.ID, PlanID: plan.ID, AmountCents: q.AmountCents, Gateway: gateway, PeriodDays: q.PeriodDays, CouponID: q.CouponID, DiscountCents: q.DiscountCents}
	switch gateway {
	case "balance":
		if err := o.Store.CreateOrder(ctx, order); err != nil {
			return nil, nil, err
		}
		paid, err := o.Store.PayWithBalance(ctx, order.No, time.Now())
		if err == nil && o.OnPaid != nil {
			o.OnPaid(ctx, paid)
		}
		if err != nil {
			return nil, nil, err
		}
		return paid, nil, nil
	default:
		gw, ok := o.Gateways[gateway]
		if !ok {
			return nil, nil, ErrGateway
		}
		if order.AmountCents == 0 {
			return nil, nil, fmt.Errorf("orders: nothing to pay: use balance")
		}
		if err := o.Store.CreateOrder(ctx, order); err != nil {
			return nil, nil, err
		}
		co, err := gw.Create(ctx, order, plan, user, clientIP)
		if err != nil {
			return nil, nil, err
		}
		return order, co, nil
	}
}

// Settle applies a verified gateway notification. It is idempotent.
func (o *Orders) Settle(ctx context.Context, n *payment.Notification) (*domain.Order, error) {
	if !n.Paid {
		return o.Store.OrderByNo(ctx, n.OrderNo)
	}
	order, err := o.Store.MarkPaid(ctx, n.OrderNo, n.GatewayRef, time.Now())
	if errors.Is(err, store.ErrAlreadyPaid) {
		return order, nil
	}
	if err == nil && o.OnPaid != nil {
		o.OnPaid(ctx, order)
	}
	return order, err
}

// newOrderNo is time-prefixed for readability with random tail; max 32 chars
// as EPay requires.
func newOrderNo() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102150405") + hex.EncodeToString(b)
}
