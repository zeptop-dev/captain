package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/store"
)

func TestEmitSignsAndFilters(t *testing.T) {
	type hit struct {
		event, sig string
		body       []byte
	}
	var got []hit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, hit{r.Header.Get("X-Captain-Event"), r.Header.Get("X-Captain-Signature"), b})
	}))
	defer srv.Close()
	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	_ = st.SetSetting(context.Background(), SettingKey, Settings{Endpoints: []Endpoint{
		{URL: srv.URL + "/all", Secret: "s3", Enabled: true},
		{URL: srv.URL + "/orders", Events: []string{OrderPaid}, Enabled: true},
		{URL: srv.URL + "/off", Enabled: false},
	}})
	h := &Hub{Store: st, Sync: true}
	h.Emit(context.Background(), UserRegistered, map[string]any{"email": "a@test"})
	h.Emit(context.Background(), OrderPaid, map[string]any{"no": "1"})
	if len(got) != 3 {
		t.Fatalf("deliveries %d", len(got))
	}
	if got[0].event != UserRegistered || got[1].event != OrderPaid || got[2].event != OrderPaid {
		t.Fatalf("events %+v", got)
	}
	if got[0].sig != "sha256="+Sign("s3", got[0].body) {
		t.Fatalf("bad signature %s", got[0].sig)
	}
	if got[2].sig != "" {
		t.Fatal("unsigned endpoint got a signature")
	}
	var env struct {
		Event string         `json:"event"`
		Data  map[string]any `json:"data"`
	}
	_ = json.Unmarshal(got[0].body, &env)
	if env.Event != UserRegistered || env.Data["email"] != "a@test" {
		t.Fatalf("envelope %s", got[0].body)
	}
}
