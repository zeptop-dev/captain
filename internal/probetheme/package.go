// Package probetheme installs and serves validated, immutable Komari theme ZIPs.
// Uploaded JavaScript is never executed by Captain or on its admin origin.
package probetheme

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"unicode"
)

const MaxArchiveBytes = 32 << 20
const maxExpandedBytes = 96 << 20
const maxFileBytes = 24 << 20
const maxFiles = 8192

type Manifest struct {
	Name          any           `json:"name"`
	Short         string        `json:"short"`
	Version       string        `json:"version"`
	Description   any           `json:"description,omitempty"`
	Author        any           `json:"author,omitempty"`
	URL           string        `json:"url,omitempty"`
	Preview       string        `json:"preview,omitempty"`
	Configuration Configuration `json:"configuration"`
}
type Configuration struct {
	Type string          `json:"type"`
	Name any             `json:"name,omitempty"`
	Icon string          `json:"icon,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}
type Field struct {
	Key      string `json:"key"`
	Name     any    `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
	Options  string `json:"options,omitempty"`
	Default  any    `json:"default,omitempty"`
	Help     any    `json:"help,omitempty"`
}
type Package struct {
	Digest   string
	Manifest Manifest
	files    map[string]*zip.File
}

var shortPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

// Parse validates every entry, including unused files, without extracting the
// archive. Actual decompressed lengths and CRCs are checked, not only headers.
func Parse(data []byte) (*Package, error) {
	if len(data) == 0 || len(data) > MaxArchiveBytes {
		return nil, fmt.Errorf("theme ZIP must be at most 32 MiB")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid ZIP archive")
	}
	if len(zr.File) > maxFiles {
		return nil, fmt.Errorf("theme contains too many files")
	}
	p := &Package{files: map[string]*zip.File{}}
	sum := sha256.Sum256(data)
	p.Digest = hex.EncodeToString(sum[:])
	seen := map[string]bool{}
	var expanded uint64
	for _, f := range zr.File {
		name := f.Name
		if !safePath(strings.TrimSuffix(name, "/")) || f.Mode()&fs.ModeSymlink != 0 || (!f.Mode().IsRegular() && !f.FileInfo().IsDir()) {
			return nil, fmt.Errorf("unsafe ZIP entry")
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate ZIP entry")
		}
		seen[name] = true
		if f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > maxFileBytes {
			return nil, fmt.Errorf("theme file is too large")
		}
		expanded += f.UncompressedSize64
		if expanded > maxExpandedBytes {
			return nil, fmt.Errorf("expanded theme exceeds 96 MiB")
		}
		rc, e := f.Open()
		if e != nil {
			return nil, fmt.Errorf("invalid compressed file")
		}
		n, e := io.Copy(io.Discard, io.LimitReader(rc, maxFileBytes+1))
		rc.Close()
		if e != nil || n > maxFileBytes || uint64(n) != f.UncompressedSize64 {
			return nil, fmt.Errorf("invalid file contents or checksum")
		}
		p.files[name] = f
	}
	manifest, err := p.read("komari-theme.json", 256<<10)
	if err != nil {
		return nil, fmt.Errorf("ZIP root must contain komari-theme.json")
	}
	if err = json.Unmarshal(manifest, &p.Manifest); err != nil {
		return nil, fmt.Errorf("invalid theme manifest JSON")
	}
	m := &p.Manifest
	if !shortPattern.MatchString(m.Short) || strings.EqualFold(m.Short, "default") || !localized(m.Name) {
		return nil, fmt.Errorf("manifest requires a name and a valid non-default short identifier")
	}
	if len(m.Version) > 100 || strings.ContainsAny(m.Version, "\r\n\x00") {
		return nil, fmt.Errorf("invalid theme version")
	}
	// Older manifests may omit a release version. The digest keeps such uploads
	// immutable instead of making every old package conflict on an empty version.
	if m.Version == "" {
		m.Version = "unversioned-" + p.Digest[:12]
	}
	if _, ok := p.files["dist/index.html"]; !ok {
		return nil, fmt.Errorf("ZIP must contain dist/index.html; upload a built release package")
	}
	if m.Configuration.Type == "" {
		m.Configuration.Type = "managed"
	}
	switch m.Configuration.Type {
	case "managed":
		if _, err = m.Fields(); err != nil {
			return nil, err
		}
	case "raw", "redirect":
		var s string
		if json.Unmarshal(m.Configuration.Data, &s) != nil || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("invalid custom settings declaration")
		}
	default:
		return nil, fmt.Errorf("unsupported configuration type")
	}
	return p, nil
}
func safePath(s string) bool {
	return s != "" && strings.IndexFunc(s, unicode.IsControl) < 0 && len(s) <= 1024 && !strings.ContainsAny(s, "\\\x00:") && !strings.HasPrefix(s, "/") && path.Clean(s) == s && s != ".." && !strings.HasPrefix(s, "../")
}
func localized(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) != "" && len(x) <= 8192
	case map[string]any:
		if len(x) > 32 {
			return false
		}
		valid := false
		for _, v := range x {
			if s, ok := v.(string); !ok || len(s) > 8192 {
				return false
			} else if strings.TrimSpace(s) != "" {
				valid = true
			}
		}
		return valid
	}
	return false
}
func (p *Package) read(name string, limit int64) ([]byte, error) {
	f, ok := p.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("file exceeds size limit")
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if len(b) > int(limit) {
		return nil, fmt.Errorf("file exceeds size limit")
	}
	return b, err
}
func (m Manifest) Fields() ([]Field, error) {
	if m.Configuration.Type != "managed" {
		return nil, nil
	}
	if len(m.Configuration.Data) == 0 || string(m.Configuration.Data) == "null" {
		return nil, nil
	}
	var fields []Field
	if json.Unmarshal(m.Configuration.Data, &fields) != nil {
		return nil, fmt.Errorf("managed configuration must contain a field array")
	}
	if len(fields) > 256 {
		return nil, fmt.Errorf("too many theme settings")
	}
	keys := map[string]bool{}
	for _, f := range fields {
		if f.Type == "title" || f.Type == "textbox" {
			continue
		}
		if f.Key == "" || len(f.Key) > 128 || keys[f.Key] || f.Key == "__proto__" || f.Key == "constructor" || f.Key == "prototype" {
			return nil, fmt.Errorf("invalid or duplicate theme setting key")
		}
		keys[f.Key] = true
	}
	return fields, nil
}

