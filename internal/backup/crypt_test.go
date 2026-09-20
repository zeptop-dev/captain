package backup

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// dbFile writes a stand-in database and returns its path and content.
func dbFile(t *testing.T) (string, []byte) {
	t.Helper()
	content := bytes.Repeat([]byte("SQLite format 3\x00 captain rows "), 4096)
	p := filepath.Join(t.TempDir(), "captain-2026-09-19.db")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p, content
}

func sealed(t *testing.T, e Encryption) []byte {
	t.Helper()
	src, _ := dbFile(t)
	gz, _, err := gzipFile(src)
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := e.sealFile(gz)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	return b
}

func TestSealOpenWithKey(t *testing.T) {
	rcpt, id, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b := sealed(t, Encryption{Mode: "key", Recipient: rcpt})
	if !bytes.HasPrefix(b, []byte(ageHeader)) || bytes.Contains(b, []byte("captain rows")) {
		t.Fatal("remote copy is not an age file")
	}
	var out bytes.Buffer
	if err := Open(bytes.NewReader(b), &out, id, ""); err != nil {
		t.Fatal(err)
	}
	if _, want := dbFile(t); !bytes.Equal(out.Bytes(), want) {
		t.Fatal("round trip changed the database")
	}
	_, other, _ := GenerateKey()
	if err := Open(bytes.NewReader(b), io.Discard, other, ""); err == nil {
		t.Fatal("another identity opened the copy")
	}
	if err := Open(bytes.NewReader(b), io.Discard, "", ""); err == nil || !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("no identity: %v", err)
	}
}

func TestSealOpenWithPassphrase(t *testing.T) {
	b := sealed(t, Encryption{Mode: "passphrase", Passphrase: "correct horse battery"})
	var out bytes.Buffer
	if err := Open(bytes.NewReader(b), &out, "", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := Open(bytes.NewReader(b), io.Discard, "", "wrong horse battery"); err == nil {
		t.Fatal("wrong passphrase opened the copy")
	}
	for _, e := range []Encryption{{Mode: "passphrase", Passphrase: "short"}, {Mode: "key", Recipient: "not-a-key"}, {Mode: "rot13"}} {
		if e.Validate() == nil {
			t.Errorf("accepted %+v", e)
		}
	}
	if (Encryption{}).Validate() != nil {
		t.Error("off must be valid")
	}
}

func TestOpenPlainCopy(t *testing.T) {
	src, want := dbFile(t)
	gz, _, _ := gzipFile(src)
	f, _ := os.Open(gz)
	defer f.Close()
	var out bytes.Buffer
	if err := Open(f, &out, "", ""); err != nil || !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("plain .gz: %v", err)
	}
}

// The copy that leaves the host is the sealed one, under a name that says so.
func TestUploadSealsRemoteCopy(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]byte{}
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			got[filepath.Base(r.URL.Path)] = b
			mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer dav.Close()
	rcpt, id, _ := GenerateKey()
	var s Settings
	s.Remote = "webdav"
	s.WebDAV.URL = dav.URL + "/captain/"
	s.Encrypt = Encryption{Mode: "key", Recipient: rcpt}
	src, want := dbFile(t)
	name, err := (&Manager{}).upload(context.Background(), s, src)
	if err != nil {
		t.Fatal(err)
	}
	if name != "captain-2026-09-19.db.gz.age" {
		t.Fatalf("remote name %q", name)
	}
	var out bytes.Buffer
	if err := Open(bytes.NewReader(got[name]), &out, id, ""); err != nil || !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("uploaded copy does not open: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(src), ".upload-*")); len(left) > 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

// The encrypted off-site copy carries config.yaml beside the database:
// the payment keys and base_url are not in the database, so a panel
// restored without the config cannot take money.
func TestEncryptedUploadCarriesConfig(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]byte{}
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			got[filepath.Base(r.URL.Path)] = b
			mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer dav.Close()

	src, want := dbFile(t)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	cfgBody := []byte("base_url: https://panel.example.com\npayments:\n  epay:\n    key: s3cr3t\n")
	if err := os.WriteFile(cfg, cfgBody, 0o600); err != nil {
		t.Fatal(err)
	}
	rcpt, id, _ := GenerateKey()
	var s Settings
	s.Remote = "webdav"
	s.WebDAV.URL = dav.URL + "/captain/"
	s.Encrypt = Encryption{Mode: "key", Recipient: rcpt}
	m := &Manager{ConfigPath: cfg}

	name, err := m.upload(context.Background(), s, src)
	if err != nil {
		t.Fatal(err)
	}
	if name != "captain-2026-09-19.tar.gz.age" {
		t.Fatalf("remote name %q", name)
	}
	if !bytes.HasPrefix(got[name], []byte(ageHeader)) || bytes.Contains(got[name], []byte("s3cr3t")) {
		t.Fatal("the copy is not sealed")
	}
	out := t.TempDir()
	written, err := Extract(bytes.NewReader(got[name]), filepath.Join(out, "captain.db"), id, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("wrote %v", written)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "captain.db")); !bytes.Equal(b, want) {
		t.Error("database does not match")
	}
	if b, _ := os.ReadFile(filepath.Join(out, "config.yaml")); !bytes.Equal(b, cfgBody) {
		t.Error("config does not match")
	}
	// Extract never overwrites what is already there.
	if _, err := Extract(bytes.NewReader(got[name]), filepath.Join(out, "captain.db"), id, ""); err == nil {
		t.Error("an existing file was overwritten")
	}
}

// Without encryption the config stays on the host: an unencrypted copy is
// the gzipped database, as before, and older copies still open.
func TestPlainUploadLeavesConfigOut(t *testing.T) {
	var mu sync.Mutex
	got := map[string][]byte{}
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			got[filepath.Base(r.URL.Path)] = b
			mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer dav.Close()

	src, want := dbFile(t)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfg, []byte("base_url: https://panel.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var s Settings
	s.Remote = "webdav"
	s.WebDAV.URL = dav.URL + "/captain/"
	m := &Manager{ConfigPath: cfg}
	name, err := m.upload(context.Background(), s, src)
	if err != nil {
		t.Fatal(err)
	}
	if name != "captain-2026-09-19.db.gz" {
		t.Fatalf("remote name %q", name)
	}
	if bytes.Contains(got[name], []byte("base_url")) {
		t.Fatal("the config was uploaded unencrypted")
	}
	out := filepath.Join(t.TempDir(), "captain.db")
	written, err := Extract(bytes.NewReader(got[name]), out, "", "")
	if err != nil || len(written) != 1 {
		t.Fatalf("extract: %v %v", err, written)
	}
	if b, _ := os.ReadFile(out); !bytes.Equal(b, want) {
		t.Error("database does not match")
	}
}
