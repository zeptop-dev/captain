package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/captain/internal/branding"
	"github.com/zeptop-dev/captain/internal/domain"

	"github.com/zeptop-dev/captain/internal/store"
)

func (h *handlers) getProbe(w http.ResponseWriter, r *http.Request) {
	var v store.ProbeSettings
	_ = h.Store.GetSetting(r.Context(), store.SettingProbe, &v)
	v.Normalize()
	if v.Hosts == nil {
		v.Hosts = []string{}
	}
	ok(w, v)
}

func (h *handlers) putProbe(w http.ResponseWriter, r *http.Request) {
	h.Store.Topology.Lock()
	defer h.Store.Topology.Unlock()
	var in struct {
		store.ProbeSettings
		Title    *string `json:"title"`
		Logo     *string `json:"logo"`
		LogoDark *string `json:"logo_dark"`
		Favicon  *string `json:"favicon"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	v := in.ProbeSettings
	v.Path = strings.TrimSpace(v.Path)
	if v.Path != "" {
		v.Path = "/" + strings.Trim(v.Path, "/")
		for _, reserved := range []string{"/admin", "/portal", "/sub", "/api", "/assets", "/brand-assets"} {
			if v.Path == reserved || strings.HasPrefix(v.Path, reserved+"/") {
				fail(w, http.StatusBadRequest, "that path is reserved")
				return
			}
		}
	}
	hosts := []string{}
	for _, hh := range v.Hosts {
		if hh = strings.ToLower(strings.TrimSpace(hh)); hh != "" {
			hosts = append(hosts, hh)
		}
	}
	v.Hosts = hosts
	var old store.ProbeSettings
	if err := h.Store.GetSetting(r.Context(), store.SettingProbe, &old); err != nil && !errors.Is(err, store.ErrNotFound) {
		serverErr(w, err)
		return
	}
	if v.FollowSiteBranding == nil {
		v.FollowSiteBranding = old.FollowSiteBranding
	}
	// Hiding the independent fields while inheritance is on must not erase
	// them, including when a client omits those disabled form controls.
	v.Title, v.Logo = old.Title, old.Logo
	if in.Title != nil {
		v.Title = *in.Title
	}
	if in.Logo != nil {
		v.Logo = *in.Logo
	}
	v.LogoDark, v.Favicon = old.LogoDark, old.Favicon
	if in.LogoDark != nil {
		v.LogoDark = *in.LogoDark
	}
	if in.Favicon != nil {
		v.Favicon = *in.Favicon
	}
	for _, value := range []string{v.LogoDark, v.Favicon} {
		if !branding.ValidAssetURL(value) {
			fail(w, http.StatusBadRequest, "invalid probe branding image URL")
			return
		}
	}
	legacyOnly := old.CarrierProbe == nil
	old.Normalize()
	if v.CarrierProbe == nil {
		v.CarrierProbe = old.CarrierProbe.Clone()
		if v.Carriers != nil {
			// An explicitly empty legacy list restores TCP IPv4 defaults;
			// omission/null leaves all four lists and mode selection untouched.
			carriers := []spec.Carrier{}
			for _, c := range v.Carriers {
				c.Name, c.Addr = strings.TrimSpace(c.Name), strings.TrimSpace(c.Addr)
				if c.Name != "" || c.Addr != "" {
					carriers = append(carriers, c)
				}
			}
			legacy := spec.LegacyCarrierProbe(carriers)
			if legacyOnly {
				// The old list described every carrier target, rather than
				// one of four independent drafts. Preserve replacement and
				// clearing semantics on the first legacy update.
				v.CarrierProbe = &legacy
			}
			v.CarrierProbe.TCPIPv4 = legacy.TCPIPv4
			if len(legacy.TCPIPv6) > 0 {
				// IPv6 literals were valid in the old single list. Keep them
				// usable without ever probing them as IPv4.
				v.CarrierProbe.TCPIPv6 = legacy.TCPIPv6
				if v.CarrierProbe.Protocol == "tcp" {
					v.CarrierProbe.Family = legacy.Family
				}
			}
		}
	}
	if err := v.CarrierProbe.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if v.FinanceCurrency == nil {
		v.FinanceCurrency = old.FinanceCurrency
	}
	if v.FinanceCurrency != nil {
		normalized := strings.ToUpper(*v.FinanceCurrency)
		if !store.ValidCurrency(normalized) {
			fail(w, http.StatusBadRequest, "invalid finance currency")
			return
		}
		v.FinanceCurrency = &normalized
	}
	if v.Appearance == nil {
		v.Appearance = old.Appearance
	}
	if v.Appearance != nil {
		v.Appearance.Normalize()
		if !v.Appearance.Valid() {
			fail(w, 400, "invalid page appearance")
			return
		}
	}
	if v.PageEnabled == nil {
		v.PageEnabled = old.PageEnabled
	}
	if v.PublicSections == nil {
		v.PublicSections = old.PublicSections
	}
	if v.Layout == "" {
		v.Layout = old.Layout
	}
	if v.Layout != "" && v.Layout != "grid" && v.Layout != "compact" {
		fail(w, 400, "invalid page layout")
		return
	}
	if v.PublicSections != nil {
		if len(*v.PublicSections) > 10 {
			fail(w, 400, "invalid public sections")
			return
		}
		for _, section := range *v.PublicSections {
			switch section {
			case "cpu", "memory", "disk", "network", "system", "info", "traffic", "latency", "history", "availability":
			default:
				fail(w, 400, "invalid public section")
				return
			}
		}
	}
	v.Normalize()
	if v.PageOn() && v.Path == store.ServiceStatusPath {
		statusConfig, err := h.Store.ServiceStatusConfig(r.Context())
		if err != nil {
			serverErr(w, err)
			return
		}
		if statusConfig.PublicEnabled {
			fail(w, 409, store.ErrServiceStatusPathConflict.Error())
			return
		}
	}
	if err := h.Store.SetSetting(r.Context(), store.SettingProbe, v); err != nil {
		serverErr(w, err)
		return
	}
	if h.Probe != nil {
		h.Probe.Invalidate()
		if !v.Enabled {
			if err := h.Probe.MonitoringDisabled(r.Context(), time.Now()); err != nil {
				serverErr(w, err)
				return
			}
		}
	}
	if h.SubLinks != nil {
		h.SubLinks.Invalidate()
	}
	ok(w, v)
}

func (h *handlers) listPingTasks(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListPingTasks(r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	ok(w, list)
}

func (h *handlers) savePingTask(w http.ResponseWriter, r *http.Request) {
	var t store.PingTask
	if !decode(r, &t) || strings.TrimSpace(t.Name) == "" || strings.TrimSpace(t.Target) == "" {
		fail(w, http.StatusBadRequest, "name and target are required")
		return
	}
	switch t.Type {
	case "icmp", "tcp", "http", "download":
	default:
		fail(w, http.StatusBadRequest, "type must be icmp, tcp, http or download")
		return
	}
	if t.Type == "download" && t.IntervalSeconds < 600 {
		t.IntervalSeconds = 600
	}
	if t.IntervalSeconds < 5 {
		t.IntervalSeconds = 30
	}
	if err := (spec.PingTask{Name: t.Name, Type: t.Type, Target: t.Target}).Validate(); err != nil {
		fail(w, 400, err.Error())
		return
	}
	t.ID = idOf(r)
	if err := h.Store.SavePingTask(r.Context(), &t); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, t)
}

func (h *handlers) deletePingTask(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeletePingTask(r.Context(), idOf(r)); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) getNodeProbe(w http.ResponseWriter, r *http.Request) {
	np, revision, linked, supplierLinked, err := h.Store.NodeProbeInfrastructure(r.Context(), idOf(r))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "node not found")
		} else {
			serverErr(w, err)
		}
		return
	}
	out := map[string]any{"probe": np, "billed": np.Billed(), "region_countries": store.RegionCountries(), "infrastructure_revision": revision, "infrastructure_linked": linked, "infrastructure_supplier_linked": supplierLinked}
	var host *spec.SystemStatus
	var received time.Time
	monitored, err := h.Store.MonitorNodes(r.Context(), time.Now(), 3*time.Minute, np.NodeID)
	if err != nil {
		serverErr(w, err)
		return
	}
	if len(monitored) > 0 && monitored[0].Host != nil {
		host, received = monitored[0].Host, time.Unix(monitored[0].SampledAt, 0)
	}
	if h.Probe != nil {
		if l, ok := h.Probe.Live(np.NodeID); ok {
			out["live"] = l
			if !l.At.Before(received) {
				host, received = &l.Host, l.At
			}
		}
	}
	out["storage_candidates"] = store.StorageCandidates(host, received, time.Now())
	ok(w, out)
}

func (h *handlers) putNodeProbe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExpectedExpiresAt      *string
		InfrastructureRevision *string
		SupplierID             *int64
		RegionMode             *string
		Hidden                 bool
		Info                   store.NodeProbeInfo
		LimitBytes             int64
		ResetDay               int
		Mode                   string
		Resources              *spec.ResourceOptions
		Storage                *store.NodeStorage
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Resources != nil {
		if err := in.Resources.Validate(); err != nil {
			fail(w, http.StatusBadRequest, "invalid network interface selection")
			return
		}
	}
	guard := store.NodeInfrastructureGuard{Revision: in.InfrastructureRevision, SupplierID: in.SupplierID, CanWrite: h.infrastructureWriteAllowed(r, idOf(r))}
	if err := h.Store.UpdateNodeProbeWithInfrastructure(r.Context(), idOf(r), in.Hidden, in.Info, in.LimitBytes, in.ResetDay, in.Mode, in.Resources, in.RegionMode, in.ExpectedExpiresAt, guard, in.Storage); err != nil {
		if errors.Is(err, store.ErrInfrastructureConflict) || errors.Is(err, store.ErrInfrastructurePermission) || errors.Is(err, store.ErrInfrastructureConversion) || errors.Is(err, store.ErrInfrastructureLinked) || errors.Is(err, store.ErrInfrastructureConfirmation) {
			infraError(w, err)
			return
		}
		if errors.Is(err, store.ErrNodeExpiryConflict) {
			fail(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, domain.ErrGeoLocation) || errors.Is(err, domain.ErrLocationSettings) || errors.Is(err, store.ErrNodeRegion) || errors.Is(err, store.ErrNodeCost) || errors.Is(err, store.ErrNodeBandwidth) || errors.Is(err, store.ErrNodeStorage) {
			fail(w, 400, err.Error())
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			fail(w, 404, "node not found")
			return
		}
		serverErr(w, err)
		return
	}
	if h.Probe != nil {
		h.Probe.Invalidate()
	}
	h.getNodeProbe(w, r)
}

func (h *handlers) resetNodeTraffic(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.ResetNodeTraffic(r.Context(), idOf(r), time.Now()); err != nil {
		serverErr(w, err)
		return
	}
	_ = h.Store.ClearAlert(r.Context(), idOf(r), "traffic80")
	_ = h.Store.ClearAlert(r.Context(), idOf(r), "traffic100")
	h.getNodeProbe(w, r)
}
