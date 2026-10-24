package subpreset

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// All preset fragments here are synthetic, not bundled upstream descriptions.
func TestOfficialNotesHeader(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
	}{
		{"empty", "", ""},
		{"before and after custom section", "\ufeff; A routing example\r\n\r\n[custom]\r\n# Return traffic through the selected country.\r\n; Service groups: enabled\r\nruleset=Proxy,[]FINAL\r\n; Later implementation comment\r\n", "A routing example\nReturn traffic through the selected country.\nService groups: enabled"},
		{"configuration comments excluded", "[custom]\n;----\n;[custom]\n;ruleset = DIRECT,[]FINAL\n#custom_proxy_group=Hidden`select`.*\n;enable_rule_generator=true\n;overwrite_original_rules = true\n;unknown_setting=value\n; Keep this explanatory sentence.\nenable_rule_generator=true\n", "Keep this explanatory sentence."},
		{"rule marker excluded", "[custom]\n; Header\n;设置规则标志位\nruleset=Proxy,[]FINAL\n", "Header"},
		{"first directive ends notes", "[custom]\nenable_rule_generator=true\n; This is not opening documentation\n", ""},
		{"unknown directive ends notes", "; Opening note\nscript=unsupported\n; Not a description\n", "Opening note"},
		{"unknown section ends notes", "; Opening note\n[other]\n; Another section's documentation\n", "Opening note"},
		{"repeated section ends notes", "[custom]\n; Opening note\n[custom]\n; Another header\n", "Opening note"},
		{"strip controls and formatting", "[custom]\n; Visible\x00 note\x1b\u202e\u2066\ufeff\n; Tabs\tseparate words\n", "Visible note\nTabs separate words"},
		{"plain text preserved", "[custom]\n; <b>Example</b> is text, not markup.\n; Use https://example.com/docs for context.\n", "<b>Example</b> is text, not markup.\nUse https://example.com/docs for context."},
		{"invalid UTF-8", "; Valid header\n; invalid\xff\n", ""},
		{"oversized cached source", "; Header\n" + strings.Repeat(" ", maxFileBytes), ""},
		{"bounded header scan", strings.Repeat("\n", maxHeaderBytes) + "; Hidden far below the header\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := OfficialNotes(tc.source); got != tc.want {
				t.Fatalf("notes = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOfficialNotesOutputBounds(t *testing.T) {
	for _, source := range []string{
		strings.Repeat("; Repeated note\n", maxNoteLines+20),
		"; " + strings.Repeat("é", maxNoteRunes+20),
		"; Intro\n; " + strings.Repeat("λ", maxNoteRunes+20),
		"; " + strings.Repeat("z", maxNoteRunes-2) + "\n; Second note",
	} {
		got := OfficialNotes(source)
		if got == "" || !utf8.ValidString(got) || utf8.RuneCountInString(got) > maxNoteRunes || len(strings.Split(got, "\n")) > maxNoteLines {
			t.Fatalf("description limits or encoding violated: %d runes, %d lines", utf8.RuneCountInString(got), len(strings.Split(got, "\n")))
		}
		for _, r := range got {
			if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
				t.Fatal("description retained an unsafe control")
			}
		}
	}
}
