package service

import (
	"context"
	"errors"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

var ErrServiceStatusPrivate = errors.New("service status is not public")

// ServiceStatusCatalog projects runtime observations onto a caller's visible
// subscription nodes. This DTO deliberately contains no network endpoints,
// infrastructure names, credentials, raw errors, or hidden category counts.
type ServiceStatusCatalog struct {
	Store        *store.Store
	Runtime      *ServiceStatus
	Subscription *Subscription
}

type ServiceStatusCategory struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Sort        int    `json:"sort"`
}

type ServiceStatusMaintenance struct {
	StartsAt int64  `json:"starts_at"`
	EndsAt   int64  `json:"ends_at"`
	Note     string `json:"note"`
}

type ServiceStatusEntry struct {
	ID       int64                  `json:"id"`
	Name     string                 `json:"name"`
	Rate     float64                `json:"rate"`
	Region   string                 `json:"region"`
	Category *ServiceStatusCategory `json:"category"`
	EntryRuntimeStatus
	Maintenance *ServiceStatusMaintenance `json:"maintenance"`
}

type ServiceStatusSummary struct {
	Healthy     int `json:"healthy"`
	Degraded    int `json:"degraded"`
	Down        int `json:"down"`
	Maintenance int `json:"maintenance"`
	Unknown     int `json:"unknown"`
	Total       int `json:"total"`
}

type ServiceStatusView struct {
	GeneratedAt    int64                `json:"generated_at"`
	RefreshSeconds int                  `json:"refresh_seconds"`
	Summary        ServiceStatusSummary `json:"summary"`
	Entries        []ServiceStatusEntry `json:"entries"`
}

// Public is opt-in and never expands a category selection to future entries.
func (s *ServiceStatusCatalog) Public(ctx context.Context, at time.Time) (ServiceStatusView, error) {
	s.Store.Topology.Lock()
	defer s.Store.Topology.Unlock()
	cfg, err := s.Store.ServiceStatusConfig(ctx)
	if err != nil {
		return ServiceStatusView{}, err
	}
	if !cfg.PublicEnabled {
		return ServiceStatusView{}, ErrServiceStatusPrivate
	}
	allowed := map[int64]bool{}
	for _, id := range cfg.PublicEntryIDs {
		allowed[id] = true
	}
	entries, err := s.enabledEntries(ctx)
	if err != nil {
		return ServiceStatusView{}, err
	}
	visible := entries[:0]
	for _, e := range entries {
		if allowed[e.ID] {
			visible = append(visible, e)
		}
	}
	// A client-chain node is meaningful only when its client-side prerequisites
	// are published too. Internal server-side dependencies are never exposed.
	return s.build(ctx, availableStatusEntries(visible), nil, cfg, at)
}

func (s *ServiceStatusCatalog) Admin(ctx context.Context, at time.Time) (ServiceStatusView, error) {
	s.Store.Topology.Lock()
	defer s.Store.Topology.Unlock()
	cfg, err := s.Store.ServiceStatusConfig(ctx)
	if err != nil {
		return ServiceStatusView{}, err
	}
	entries, err := s.enabledEntries(ctx)
	if err != nil {
		return ServiceStatusView{}, err
	}
	return s.build(ctx, entries, nil, cfg, at)
}

func (s *ServiceStatusCatalog) Portal(ctx context.Context, u *domain.User, at time.Time) (ServiceStatusView, error) {
	s.Store.Topology.Lock()
	defer s.Store.Topology.Unlock()
	cfg, err := s.Store.ServiceStatusConfig(ctx)
	if err != nil {
		return ServiceStatusView{}, err
	}
	lines, _, err := s.Subscription.PublishedLines(ctx, u, at)
	if errors.Is(err, ErrNoAccess) {
		lines, err = nil, nil
	}
	if err != nil {
		return ServiceStatusView{}, err
	}
	entries := []*domain.Entry{}
	names := map[int64]string{}
	for _, l := range AvailablePublications(lines) {
		if l.EntryID == 0 {
			continue
		} // informational/imported lines have no managed Entry.
		e, err := s.Store.EntryByID(ctx, l.EntryID)
		if err != nil {
			return ServiceStatusView{}, err
		}
		entries = append(entries, e)
		names[e.ID] = l.Name
	}
	return s.build(ctx, entries, names, cfg, at)
}

