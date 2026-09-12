package mail

import (
	"encoding/base64"
	"strings"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// wrap folds base64 at 76 columns as MIME requires.
func wrap(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76])
		b.WriteString("\r\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}
