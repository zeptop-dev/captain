package admin

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zeptop-dev/bosun/pkg/selfupdate"

	"github.com/zeptop-dev/captain/internal/backup"
)

// Panel self-check: what bosun's doctor is to a node, for Captain itself.
// Each check names a gap an operator otherwise finds only when it bites —
// backups that never leave the host, nothing watching the panel, staff
// without a second factor, a filling disk, an old build. The console
// translates the id and code; args carry the numbers and names.
//
// Under /api/admin/system/, so only admins see it (it lists staff emails).

type selfCheck struct {
	ID     string         `json:"id"`
	Status string         `json:"status"` // ok, warn, fail, skip
	Code   string         `json:"code,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
}

const (
	backupStaleAfter = 26 * time.Hour
	nodeOfflineAfter = 5 * time.Minute
	diskWarnBytes    = 1 << 30
	diskFailBytes    = 256 << 20
	certWarnDays     = 14
)

func (h *handlers) selfCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	checks := []func(context.Context) []selfCheck{
		h.checkBackups, h.checkHeartbeat, h.checkMail, h.checkStaff2FA,
		h.checkVersion, h.checkDisk, h.checkNodes, h.checkPanelCert,
	}
	var mu sync.Mutex
	var out []selfCheck
	var wg sync.WaitGroup
	for _, c := range checks {
		wg.Add(1)
		go func(c func(context.Context) []selfCheck) {
			defer wg.Done()
			res := c(ctx)
			mu.Lock()
			out = append(out, res...)
			mu.Unlock()
		}(c)
	}
	wg.Wait()
	order := map[string]int{}
	for i, id := range []string{"backup_local", "backup_remote", "backup_encrypt", "heartbeat", "mail", "staff_2fa", "version", "disk", "nodes", "panel_cert"} {
		order[id] = i
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i].ID] < order[out[j].ID] })
	ok(w, map[string]any{"checks": out, "at": time.Now()})
}

func (h *handlers) checkBackups(ctx context.Context) []selfCheck {
	if h.Backups == nil || h.Backups.Dir == "" {
		return []selfCheck{{ID: "backup_local", Status: "skip"}, {ID: "backup_remote", Status: "skip"}, {ID: "backup_encrypt", Status: "skip"}}
	}
	var s backup.Settings
	var st backup.Status
	_ = h.Store.GetSetting(ctx, backup.SettingKey, &s)
	_ = h.Store.GetSetting(ctx, backup.StatusKey, &st)
	local := selfCheck{ID: "backup_local", Status: "ok", Args: map[string]any{"at": st.LastAt}}
	switch {
	case st.LastError != "":
		local.Status, local.Code, local.Args = "fail", "error", map[string]any{"error": st.LastError}
	case st.LastAt.IsZero():
		local.Status, local.Code = "warn", "never"
	case time.Since(st.LastAt) > backupStaleAfter:
		local.Status, local.Code = "warn", "stale"
	}
	remote := selfCheck{ID: "backup_remote", Status: "ok", Args: map[string]any{"name": st.RemoteName, "at": st.RemoteAt}}
	enc := selfCheck{ID: "backup_encrypt", Status: "ok"}
	switch {
	case s.Remote == "":
		remote.Status, remote.Code, remote.Args = "warn", "none", nil
		enc.Status = "skip"
	case st.RemoteErr != "":
		remote.Status, remote.Code, remote.Args = "fail", "error", map[string]any{"error": st.RemoteErr}
	}
	if s.Remote != "" && !s.Encrypt.Enabled() {
		enc.Status, enc.Code = "warn", "off"
	}
	return []selfCheck{local, remote, enc}
}

func (h *handlers) checkHeartbeat(ctx context.Context) []selfCheck {
	c := selfCheck{ID: "heartbeat", Status: "ok"}
	if h.Heartbeat == nil {
		c.Status = "skip"
		return []selfCheck{c}
	}
	if !h.Heartbeat.Settings(ctx).Enabled {
		c.Status, c.Code = "warn", "off"
		return []selfCheck{c}
	}
	if st := h.Heartbeat.Status(); st.Error != "" {
		c.Status, c.Code, c.Args = "fail", "failing", map[string]any{"error": st.Error}
	} else if !st.At.IsZero() {
		c.Args = map[string]any{"at": st.At}
	}
	return []selfCheck{c}
}

func (h *handlers) checkMail(ctx context.Context) []selfCheck {
	c := selfCheck{ID: "mail", Status: "ok"}
	if h.Mail == nil {
		c.Status = "skip"
	} else if p := h.Mail.Settings(ctx).Provider; p == "" {
		c.Status, c.Code = "warn", "off"
	} else {
		c.Args = map[string]any{"provider": p}
	}
	return []selfCheck{c}
}

func (h *handlers) checkStaff2FA(ctx context.Context) []selfCheck {
	c := selfCheck{ID: "staff_2fa", Status: "ok"}
	staff, err := h.Store.ListStaff(ctx)
	if err != nil {
		c.Status = "skip"
		return []selfCheck{c}
	}
	var without []string
	for _, u := range staff {
		if _, on, err := h.Store.TOTP(ctx, u.ID); err == nil && !on {
			without = append(without, u.Email)
		}
	}
	if len(without) > 0 {
		c.Status, c.Code, c.Args = "warn", "missing", map[string]any{"count": len(without), "emails": strings.Join(without, ", ")}
	}
	return []selfCheck{c}
}

func (h *handlers) checkVersion(ctx context.Context) []selfCheck {
	c := selfCheck{ID: "version", Status: "ok"}
	if h.Updater == nil || !h.Updater.ReleaseBuild() {
		c.Status = "skip" // a dev build is never offered updates
		return []selfCheck{c}
	}
	info := h.Updater.Check(ctx, false)
	if info.HasUpdate {
		c.Status, c.Code = "warn", "behind"
	}
	c.Args = map[string]any{"current": info.Current, "latest": info.Latest}
	return []selfCheck{c}
}

func (h *handlers) checkDisk(ctx context.Context) []selfCheck {
	c := selfCheck{ID: "disk", Status: "ok"}
	if h.Backups == nil || h.Backups.Dir == "" {
		c.Status = "skip"
		return []selfCheck{c}
	}
	free, err := selfupdate.FreeSpace(h.Backups.Dir)
	if err != nil {
		c.Status = "skip"
		return []selfCheck{c}
	}
	c.Args = map[string]any{"free": free}
	switch {
	case free < diskFailBytes:
		c.Status, c.Code = "fail", "low"
	case free < diskWarnBytes:
		c.Status, c.Code = "warn", "low"
	}
	return []selfCheck{c}
}

func (h *handlers) checkNodes(ctx context.Context) []selfCheck {
	c := selfCheck{ID: "nodes", Status: "ok"}
	nodes, err := h.Store.ListNodes(ctx)
	if err != nil || len(nodes) == 0 {
		c.Status = "skip"
		return []selfCheck{c}
	}
	var off []string
	for _, n := range nodes {
		if n.LastSeenAt != nil && time.Since(*n.LastSeenAt) > nodeOfflineAfter {
			off = append(off, n.Name)
		}
	}
	c.Args = map[string]any{"count": len(nodes)}
	if len(off) > 0 {
		c.Status, c.Code, c.Args = "warn", "offline", map[string]any{"count": len(off), "names": strings.Join(off, ", ")}
	}
	return []selfCheck{c}
}

// checkPanelCert reads the certificate the panel's public address serves,
// whoever terminates TLS (Captain itself or a reverse proxy in front). A
// host that cannot reach its own public name is skipped, not failed.
func (h *handlers) checkPanelCert(ctx context.Context) []selfCheck {
	c := selfCheck{ID: "panel_cert", Status: "ok"}
	u, err := url.Parse(h.BaseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		c.Status = "skip"
		return []selfCheck{c}
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	d := tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: &tls.Config{ServerName: u.Hostname(), InsecureSkipVerify: true}} //nolint:gosec // reading expiry only
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		c.Status, c.Args = "skip", map[string]any{"error": err.Error()}
		return []selfCheck{c}
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		c.Status = "skip"
		return []selfCheck{c}
	}
	days := int(time.Until(certs[0].NotAfter).Hours() / 24)
	c.Args = map[string]any{"days": days, "not_after": certs[0].NotAfter}
	switch {
	case days < 0:
		c.Status, c.Code = "fail", "expired"
	case days < certWarnDays:
		c.Status, c.Code = "warn", "expiring"
	}
	return []selfCheck{c}
}
