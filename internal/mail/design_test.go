package mail

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMailDesignCompatibilityAndIsolation(t *testing.T) {
	for _, lang := range Languages {
		old := For(lang.Code)
		current := Configured(Settings{}, lang.Code)
		at := time.Date(2030, 1, 15, 12, 0, 0, 0, time.UTC)
		a := []Message{old.Code("Site", "to@example.com", "register", "123456"), old.Code("Site", "to@example.com", "reset", "123456"), old.Expiry("Site", "to@example.com", "https://example.com/portal/", at), old.Traffic("Site", "to@example.com", "https://example.com/portal/", 90), old.Test("Site", "to@example.com")}
		b := []Message{current.Code("Site", "to@example.com", "register", "123456"), current.Code("Site", "to@example.com", "reset", "123456"), current.Expiry("Site", "to@example.com", "https://example.com/portal/", at), current.Traffic("Site", "to@example.com", "https://example.com/portal/", 90), current.Test("Site", "to@example.com")}
		if !reflect.DeepEqual(a, b) {
			t.Fatal(lang.Code, "defaults changed")
		}
	}
	d := Design{Name: "Brand", Color: "#123456", Logo: "https://example.com/logo.png", Footer: "<Footer>", Templates: map[string]map[string]Template{"zh-CN": {"register": {Subject: "{{site}} code", Body: "Welcome <script> {{site}}"}, "traffic": {Subject: "{{site}} {{plan}}", Body: "{{used}} / {{total}}; {{reset_at}}", Button: "Open"}, "notice": {Subject: "{{subject}}", Body: "{{body}}"}}}}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	b := Configured(Settings{Design: d}, "zh-CN")
	m := b.Code("Site", "to@example.com", "register", "654321")
	if !strings.Contains(m.HTML, "654321") || strings.Contains(m.HTML, "<script>") || !strings.Contains(m.HTML, "&lt;Footer&gt;") || m.Subject != "Brand code" {
		t.Fatal(m)
	}
	m = b.Traffic("Site", "to@example.com", "https://example.com/portal/", 90, ReminderValues("Plan", 90<<30, 100<<30, 1893456000))
	for _, want := range []string{"90.00 GiB / 100.00 GiB", "#123456", "https://example.com/portal/", "2030-01-01"} {
		if !strings.Contains(m.HTML, want) {
			t.Fatal(want, m)
		}
	}
	en := Configured(Settings{Design: d}, "en").Code("Site", "to@example.com", "register", "654321")
	if strings.Contains(en.HTML, "Welcome") {
		t.Fatal("custom language leaked", en)
	}
	m = b.Notice("Site", "to@example.com", "Injected\r\nBcc: x@example.com", "literal {{code}} <img src=x>")
	if strings.ContainsAny(m.Subject, "\r\n") || strings.Contains(m.HTML, "<img src=x>") || !strings.Contains(m.HTML, "{{code}}") {
		t.Fatal(m)
	}
	for _, kind := range Kinds {
		m, err := b.Sample(kind, "Site", "to@example.com", "https://example.com/portal/")
		if err != nil || !strings.HasPrefix(m.Subject, "[TEST] ") {
			t.Fatal(kind, m, err)
		}
	}
}
func TestMailDesignValidation(t *testing.T) {
	for _, d := range []Design{{Color: "red;display:none"}, {Logo: "javascript:alert(1)"}, {Logo: "https://user:secret@example.com"}, {Name: "x\r\nBcc:x"}, {Templates: map[string]map[string]Template{"zz": {"test": {Body: "x"}}}}, {Templates: map[string]map[string]Template{"en": {"unknown": {Body: "x"}}}}, {Templates: map[string]map[string]Template{"en": {"test": {Body: "{{code}}"}}}}, {Templates: map[string]map[string]Template{"en": {"reset": {Body: "{{unknown}}"}}}}, {Templates: map[string]map[string]Template{"en": {"reset": {Body: "{{code"}}}}} {
		if d.Validate() == nil {
			t.Fatal("accepted", d)
		}
	}
}
