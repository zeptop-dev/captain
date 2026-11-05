// Package agentendpoint resolves the published address used by agent installers.
// An address alone grants no server-side fetch permission. The explicit admin
// Agent HTTPS diagnostic separately authorizes one saved origin and fixed path.
package agentendpoint

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const (
	DownloadGitHub  = "github"
	DownloadCaptain = "captain"
)

// NormalizeURL accepts an optional HTTPS origin. Private destinations are valid:
// the node may reach Captain through a private network or an internal proxy.
// Existing base_url values are not passed through this new-field validator.
func NormalizeURL(raw string) (string, error) {
	for _, c := range raw {
		if unicode.IsControl(c) {
			return "", fmt.Errorf("agent URL must not contain control characters")
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return "", fmt.Errorf("agent URL must be an HTTPS origin without credentials, path, query or fragment")
	}
	host := u.Hostname()
	if host == "" || strings.Contains(host, "%") {
		return "", fmt.Errorf("agent URL has an invalid host")
	}
	if ip, err := netip.ParseAddr(host); err != nil {
		// Only ASCII DNS names are accepted. International names can use their
		// explicit punycode form; local single-label names remain supported.
		if strings.ContainsAny(u.Host, "[]") || strings.Contains(u.Host, ":") && u.Port() == "" || len(host) > 253 {
			return "", fmt.Errorf("agent URL has an invalid host")
		}
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return "", fmt.Errorf("agent URL has an invalid host")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
					return "", fmt.Errorf("agent URL has an invalid host")
				}
			}
		}
	} else if ip.Is6() != strings.HasPrefix(u.Host, "[") {
		return "", fmt.Errorf("agent URL requires brackets only around IPv6 addresses")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("agent URL port must be between 1 and 65535")
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return "", fmt.Errorf("agent URL port is empty")
	}
	return "https://" + strings.ToLower(u.Host), nil
}

// EffectiveURL preserves the existing base_url fallback, including deployments
// that explicitly use HTTP. New overrides are validated before being stored.
func EffectiveURL(nodeURL, defaultURL, baseURL string) string {
	for _, candidate := range []string{nodeURL, defaultURL, baseURL} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimRight(strings.TrimSpace(candidate), "/")
		}
	}
	return ""
}

func NormalizeDownloadSource(raw string) (string, error) {
	switch raw {
	case "", DownloadGitHub:
		return DownloadGitHub, nil
	case DownloadCaptain:
		return DownloadCaptain, nil
	default:
		return "", fmt.Errorf("download source must be github or captain")
	}
}