// Settings normalizes documented selector fields to arrays, retaining explicit
// false/zero values. Unknown types remain JSON so future themes can be edited
// without executing their custom administrator interface.
func (m Manifest) Settings(input map[string]any) (map[string]any, error) {
	if input == nil {
		input = map[string]any{}
	}
	encoded, err := json.Marshal(input)
	if err != nil || len(encoded) > 256<<10 || len(input) > 256 {
		return nil, fmt.Errorf("theme settings exceed limits")
	}
	out := map[string]any{}
	for k, v := range input {
		if k == "__proto__" || k == "constructor" || k == "prototype" || len(k) > 128 {
			return nil, fmt.Errorf("invalid setting key")
		}
		out[k] = v
	}
	fields, err := m.Fields()
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if f.Type == "title" || f.Type == "textbox" {
			continue
		}
		v, ok := out[f.Key]
		if !ok {
			v = f.Default
		}
		if v == nil {
			if f.Required {
				return nil, fmt.Errorf("required setting %s is missing", f.Key)
			}
			continue
		}
		switch f.Type {
		case "switch":
			if _, ok := v.(bool); !ok {
				return nil, fmt.Errorf("setting %s must be boolean", f.Key)
			}
		case "number":
			if _, ok := v.(float64); !ok {
				return nil, fmt.Errorf("setting %s must be a number", f.Key)
			}
		case "string", "richtext", "select":
			s, ok := v.(string)
			if !ok || len(s) > 64<<10 {
				return nil, fmt.Errorf("setting %s must be text", f.Key)
			}
			if f.Required && strings.TrimSpace(s) == "" {
				return nil, fmt.Errorf("required setting %s is empty", f.Key)
			}
		case "nodes", "pingtasks":
			if s, ok := v.(string); ok {
				if json.Unmarshal([]byte(s), &v) != nil {
					return nil, fmt.Errorf("invalid selector %s", f.Key)
				}
			}
			a, ok := v.([]any)
			if !ok || len(a) > 1000 {
				return nil, fmt.Errorf("setting %s must be an array", f.Key)
			}
			for _, item := range a {
				if f.Type == "nodes" {
					if s, ok := item.(string); !ok || len(s) > 128 {
						return nil, fmt.Errorf("invalid node selector")
					}
				} else {
					n, ok := item.(float64)
					if !ok || n < 1 || n != float64(int64(n)) {
						return nil, fmt.Errorf("invalid ping task selector")
					}
				}
			}
		}
		out[f.Key] = v
	}
	return out, nil
}
