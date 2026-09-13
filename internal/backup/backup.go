// Package backup snapshots the SQLite database daily, keeps a few copies
// locally and can push each one to WebDAV or an S3-compatible bucket.
package backup

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/store"
)

const SettingKey = "backup"

// Settings is the admin-editable configuration.
type Settings struct {
	Keep   int    `json:"keep"`   // local copies to keep; 0 = 7
	Hour   int    `json:"hour"`   // local hour of day for the daily snapshot
	Remote string `json:"remote"` // "", "webdav", "s3"
	WebDAV struct {
		URL      string `json:"url"` // directory URL, e.g. https://dav.example.com/captain/
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"webdav"`
	S3 struct {
		Endpoint  string `json:"endpoint"` // https://s3.amazonaws.com or a compatible host
		Region    string `json:"region"`
		Bucket    string `json:"bucket"`
		Prefix    string `json:"prefix"`
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
		PathStyle bool   `json:"path_style"` // bucket in the path (MinIO, some R2/B2 setups)
	} `json:"s3"`
	RemoteKeep int `json:"remote_keep"` // remote copies to keep; 0 = keep all
}

// Status is the last outcome, kept in the settings table for the console.
type Status struct {
	LastAt     time.Time `json:"last_at"`
	LastFile   string    `json:"last_file"`
	LastError  string    `json:"last_error"`
	RemoteAt   time.Time `json:"remote_at"`
	RemoteName string    `json:"remote_name"`
	RemoteErr  string    `json:"remote_error"`
}

const StatusKey = "backup_status"

// Manager runs snapshots.
type Manager struct {
	Store *store.Store
	Dir   string
	Log   *slog.Logger
	// Client is used for uploads; nil = http.DefaultClient with a timeout.
	Client *http.Client
	// Now is for tests.
	Now func() time.Time

	lastDay string
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) settings(ctx context.Context) Settings {
	var s Settings
	_ = m.Store.GetSetting(ctx, SettingKey, &s)
	return s
}

// Tick takes today's snapshot once the configured hour has passed.
func (m *Manager) Tick(ctx context.Context) {
	if m.Dir == "" {
		return
	}
	s := m.settings(ctx)
	now := m.now()
	day := now.Format("2006-01-02")
	if m.lastDay == day || now.Hour() < s.Hour {
		return
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "captain-"+day+".db")); err == nil {
		m.lastDay = day
		return
	}
	m.lastDay = day
	if _, err := m.Run(ctx); err != nil && m.Log != nil {
		m.Log.Error("backup failed", "component", "backup", "err", err)
	}
}

// Run snapshots now, prunes old copies, uploads when a remote is set and
// records the outcome. Returns the local file name.
func (m *Manager) Run(ctx context.Context) (string, error) {
	s := m.settings(ctx)
	st := Status{}
	_ = m.Store.GetSetting(ctx, StatusKey, &st)
	now := m.now()
	name, err := m.snapshot(ctx, now, s.Keep)
	st.LastAt = now
	if err != nil {
		st.LastError = err.Error()
		_ = m.Store.SetSetting(ctx, StatusKey, st)
		return "", err
	}
	st.LastFile, st.LastError = filepath.Base(name), ""
	if s.Remote != "" {
		remoteName, uerr := m.upload(ctx, s, name)
		st.RemoteAt = now
		if uerr != nil {
			st.RemoteErr = uerr.Error()
			_ = m.Store.SetSetting(ctx, StatusKey, st)
			return name, fmt.Errorf("upload: %w", uerr)
		}
		st.RemoteName, st.RemoteErr = remoteName, ""
	}
	_ = m.Store.SetSetting(ctx, StatusKey, st)
	if m.Log != nil {
		m.Log.Info("database backed up", "component", "backup", "file", filepath.Base(name), "remote", s.Remote)
	}
	return name, nil
}

// snapshot writes captain-<date>[-<time>].db and prunes to keep copies.
func (m *Manager) snapshot(ctx context.Context, now time.Time, keep int) (string, error) {
	if keep <= 0 {
		keep = 7
	}
	if err := os.MkdirAll(m.Dir, 0o750); err != nil {
		return "", err
	}
	name := filepath.Join(m.Dir, "captain-"+now.Format("2006-01-02")+".db")
	if _, err := os.Stat(name); err == nil {
		name = filepath.Join(m.Dir, "captain-"+now.Format("2006-01-02-150405")+".db")
	}
	if err := m.Store.Backup(ctx, name); err != nil {
		return "", err
	}
	files, _ := filepath.Glob(filepath.Join(m.Dir, "captain-*.db"))
	sort.Strings(files)
	for len(files) > keep {
		_ = os.Remove(files[0])
		files = files[1:]
	}
	return name, nil
}

// List returns local backups, newest first.
func (m *Manager) List() ([]File, error) {
	files, err := filepath.Glob(filepath.Join(m.Dir, "captain-*.db"))
	if err != nil {
		return nil, err
	}
	out := []File{}
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			out = append(out, File{Name: filepath.Base(f), Size: fi.Size(), ModTime: fi.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// File is one local backup.
type File struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// Path resolves a listed name to a file, refusing anything outside Dir.
func (m *Manager) Path(name string) (string, error) {
	if name != filepath.Base(name) || !strings.HasPrefix(name, "captain-") || !strings.HasSuffix(name, ".db") {
		return "", errors.New("bad backup name")
	}
	p := filepath.Join(m.Dir, name)
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

func (m *Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// upload gzips the snapshot and sends it; returns the remote object name.
func (m *Manager) upload(ctx context.Context, s Settings, local string) (string, error) {
	gz, size, err := gzipFile(local)
	if err != nil {
		return "", err
	}
	defer os.Remove(gz)
	name := filepath.Base(local) + ".gz"
	var r Remote
	switch s.Remote {
	case "webdav":
		r = &WebDAV{URL: s.WebDAV.URL, Username: s.WebDAV.Username, Password: s.WebDAV.Password, Client: m.client()}
	case "s3":
		r = &S3{Endpoint: s.S3.Endpoint, Region: s.S3.Region, Bucket: s.S3.Bucket, Prefix: s.S3.Prefix, AccessKey: s.S3.AccessKey, SecretKey: s.S3.SecretKey, PathStyle: s.S3.PathStyle, Client: m.client()}
	default:
		return "", fmt.Errorf("unknown remote %q", s.Remote)
	}
	f, err := os.Open(gz)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := r.Put(ctx, name, f, size); err != nil {
		return "", err
	}
	if s.RemoteKeep > 0 {
		if names, err := r.List(ctx); err == nil {
			sort.Strings(names)
			for len(names) > s.RemoteKeep {
				_ = r.Delete(ctx, names[0])
				names = names[1:]
			}
		}
	}
	return name, nil
}

// Remote is a place backups go.
type Remote interface {
	Put(ctx context.Context, name string, body io.Reader, size int64) error
	List(ctx context.Context) ([]string, error)
	Delete(ctx context.Context, name string) error
}

// Test checks credentials by writing and deleting a tiny object.
func Test(ctx context.Context, s Settings, client *http.Client) error {
	m := &Manager{Client: client}
	var r Remote
	switch s.Remote {
	case "webdav":
		r = &WebDAV{URL: s.WebDAV.URL, Username: s.WebDAV.Username, Password: s.WebDAV.Password, Client: m.client()}
	case "s3":
		r = &S3{Endpoint: s.S3.Endpoint, Region: s.S3.Region, Bucket: s.S3.Bucket, Prefix: s.S3.Prefix, AccessKey: s.S3.AccessKey, SecretKey: s.S3.SecretKey, PathStyle: s.S3.PathStyle, Client: m.client()}
	default:
		return errors.New("no remote configured")
	}
	name := "captain-test-" + time.Now().Format("20060102150405") + ".txt"
	if err := r.Put(ctx, name, strings.NewReader("ok"), 2); err != nil {
		return err
	}
	return r.Delete(ctx, name)
}

func gzipFile(src string) (string, int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(src), ".upload-*.gz")
	if err != nil {
		return "", 0, err
	}
	zw := gzip.NewWriter(out)
	if _, err := io.Copy(zw, in); err != nil {
		out.Close()
		os.Remove(out.Name())
		return "", 0, err
	}
	if err := zw.Close(); err != nil {
		out.Close()
		os.Remove(out.Name())
		return "", 0, err
	}
	fi, err := out.Stat()
	out.Close()
	if err != nil {
		os.Remove(out.Name())
		return "", 0, err
	}
	return out.Name(), fi.Size(), nil
}

func joinURL(base, name string) string {
	return strings.TrimRight(base, "/") + "/" + path.Base(name)
}