func (s *ServiceStatusCatalog) enabledEntries(ctx context.Context) ([]*domain.Entry, error) {
	list, err := s.Store.ListEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := []*domain.Entry{}
	for _, e := range list {
		if !e.Enabled {
			continue
		}
		ib, err := s.Store.InboundByID(ctx, e.InboundID)
		if err != nil {
			return nil, err
		}
		if ib.Enabled {
			out = append(out, e)
		}
	}
	return out, nil
}

func availableStatusEntries(entries []*domain.Entry) []*domain.Entry {
	lines := make([]PublishedLine, 0, len(entries))
	byID := make(map[int64]*domain.Entry, len(entries))
	for _, e := range entries {
		l := PublishedLine{EntryID: e.ID}
		if e.ClientDialerEntryID != nil {
			l.DialerEntryID = *e.ClientDialerEntryID
		}
		lines = append(lines, l)
		byID[e.ID] = e
	}
	out := make([]*domain.Entry, 0, len(entries))
	for _, l := range AvailablePublications(lines) {
		out = append(out, byID[l.EntryID])
	}
	return out
}

func (s *ServiceStatusCatalog) build(ctx context.Context, entries []*domain.Entry, names map[int64]string, cfg store.ServiceStatusConfig, at time.Time) (ServiceStatusView, error) {
	out := ServiceStatusView{GeneratedAt: at.Unix(), RefreshSeconds: 60, Entries: []ServiceStatusEntry{}}
	runtime, err := s.Runtime.Evaluate(ctx, entries, at)
	if err != nil {
		return out, err
	}
	categories := map[int64]*ServiceStatusCategory{}
	for _, e := range entries {
		if e.CategoryID != nil {
			categories[*e.CategoryID] = nil
		}
	}
	if len(categories) > 0 {
		all, err := s.Store.ListEntryCategories(ctx)
		if err != nil {
			return out, err
		}
		position := 0
		for _, c := range all {
			if _, ok := categories[c.ID]; ok {
				categories[c.ID] = &ServiceStatusCategory{ID: c.ID, Name: c.Name, Description: c.Description, Sort: position}
				position++
			}
		}
	}
	maintenance := map[int64]*ServiceStatusMaintenance{}
	for _, m := range cfg.Maintenance {
		if m.StartsAt <= at.Unix() && at.Unix() < m.EndsAt {
			maintenance[m.EntryID] = &ServiceStatusMaintenance{StartsAt: m.StartsAt, EndsAt: m.EndsAt, Note: m.Note}
		}
	}
	for _, e := range entries {
		r, ok := runtime[e.ID]
		if !ok {
			r = EntryRuntimeStatus{State: "unknown", Reason: "no_data"}
		}
		if r.Evidence == nil {
			r.Evidence = []string{}
		}
		// No runtime observation in this version is an end-to-end service test.
		r.EndToEnd = false
		v := ServiceStatusEntry{ID: e.ID, Name: e.Name, Rate: e.Rate, Region: e.Region, EntryRuntimeStatus: r, Maintenance: maintenance[e.ID]}
		if name, ok := names[e.ID]; ok {
			v.Name = name
		}
		if e.CategoryID != nil {
			v.Category = categories[*e.CategoryID]
		}
		if v.Maintenance != nil {
			v.State = "maintenance"
			v.Reason = "maintenance"
		}
		switch v.State {
		case "healthy":
			out.Summary.Healthy++
		case "degraded":
			out.Summary.Degraded++
		case "down":
			out.Summary.Down++
		case "maintenance":
			out.Summary.Maintenance++
		default:
			v.State = "unknown"
			out.Summary.Unknown++
		}
		out.Entries = append(out.Entries, v)
	}
	out.Summary.Total = len(out.Entries)
	return out, nil
}
