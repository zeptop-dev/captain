package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
)

// fakeAPI answers getUpdates once with queued messages and records sends.
type fakeAPI struct {
	updates []Update
	sent    []map[string]any
}

func (f *fakeAPI) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var params map[string]any
		_ = json.NewDecoder(r.Body).Decode(&params)
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"username": "captain_bot"}})
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			ups := f.updates
			f.updates = nil
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": ups})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			f.sent = append(f.sent, params)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "unknown method"})
		}
	})
}

func msg(id, chat int64, text string) Update {
	var u Update
	_ = json.Unmarshal([]byte(`{"update_id":`+itoa(id)+`,"message":{"text":`+strconvQuote(text)+`,"chat":{"id":`+itoa(chat)+`}}}`), &u)
	return u
}

func itoa(i int64) string {
	return json.Number(strings.TrimSpace(strings.Replace(string(mustJSON(i)), "\n", "", -1))).String()
}
func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
func strconvQuote(s string) string { return string(mustJSON(s)) }

func TestBindAndCommands(t *testing.T) {
	api := &fakeAPI{}
	srv := httptest.NewServer(api.handler())
	defer srv.Close()
	APIBase = srv.URL

	conn, _ := db.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	_ = db.Migrate(context.Background(), conn, "sqlite")
	st := store.New(conn)
	u := &domain.User{Email: "u@test", Role: "user", UUID: "uuid", SubToken: "tok", Status: "active"}
	_ = st.CreateUser(context.Background(), u)
	_ = st.SetSetting(context.Background(), store.SettingTelegram, store.TelegramSettings{BotToken: "T", AdminChatID: 999})
	code, _ := st.NewTelegramBindCode(context.Background(), u.ID, time.Hour)

	bot := &Bot{Store: st, SiteName: "Captain", SubURL: func(_ context.Context, tok string) string { return "https://p/sub/" + tok }}
	api.updates = []Update{msg(1, 42, "/start"), msg(2, 42, "/bind "+strings.ToLower(code)), msg(3, 42, "/sub"), msg(4, 42, "/status@captain_bot"), msg(5, 43, "/sub")}
	if err := bot.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.sent) != 5 {
		t.Fatalf("sent %d messages: %v", len(api.sent), api.sent)
	}
	texts := make([]string, 0, 5)
	for _, m := range api.sent {
		texts = append(texts, m["text"].(string))
	}
	if !strings.Contains(texts[0], "/bind CODE") || !strings.Contains(texts[1], "Linked to <b>u@test</b>") || !strings.Contains(texts[2], "https://p/sub/tok") || !strings.Contains(texts[3], "No active plan") || !strings.Contains(texts[4], "Not linked") {
		t.Fatalf("replies: %q", texts)
	}
	if id, _ := st.TelegramID(context.Background(), u.ID); id != 42 {
		t.Fatalf("telegram id %d", id)
	}
	if bot.offset != 6 {
		t.Fatalf("offset %d", bot.offset)
	}

	// Notifications reach the linked chat and the admin chat.
	sent, err := bot.NotifyUser(context.Background(), u.ID, "hello")
	if err != nil || !sent {
		t.Fatalf("notify user: %v %v", sent, err)
	}
	if err := bot.NotifyAdmin(context.Background(), "admin"); err != nil {
		t.Fatal(err)
	}
	last := api.sent[len(api.sent)-1]
	if last["chat_id"].(float64) != 999 || last["text"] != "admin" {
		t.Fatalf("admin notice: %v", last)
	}
	// Unbind, then a user notice is not deliverable.
	api.updates = []Update{msg(6, 42, "/unbind")}
	_ = bot.Poll(context.Background())
	if sent, _ := bot.NotifyUser(context.Background(), u.ID, "x"); sent {
		t.Fatal("still bound")
	}
}
