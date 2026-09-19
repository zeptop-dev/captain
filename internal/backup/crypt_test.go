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
