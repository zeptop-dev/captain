package probetheme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/zeptop-dev/captain/internal/store"
)

type Manager struct {
	Store  *store.Store
	mu     sync.Mutex
	cached *Package
}

func New(st *store.Store) *Manager { return &Manager{Store: st} }

type Catalog struct {
	Packages []store.ProbeThemePackage `json:"packages"`
	State    store.ProbeThemeState     `json:"state"`
}

func (m *Manager) List(ctx context.Context) (Catalog, error) {
	p, s, e := m.Store.ProbeThemes(ctx)
	return Catalog{p, s}, e
}
func (m *Manager) Install(ctx context.Context, data []byte) (store.ProbeThemePackage, error) {
	p, err := Parse(data)
	if err != nil {
		return store.ProbeThemePackage{}, err
	}
	manifest, _ := json.Marshal(p.Manifest)
	row := store.ProbeThemePackage{Digest: p.Digest, Short: p.Manifest.Short, Version: p.Manifest.Version, Manifest: manifest}
	err = m.Store.InstallProbeTheme(ctx, row, data)
	return row, err
}

// Active returns nil when the administrator selected Captain's built-in theme.
func (m *Manager) Active(ctx context.Context) (*Package, error) {
	st, err := m.Store.ProbeThemeState(ctx)
	if err != nil || st.Active == "" {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cached != nil && m.cached.Digest == st.Active {
		return m.cached, nil
	}
	b, err := m.Store.ProbeThemeArchive(ctx, st.Active)
	if err != nil {
		return nil, err
	}
	p, err := Parse(b)
	if err != nil {
		return nil, err
	}
	m.cached = p
	return p, nil
}
func (m *Manager) Activate(ctx context.Context, digest string, revision int64) error {
	if digest != "" {
		b, err := m.Store.ProbeThemeArchive(ctx, digest)
		if err != nil {
			return err
		}
		if _, err = Parse(b); err != nil {
			return err
		}
	}
	return m.Store.ActivateProbeTheme(ctx, digest, revision)
}
func (m *Manager) Remove(ctx context.Context, digest string) error {
	return m.Store.RemoveProbeTheme(ctx, digest)
}
func (m *Manager) SaveSettings(ctx context.Context, digest string, revision int64, settings map[string]any) error {
	rows, _, err := m.Store.ProbeThemes(ctx)
	if err != nil {
		return err
	}
	for _, p := range rows {
		if p.Digest != digest {
			continue
		}
		var manifest Manifest
		if err = json.Unmarshal(p.Manifest, &manifest); err != nil {
			return err
		}
		normalized, err := manifest.Settings(settings)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(normalized)
		return m.Store.SaveProbeThemeSettings(ctx, digest, revision, b)
	}
	return store.ErrNotFound
}

// PublicSettings is the Komari theme short name and public dynamic settings.
// These values are intentionally not secrets and never contain admin sessions.
func (m *Manager) PublicSettings(ctx context.Context) (string, map[string]any, error) {
	rows, state, err := m.Store.ProbeThemes(ctx)
	if err != nil {
		return "", nil, err
	}
	if state.Active == "" {
		return "default", map[string]any{}, nil
	}
	for _, p := range rows {
		if p.Digest != state.Active {
			continue
		}
		var manifest Manifest
		var settings map[string]any
		if err = json.Unmarshal(p.Manifest, &manifest); err != nil {
			return "", nil, err
		}
		if err = json.Unmarshal(p.Settings, &settings); err != nil {
			return "", nil, err
		}
		out, err := manifest.Settings(settings)
		return p.Short, out, err
	}
	return "", nil, fmt.Errorf("active theme is unavailable")
}
func IsConflict(err error) bool { return errors.Is(err, store.ErrProbeThemeConflict) }

// SelectorOptions contains only publicly eligible identities, never private labels.
type SelectorOptions struct {
	Nodes     []SelectorOption `json:"nodes"`
	PingTasks []SelectorOption `json:"pingtasks"`
}
type SelectorOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
