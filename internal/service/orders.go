package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gitlab.com/boyang-hu/captain/internal/domain"
	"gitlab.com/boyang-hu/captain/internal/payment"
	"gitlab.com/boyang-hu/captain/internal/store"
)

// Orders creates orders and settles them through gateways or balance.
type Orders struct {
	Store    *store.Store
	Gateways map[string]payment.Gateway // by name
}

// ErrGateway means the requested gateway is not configured.
var ErrGateway = errors.New("orders: gateway not available")

// Create makes a pending order for plan and starts the payment. For
// "balance" the order settles immediately and Checkout is nil.
func (o *Orders) Create(ctx context.Context, user *domain.User, planID int64, gateway, clientIP string) (*domain.Order, *payment.Checkout, error) {
	plan, err := o.Store.PlanByID(ctx, planID)
	if err != nil || !plan.Enabled {
		return nil, nil, fmt.Errorf("orders: plan not available")
	}
	order := &domain.Order{No: newOrderNo(), UserID: user.ID, PlanID: plan.ID, AmountCents: plan.PriceCents, Gateway: gateway}
	switch gateway {
	case "balance":
		if err := o.Store.CreateOrder(ctx, order); err != nil {
			return nil, nil, err
		}
		paid, err := o.Store.PayWithBalance(ctx, order.No, time.Now())
		if err != nil {
			return nil, nil, err
		}
		return paid, nil, nil
	default:
		gw, ok := o.Gateways[gateway]
		if !ok {
			return nil, nil, ErrGateway
		}
		if plan.PriceCents == 0 {
			return nil, nil, fmt.Errorf("orders: free plan: use balance")
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
	return order, err
}

// newOrderNo is time-prefixed for readability with random tail; max 32 chars
// as EPay requires.
func newOrderNo() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102150405") + hex.EncodeToString(b)
}
