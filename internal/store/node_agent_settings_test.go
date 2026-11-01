package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestNodeAgentSettingsPersistenceAndLegacyWrites(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "captain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = db.Migrate(ctx, conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	n := &domain.Node{Name: "Private node", AgentURL: "https://10.10.0.2:18443/", DownloadSource: "captain"}
	if err = s.CreateNode(ctx, n, "pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	check := func(wantURL, wantSource string) {
		t.Helper()
		got, err := s.NodeByID(ctx, n.ID)
		if err != nil || got.AgentURL != wantURL || got.DownloadSource != wantSource {
			t.Fatalf("stored settings: %+v %v", got, err)
		}
	}
	check("https://10.10.0.2:18443", "captain")
	// Legacy callers constructing a replacement Node must not clear new fields.
	legacy := &domain.Node{ID: n.ID, Name: "Renamed"}
	if err = s.UpdateNode(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	check("https://10.10.0.2:18443", "captain")
	_, err = s.RedeemPairCode(ctx, "pair", "node-token-hash", "host", "test", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ResetPairCode(ctx, n.ID, "next-pair", time.Hour); err != nil {
		t.Fatal(err)
	}
	paired, err := s.NodeByPairCode(ctx, "next-pair")
	if err != nil || paired.AgentURL != "https://10.10.0.2:18443" || paired.DownloadSource != "captain" {
		t.Fatalf("repair lost settings: %+v %v", paired, err)
	}
	badURL, github := "https://example.com/path", "github"
	if err = s.UpdateNodeWithAgentSettings(ctx, legacy, &badURL, &github); err == nil {
		t.Fatal("bad URL accepted")
	}
	check("https://10.10.0.2:18443", "captain")
	empty := ""
	if err = s.UpdateNodeWithAgentSettings(ctx, legacy, &empty, nil); err != nil {
		t.Fatal(err)
	}
	check("", "captain")
	if err = s.UpdateNodeWithAgentSettings(ctx, legacy, nil, &empty); err != nil {
		t.Fatal(err)
	}
	check("", "github")
}
