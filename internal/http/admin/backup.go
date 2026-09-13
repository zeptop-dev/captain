package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/backup"
)

// Backups: settings (secrets masked), status, local list, run now, test
// remote, download. Admin role only via the settings path prefix.

func (h *handlers) registerBackup(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/settings/backup", h.requireAdmin(h.getBackup))
	mux.HandleFunc("PUT /api/admin/settings/backup", h.requireAdmin(h.putBackup))
	mux.HandleFunc("POST /api/admin/settings/backup/run", h.requireAdmin(h.runBackup))
	mux.HandleFunc("POST /api/admin/settings/backup/test", h.requireAdmin(h.testBackup))
	mux.HandleFunc("GET /api/admin/settings/backup/files/{name}", h.requireAdmin(h.downloadBackup))
}

func (h *handlers) getBackup(w http.ResponseWriter, r *http.Request) {
	if h.Backups == nil {
		ok(w, map[string]any{"available": false})
		return
	}
	var s backup.Settings
	_ = h.Store.GetSetting(r.Context(), backup.SettingKey, &s)
	var st backup.Status
	_ = h.Store.GetSetting(r.Context(), backup.StatusKey, &st)
	files, _ := h.Backups.List()
	hasDav, hasS3 := s.WebDAV.Password != "", s.S3.SecretKey != ""
	s.WebDAV.Password, s.S3.SecretKey = "", ""
	ok(w, map[string]any{"available": true, "settings": s, "has_webdav_password": hasDav, "has_s3_secret": hasS3, "status": st, "files": files, "dir": h.Backups.Dir})
}

// putBackup stores settings; blank secrets keep the stored ones.
func (h *handlers) putBackup(w http.ResponseWriter, r *http.Request) {
	var in backup.Settings
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var cur backup.Settings
	_ = h.Store.GetSetting(r.Context(), backup.SettingKey, &cur)
	if in.WebDAV.Password == "" {
		in.WebDAV.Password = cur.WebDAV.Password
	}
	if in.S3.SecretKey == "" {
		in.S3.SecretKey = cur.S3.SecretKey
	}
	in.WebDAV.URL = strings.TrimSpace(in.WebDAV.URL)
	in.S3.Endpoint = strings.TrimSpace(in.S3.Endpoint)
	switch in.Remote {
	case "":
	case "webdav":
		if !strings.HasPrefix(in.WebDAV.URL, "http://") && !strings.HasPrefix(in.WebDAV.URL, "https://") {
			fail(w, http.StatusBadRequest, "WebDAV URL must start with http:// or https://")
			return
		}
	case "s3":
		if !strings.HasPrefix(in.S3.Endpoint, "http://") && !strings.HasPrefix(in.S3.Endpoint, "https://") || in.S3.Bucket == "" || in.S3.AccessKey == "" || in.S3.SecretKey == "" {
			fail(w, http.StatusBadRequest, "S3 needs endpoint, bucket, access key and secret")
			return
		}
		if in.S3.Region == "" {
			in.S3.Region = "auto"
		}
	default:
		fail(w, http.StatusBadRequest, "remote must be empty, webdav or s3")
		return
	}
	if in.Hour < 0 || in.Hour > 23 {
		fail(w, http.StatusBadRequest, "hour must be 0-23")
		return
	}
	if err := h.Store.SetSetting(r.Context(), backup.SettingKey, in); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) runBackup(w http.ResponseWriter, r *http.Request) {
	if h.Backups == nil {
		fail(w, http.StatusNotFound, "backups not available")
		return
	}
	name, err := h.Backups.Run(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]any{"file": name})
}

// testBackup tries the remote with the submitted settings (blank secrets
// fall back to the stored ones).
func (h *handlers) testBackup(w http.ResponseWriter, r *http.Request) {
	var in backup.Settings
	if !decode(r, &in) {
		fail(w, http.StatusBadRequest, "bad json")
		return
	}
	var cur backup.Settings
	_ = h.Store.GetSetting(r.Context(), backup.SettingKey, &cur)
	if in.WebDAV.Password == "" {
		in.WebDAV.Password = cur.WebDAV.Password
	}
	if in.S3.SecretKey == "" {
		in.S3.SecretKey = cur.S3.SecretKey
	}
	if err := backup.Test(r.Context(), in, &http.Client{Timeout: 30 * time.Second}); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	ok(w, map[string]bool{"ok": true})
}

func (h *handlers) downloadBackup(w http.ResponseWriter, r *http.Request) {
	if h.Backups == nil {
		http.NotFound(w, r)
		return
	}
	p, err := h.Backups.Path(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename="+r.PathValue("name"))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, p)
}
