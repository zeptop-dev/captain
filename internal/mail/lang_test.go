package mail

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Every language offered in the picker must have every string, and none
// may be empty: a missing one would go out as a blank subject or an empty
// paragraph to a customer.
func TestCatalogIsComplete(t *testing.T) {
	for _, l := range Languages {
		tx, ok := catalog[l.Code]
		if !ok {
			t.Fatalf("language %q is offered but has no strings", l.Code)
		}
		v := reflect.ValueOf(tx)
		for i := 0; i < v.NumField(); i++ {
			if strings.TrimSpace(v.Field(i).String()) == "" {
				t.Errorf("%s: %s is empty", l.Code, v.Type().Field(i).Name)
			}
		}
	}
	for code := range catalog {
		found := false
		for _, l := range Languages {
			found = found || l.Code == code
		}
		if !found {
			t.Errorf("catalog has %q but the picker does not offer it", code)
		}
	}
}

// An unset or unknown language falls back to English rather than sending
// something empty.
func TestUnknownLanguageFallsBackToEnglish(t *testing.T) {
	en := For("en").Test("Site", "to@example.com")
	for _, lang := range []string{"", "klingon", "zh", "EN"} {
		if got := For(lang).Test("Site", "to@example.com"); got.Subject != en.Subject {
			t.Errorf("language %q: subject %q, want the English %q", lang, got.Subject, en.Subject)
		}
	}
}

// Each message carries its variable part in the subject and the body, in
// every language: the code, the date, the percentage.
func TestMessagesCarryTheirValues(t *testing.T) {
	expires := time.Date(2026, 9, 19, 15, 4, 0, 0, time.UTC)
	for _, l := range Languages {
		b := For(l.Code)

		code := b.Code("Site", "to@example.com", "register", "123456")
		if !strings.Contains(code.Subject, "123456") || !strings.Contains(code.HTML, "123456") {
			t.Errorf("%s: code missing from %q", l.Code, code.Subject)
		}
		reset := b.Code("Site", "to@example.com", "reset", "654321")
		if reset.Subject == code.Subject {
			t.Errorf("%s: reset and register share a subject", l.Code)
		}

		exp := b.Expiry("Site", "to@example.com", "https://portal.example.com", expires)
		if !strings.Contains(exp.Subject, "09-19") || !strings.Contains(exp.HTML, "2026-09-19 15:04") {
			t.Errorf("%s: expiry date missing: %q", l.Code, exp.Subject)
		}
		if !strings.Contains(exp.HTML, "https://portal.example.com") {
			t.Errorf("%s: expiry has no portal link", l.Code)
		}

		tr := b.Traffic("Site", "to@example.com", "https://portal.example.com", 95)
		if !strings.Contains(tr.Subject, "95") || !strings.Contains(tr.HTML, "95") {
			t.Errorf("%s: percentage missing: %q", l.Code, tr.Subject)
		}

		for _, m := range []Message{code, reset, exp, tr, b.Test("Site", "to@example.com")} {
			if !strings.HasPrefix(m.Subject, "[Site] ") {
				t.Errorf("%s: subject does not start with the site name: %q", l.Code, m.Subject)
			}
			if strings.Contains(m.Subject, "%!") || strings.Contains(m.HTML, "%!") {
				t.Errorf("%s: format placeholder mismatch in %q", l.Code, m.Subject)
			}
			if strings.Contains(m.HTML, "%s") || strings.Contains(m.HTML, "%d") {
				t.Errorf("%s: unfilled placeholder in the body of %q", l.Code, m.Subject)
			}
		}
	}
}

// The body is HTML, so a hostile portal URL or code cannot inject markup.
func TestValuesAreEscaped(t *testing.T) {
	b := For("en")
	m := b.Expiry("Site", "to@example.com", `https://x/"><script>alert(1)</script>`, time.Now())
	if strings.Contains(m.HTML, "<script>") {
		t.Fatalf("portal url was not escaped:\n%s", m.HTML)
	}
	c := b.Code("Site", "to@example.com", "register", "<b>1</b>")
	if strings.Contains(c.HTML, "<b>1</b>") {
		t.Fatalf("code was not escaped:\n%s", c.HTML)
	}
}
