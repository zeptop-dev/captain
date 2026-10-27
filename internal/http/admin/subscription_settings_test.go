package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

func subscriptionSettingsHandler(t *testing.T) *handlers {
	t.Helper()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(context.Background(), conn, "sqlite"); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	return &handlers{Deps: Deps{Store: st, SubLinks: &service.SubLinks{Store: st, BaseURL: "https://panel.example.com"}}}
}

func putSubscriptionSettings(h *handlers, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.putSubscription(w, httptest.NewRequest(http.MethodPut, "/api/admin/settings/subscription", strings.NewReader(body)))
	return w
}

func readSubscriptionSettings(t *testing.T, h *handlers) service.SubscriptionSettings {
	t.Helper()
	w := httptest.NewRecorder()
	h.getSubscription(w, httptest.NewRequest(http.MethodGet, "/api/admin/settings/subscription", nil))
	var got service.SubscriptionSettings
	if w.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSubscriptionSettingsIndependentSections(t *testing.T) {
	const subscription = `{"URLs":[" https://sub.example.com/ ",""],"short_links":true,"auto_flags":true,"info_lines":["{{PLAN}}","{{TRAFFIC_LEFT}}"],"hwid":{"enabled":true,"require":true,"fallback_limit":3,"announce":"Device limit"}}`
	want := service.SubscriptionSettings{
		URLs: []string{"https://sub.example.com"}, ShortLinks: true, AutoFlags: true, SinglePlan: true,
		InfoLines: []string{"{{PLAN}}", "{{TRAFFIC_LEFT}}"},
		HWID:      service.HWIDSettings{Enabled: true, Require: true, FallbackLimit: 3, Announce: "Device limit"},
	}
	for _, purchaseFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "purchase-first", false: "subscription-first"}[purchaseFirst], func(t *testing.T) {
			h := subscriptionSettingsHandler(t)
			// Prime the real subscription URL cache before either form saves.
			if got := h.SubLinks.URL(context.Background(), "example-token"); got != "https://panel.example.com/sub/example-token" {
				t.Fatal(got)
			}
			bodies := []string{`{"single_plan":true}`, subscription}
			if !purchaseFirst {
				bodies[0], bodies[1] = bodies[1], bodies[0]
			}
			for _, body := range bodies {
				if w := putSubscriptionSettings(h, body); w.Code != http.StatusOK {
					t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
				}
			}
			if got := readSubscriptionSettings(t, h); !reflect.DeepEqual(got, want) {
				t.Fatalf("independent save lost settings: got %+v, want %+v", got, want)
			}
			if got := h.SubLinks.URL(context.Background(), "example-token"); !strings.HasPrefix(got, "https://sub.example.com/") {
				t.Fatalf("subscription URL cache was not invalidated: %s", got)
			}
			if w := putSubscriptionSettings(h, `{"single_plan":false}`); w.Code != http.StatusOK {
				t.Fatal(w.Code, w.Body.String())
			}
			wantDisabled := want
			wantDisabled.SinglePlan = false
			if got := readSubscriptionSettings(t, h); !reflect.DeepEqual(got, wantDisabled) {
				t.Fatalf("disabling single-plan modified subscription fields: %+v", got)
			}
		})
	}
}

func TestSubscriptionSettingsLegacyFullPutAndPartialValues(t *testing.T) {
	h := subscriptionSettingsHandler(t)
	for _, body := range []string{
		`{"urls":["https://old.example.com"],"short_links":true,"auto_flags":true,"single_plan":true,"info_lines":["old"],"hwid":{"enabled":true,"require":true,"fallback_limit":3,"announce":"Old"}}`,
		`{"hwid":{"fallback_limit":4}}`,
	} {
		if w := putSubscriptionSettings(h, body); w.Code != http.StatusOK {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if got := readSubscriptionSettings(t, h); !got.SinglePlan || got.HWID != (service.HWIDSettings{Enabled: true, Require: true, FallbackLimit: 4, Announce: "Old"}) {
		t.Fatalf("partial nested HWID no longer preserves omitted fields: %+v", got)
	}
	// A legacy whole-document PUT still replaces explicit booleans and arrays.
	if w := putSubscriptionSettings(h, `{"URLs":[],"short_links":false,"auto_flags":false,"single_plan":false,"info_lines":[],"hwid":{"enabled":false,"require":false,"fallback_limit":0,"announce":""}}`); w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	want := service.SubscriptionSettings{URLs: []string{}, InfoLines: []string{}}
	if got := readSubscriptionSettings(t, h); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy full PUT changed semantics: %+v", got)
	}
	for _, body := range []string{`{"urls":["invalid"],"single_plan":true}`, `{"single_plan":"invalid"}`, `{"single_plan":`} {
		if w := putSubscriptionSettings(h, body); w.Code != http.StatusBadRequest {
			t.Fatal(w.Code, w.Body.String())
		}
		if got := readSubscriptionSettings(t, h); !reflect.DeepEqual(got, want) {
			t.Fatalf("invalid save changed persisted settings: %+v", got)
		}
	}
}

func TestSubscriptionSettingsReadFailureDoesNotReplaceDocument(t *testing.T) {
	h := subscriptionSettingsHandler(t)
	const corrupt = `{"single_plan":true,"hwid":`
	if _, err := h.Store.DB().ExecContext(context.Background(), `INSERT INTO settings (key, value_json) VALUES (?, ?)`, service.SettingSubscription, corrupt); err != nil {
		t.Fatal(err)
	}
	if w := putSubscriptionSettings(h, `{"single_plan":false}`); w.Code != http.StatusInternalServerError {
		t.Fatal(w.Code, w.Body.String())
	}
	var raw string
	if err := h.Store.DB().QueryRowContext(context.Background(), `SELECT value_json FROM settings WHERE key = ?`, service.SettingSubscription).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != corrupt {
		t.Fatal("a failed read was overwritten with default fields")
	}
}

type gatedSubscriptionBody struct {
	io.Reader
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (b *gatedSubscriptionBody) Read(p []byte) (int, error) {
	b.once.Do(func() {
		close(b.entered)
		<-b.release
	})
	return b.Reader.Read(p)
}

func TestSubscriptionSettingsConcurrentSectionSaves(t *testing.T) {
	h := subscriptionSettingsHandler(t)
	body := &gatedSubscriptionBody{Reader: strings.NewReader(`{"single_plan":true}`), entered: make(chan struct{}), release: make(chan struct{})}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.putSubscription(w, httptest.NewRequest(http.MethodPut, "/api/admin/settings/subscription", body))
		first <- w
	}()
	// The first request has read the stored document but not its JSON body.
	<-body.entered
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		second <- putSubscriptionSettings(h, `{"urls":["https://sub.example.com"],"short_links":true}`)
	}()
	var secondResponse *httptest.ResponseRecorder
	select {
	case secondResponse = <-second:
		t.Error("second section saved before the first merge completed")
	case <-time.After(100 * time.Millisecond):
	}
	close(body.release)
	if w := <-first; w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if secondResponse == nil {
		secondResponse = <-second
	}
	if secondResponse.Code != http.StatusOK {
		t.Fatal(secondResponse.Code, secondResponse.Body.String())
	}
	got := readSubscriptionSettings(t, h)
	if !got.SinglePlan || !got.ShortLinks || !reflect.DeepEqual(got.URLs, []string{"https://sub.example.com"}) {
		t.Fatalf("concurrent section saves lost an edit: %+v", got)
	}
}
