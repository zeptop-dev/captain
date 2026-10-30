package store

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/domain"
)

// ValidateInboundIngress is shared by ordinary writes and typed change sets.
func ValidateInboundIngress(ib *domain.Inbound, gs []Ingress) error {
	if e := ib.Spec().CheckCoreListen(""); e != nil {
		return e
	}
	id := ""
	if ib.IngressID != nil {
		id = strconv.FormatInt(*ib.IngressID, 10)
	}
	var selected *Ingress
	for i := range gs {
		g := &gs[i]
		if id == "" && g.RequireIngress {
			return errors.New("this node requires an ingress for every inbound and forward")
		}
		if id != "" && strconv.FormatInt(g.ID, 10) == id {
			selected = g
		}
	}
	if id != "" && selected == nil {
		return errors.New("ingress not found on this node")
	}
	if selected == nil {
		return nil
	}
	if e := selected.Ports().Check(ib.Port); e != nil {
		return fmt.Errorf("%s: %w", selected.Name, e)
	}
	if e := spec.CheckIngressListen(selected.BindIP, ib.Listen); e != nil {
		return e
	}
	if e := ib.Spec().CheckCoreListen(selected.BindIP); e != nil {
		return e
	}
	return selected.Ports().CheckInbound(ib.Spec())
}

// ValidateInboundCore retains the ordinary editor's capability fallback for
// older/offline nodes; protocols requiring fresh support remain gated.
func ValidateInboundCore(ib *domain.Inbound, n *domain.Node, candidates []spec.CoreCandidate, gs []Ingress) error {
	if !ib.Enabled && !ib.Settings.PrivateAccess.Enabled() {
		return nil
	}
	if n == nil {
		return ErrNotFound
	}
	if ib.Protocol == spec.Mieru && ib.Core == "singbox-extended" && n.MitaQuotas {
		return errors.New("extended Mieru does not support mita native quotas; disable native quotas or select mita")
	}
	if (ib.Protocol == spec.SSH || ib.Core == "singbox-extended") && candidates == nil {
		return errors.New("this core or protocol requires a recent supported node capability report")
	}
	if ib.Settings.PrivateAccess.Enabled() && candidates == nil {
		return errors.New("private access requires a recent supported node capability report")
	}
	if candidates != nil {
		input := ib.Spec()
		if ib.IngressID != nil {
			for _, g := range gs {
				if g.ID == *ib.IngressID && g.BindIP != "" {
					input.Listen = g.BindIP
				}
			}
		}
		_, e := spec.SelectCore(input, candidates)
		return e
	}
	return nil
}
