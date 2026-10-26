package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	cc "github.com/zeptop-dev/captain/internal/clientcatalog"
)

func catalogTestRelease(version string, assetID int64) *cc.Release {
	return &cc.Release{Version: version, URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/tag/" + version, PublishedAt: time.Now().UTC(), Assets: []cc.Asset{{ID: assetID, Name: "Example_x64.exe", Platform: "windows", Arch: "x64", PackageType: "exe", Size: 7, URL: "https://github.com/clash-verge-rev/clash-verge-rev/releases/download/" + version + "/Example_x64.exe", Version: version}}}
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
