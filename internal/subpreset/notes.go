package subpreset

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxNoteLines   = 12
	maxNoteRunes   = 1024
	maxHeaderBytes = 16 << 10
)

// OfficialNotes returns a plain-text excerpt of the preset's opening comments.
// It does not interpret the comments as capabilities or inspect later rules.
// Callers must establish that source is an official, verified cached preset.
func OfficialNotes(source string) string {
	if len(source) > maxFileBytes || !utf8.ValidString(source) {
		return ""
	}
	source = strings.TrimPrefix(source, "\ufeff")
	lines := make([]string, 0, maxNoteLines)
	runes, scanned := 0, 0
	section := false
	for source != "" {
		line, rest, _ := strings.Cut(source, "\n")
		source = rest
		scanned += len(line) + 1
		if scanned > maxHeaderBytes {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "[custom]" && !section {
			section = true
			continue
		}
		if line[0] != ';' && line[0] != '#' {
			break
		}
		line = cleanNote(strings.TrimLeft(line, ";# \t"))
		if line == "" || noteIsConfiguration(line) {
			continue
		}
		if len(lines) == maxNoteLines {
			break
		}
		size := utf8.RuneCountInString(line)
		separator := 0
		if len(lines) > 0 {
			separator = 1
		}
		remaining := maxNoteRunes - runes - separator
		if remaining <= 0 {
			break
		}
		if size > remaining {
			line = string([]rune(line)[:remaining-1]) + "…"
			lines = append(lines, line)
			break
		}
		lines = append(lines, line)
		runes += separator + size
	}
	return strings.Join(lines, "\n")
}

func cleanNote(line string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, line))
}

func noteIsConfiguration(line string) bool {
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return true
	}
	if key, _, found := strings.Cut(line, "="); found {
		key = strings.TrimSpace(key)
		identifier := key != ""
		for _, r := range key {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
				identifier = false
				break
			}
		}
		if identifier {
			return true
		}
	}
	if strings.Contains(line, "设置规则标志位") {
		return true
	}
	// Decorative separators are not descriptions.
	for _, r := range line {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
