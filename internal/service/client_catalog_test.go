package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	cc "github.com/zeptop-dev/captain/internal/clientcatalog"
)

func catalogTestRelease(version string, assetID int64) *cc.Release {
	return &cc.Release{Version: version, URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/tag/" + version, PublishedAt: time.Now().UTC(), Assets: []cc.Asset{{ID: assetID, Name: "Example_x64.exe", Platform: "windows", Arch: "x64", PackageType: "exe", Size: 7, URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/download/" + version + "/Example_x64.exe", Version: version}}}
}

func TestClientCatalogViewKeepsCompletionBehindCacheSnapshot(t *testing.T) {
	r := newRuntimeRig(t)
	dir := t.TempDir()
	m := NewClientCatalog(r.store, dir, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := m.View(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := r.store.ClientCatalog(ctx, "clash-verge-rev")
	if err != nil {
		t.Fatal(err)
	}
	m.job = &cc.Job{ID: "download", ClientID: "clash-verge-rev", AssetID: 1, State: "downloading"}

	// Hold the sole SQLite connection so the polling snapshot pauses during its
	// metadata read while a completed download is about to notify the caller.
	conn, err := r.store.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waits := r.store.DB().Stats().WaitCount
	type result struct {
		view cc.CatalogView
		err  error
	}
	viewResult := make(chan result, 1)
	go func() {
		view, err := m.View(ctx)
		viewResult <- result{view, err}
	}()
	for r.store.DB().Stats().WaitCount == waits {
		select {
		case <-ctx.Done():
			t.Fatal("catalog view did not start reading", ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}

	// Mirror download finalization: bytes and metadata become ready before the
	// complete notification. Use the held connection to preserve the pause.
	sha := fmt.Sprintf("%x", sha256.Sum256([]byte("fixture")))
	if err = os.WriteFile(filepath.Join(dir, sha), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	state.CachedAssets = []cc.CachedAsset{{Asset: catalogTestRelease("v1", 1).Assets[0], SHA256: sha, VerifiedBy: "local_sha256", CachedAt: time.Now().UTC()}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `UPDATE client_catalog SET document_json = ? WHERE id = ?`, string(raw), "clash-verge-rev"); err != nil {
		t.Fatal(err)
	}
	completed := make(chan struct{})
	go func() {
		m.setJob("download", "complete", "")
		close(completed)
	}()
	select {
	case <-completed:
		t.Fatal("completion overtook an unfinished catalog snapshot")
	case <-time.After(50 * time.Millisecond):
	}
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}
	first := <-viewResult
	if first.err != nil || first.view.Job == nil || first.view.Job.State != "downloading" {
		t.Fatalf("in-flight snapshot lost its pending job: %+v, %v", first.view.Job, first.err)
	}
	select {
	case <-completed:
	case <-ctx.Done():
		t.Fatal("completion remained blocked after snapshot", ctx.Err())
	}
	final, err := m.View(ctx)
	if err != nil || final.Job == nil || final.Job.State != "complete" {
		t.Fatalf("completion missing from next poll: %+v, %v", final.Job, err)
	}
	for _, client := range final.Clients {
		if client.ID == "clash-verge-rev" {
			if len(client.CachedAssets) != 1 || client.CachedAssets[0].ID != 1 {
				t.Fatalf("complete job omitted its package: %+v", client.CachedAssets)
			}
			return
		}
	}
	t.Fatal("completed client missing")
}

func TestClientCatalogPublicationSnapshotAndFailedCheck(t *testing.T) {
	r := newRuntimeRig(t)
	m := NewClientCatalog(r.store, t.TempDir(), nil)
	ctx := context.Background()
	inc, e := m.BeginCheck(ctx, "clash-verge-rev")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.CommitCheck(ctx, "clash-verge-rev", inc, catalogTestRelease("v1", 1), false, nil); e != nil {
		t.Fatal(e)
	}
	m.EndCheck("clash-verge-rev")
	c, e := m.Get(ctx, "clash-verge-rev")
	if e != nil {
		t.Fatal(e)
	}
	c.Platforms[0].ShowHome = true
	c.Platforms[0].AssetIDs = []int64{1}
	if _, e = m.Save(ctx, c.ID, c.Revision, c.Platforms); e != nil {
		t.Fatal(e)
	}
	before, e := m.Published(ctx, true)
	if e != nil || len(before) != 1 {
		t.Fatal(before, e)
	}
	if e = m.CommitCheck(ctx, c.ID, inc, catalogTestRelease("v2", 2), false, nil); e != nil {
		t.Fatal(e)
	}
	after, e := m.Published(ctx, true)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("check rewrote published snapshot", after, e)
	}
	if e = m.CommitCheck(ctx, c.ID, inc, nil, false, errors.New("offline")); e != nil {
		t.Fatal(e)
	}
	c, e = m.Get(ctx, c.ID)
	if e != nil || c.Release.Version != "v2" || c.CheckError == "" {
		t.Fatal("failure lost release", c, e)
	}
	// An old official asset remains selectable after a newer release is checked.
	if _, e = m.Save(ctx, c.ID, c.Revision, c.Platforms); e != nil {
		t.Fatal(e)
	}
	after, _ = m.Published(ctx, true)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("selected old asset lost", after)
	}
	if _, e = m.Save(ctx, c.ID, c.Revision, c.Platforms); !errors.Is(e, cc.ErrConflict) {
		t.Fatal("stale publication accepted", e)
	}
}
func TestClientCatalogMissingCacheCanAlwaysBeWithdrawn(t *testing.T) {
	r := newRuntimeRig(t)
	dir := t.TempDir()
	m := NewClientCatalog(r.store, dir, nil)
	ctx := context.Background()
	c, e := m.Get(ctx, "clash-verge-rev")
	if e != nil {
		t.Fatal(e)
	}
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	asset := catalogTestRelease("v1", 1).Assets[0]
	cached := cc.CachedAsset{Asset: asset, SHA256: sha, VerifiedBy: "local_sha256", CachedAt: time.Now()}
	if e = os.WriteFile(filepath.Join(dir, sha), []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = r.store.MutateClientCatalog(ctx, c.ID, func(s *cc.SavedClient) error { s.CachedAssets = []cc.CachedAsset{cached}; return nil }); e != nil {
		t.Fatal(e)
	}
	c.Platforms[0].Source = "cache"
	c.Platforms[0].AssetIDs = []int64{1}
	c.Platforms[0].ShowHome = true
	c, e = m.Save(ctx, c.ID, c.Revision, c.Platforms)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(dir, sha)); e != nil {
		t.Fatal(e)
	}
	view, e := m.Get(ctx, c.ID)
	if e != nil || len(view.CachedAssets) != 0 {
		t.Fatal("missing file fake-ready", view, e)
	}
	pub, e := m.Published(ctx, true)
	if e != nil || len(pub) != 0 {
		t.Fatal("missing file published", pub, e)
	}
	c.Platforms[0].ShowHome = false
	c, e = m.Save(ctx, c.ID, c.Revision, c.Platforms)
	if e != nil {
		t.Fatal("cannot withdraw missing package", e)
	}
	if e = m.DeleteCache(ctx, c.ID, 1); e != nil {
		t.Fatal(e)
	}
	c, e = m.Save(ctx, c.ID, c.Revision, c.Platforms)
	if e != nil {
		t.Fatal("hidden stale selection prevents save", e)
	}
	c.Platforms[0].ShowHome = true
	if _, e = m.Save(ctx, c.ID, c.Revision, c.Platforms); e == nil {
		t.Fatal("missing package was republished")
	}
}

type catalogBytesSource struct {
	body        []byte
	called      bool
	wrongDigest bool
}

func (s *catalogBytesSource) Check(context.Context, string) (*cc.Release, bool, error) {
	return nil, false, cc.ErrInvalid
}
func (s *catalogBytesSource) Download(_ context.Context, _ cc.Asset, w io.Writer) (string, error) {
	s.called = true
	_, e := w.Write(s.body)
	if s.wrongDigest {
		return strings.Repeat("a", 64), e
	}
	return fmt.Sprintf("%x", sha256.Sum256(s.body)), e
}
func TestClientCatalogCacheRestartCleanupQuotaAndChecksum(t *testing.T) {
	for _, scenario := range []string{"partial_and_orphan", "quota", "wrong_checksum"} {
		t.Run(scenario, func(t *testing.T) {
			r := newRuntimeRig(t)
			dir := t.TempDir()
			source := &catalogBytesSource{body: []byte("fixture"), wrongDigest: scenario == "wrong_checksum"}
			m := NewClientCatalog(r.store, dir, source)
			ctx := context.Background()
			inc, e := m.BeginCheck(ctx, "clash-verge-rev")
			if e != nil {
				t.Fatal(e)
			}
			if e = m.CommitCheck(ctx, "clash-verge-rev", inc, catalogTestRelease("v1", 1), false, nil); e != nil {
				t.Fatal(e)
			}
			m.EndCheck("clash-verge-rev")
			partial := filepath.Join(dir, ".part-old")
			orphan := filepath.Join(dir, strings.Repeat("b", 64))
			if e = os.WriteFile(partial, []byte("partial"), 0600); e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(orphan, []byte("orphan"), 0600); e != nil {
				t.Fatal(e)
			}
			if scenario == "quota" {
				f, e := os.OpenFile(filepath.Join(dir, "preserved-other-file"), os.O_CREATE|os.O_WRONLY, 0600)
				if e != nil {
					t.Fatal(e)
				}
				e = f.Truncate(cc.MaxCacheBytes)
				f.Close()
				if e != nil {
					t.Fatal(e)
				}
			}
			_, run, e := m.PrepareDownload(ctx, "clash-verge-rev", 1)
			if e != nil {
				t.Fatal(e)
			}
			run(func(commit func() error) error { return commit() })
			view, e := m.View(ctx)
			if e != nil {
				t.Fatal(e)
			}
			if scenario == "partial_and_orphan" {
				if view.Job.State != "complete" || view.Storage.UsedBytes != 7 {
					t.Fatal("restart cache recovery", view.Job, view.Storage)
				}
				if _, e = os.Stat(partial); !os.IsNotExist(e) {
					t.Fatal("partial retained", e)
				}
				if _, e = os.Stat(orphan); !os.IsNotExist(e) {
					t.Fatal("orphan retained", e)
				}
			} else {
				if view.Job.State != "failed" {
					t.Fatal("invalid transfer succeeded", view.Job)
				}
				c, _ := m.Get(ctx, "clash-verge-rev")
				if len(c.CachedAssets) > 0 {
					t.Fatal("invalid file ready", c)
				}
				if scenario == "quota" && source.called {
					t.Fatal("quota checked after network download")
				}
			}
		})
	}
}
