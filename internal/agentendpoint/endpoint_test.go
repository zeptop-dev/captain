package agentendpoint

import "testing"

func TestNormalizeURL(t *testing.T) {
	valid := map[string]string{
		"": "", "  ": "", "https://Agent.Example.com:18443/": "https://agent.example.com:18443",
		" https://10.10.0.2:18443 ": "https://10.10.0.2:18443", "https://captain": "https://captain",
		"https://[2001:db8::20]:18443": "https://[2001:db8::20]:18443", "https://[fd00::20]": "https://[fd00::20]",
	}
	for raw, want := range valid {
		if got, err := NormalizeURL(raw); err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"http://example.com", "ftp://example.com", "//example.com", "https://", "https://user:pass@example.com",
		"https://example.com/api", "https://example.com//", "https://example.com/%2f", "https://example.com?", "https://example.com?q=1", "https://example.com#", "https://example.com#fragment",
		"https://example.com:0", "https://example.com:65536", "https://example.com:", "https://example.com:-1", "https://example.com:abc",
		"https://example.com\n", "https://example.com\t", "https://example.com\x00", "https://example.com`cmd`", "https://$(cmd).example.com",
		"https://[example.com]:443", "https://[198.51.100.20]:443", "https://2001:db8::20", "https://[fe80::20%25eth0]", "https://-example.com", "https://a..example.com",
	} {
		if got, err := NormalizeURL(raw); err == nil {
			t.Errorf("accepted %q as %q", raw, got)
		}
	}
}

func TestEffectiveURLAndDownloadSource(t *testing.T) {
	for _, tc := range []struct{ node, global, base, want string }{
		{"https://node.example.com:18443/", "https://agent.example.com", "https://example.com", "https://node.example.com:18443"},
		{"", "https://agent.example.com/", "https://example.com", "https://agent.example.com"},
		{"", "", "http://10.10.0.2:8080/", "http://10.10.0.2:8080"},
	} {
		if got := EffectiveURL(tc.node, tc.global, tc.base); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
	for raw, want := range map[string]string{"": DownloadGitHub, "github": DownloadGitHub, "captain": DownloadCaptain} {
		if got, err := NormalizeDownloadSource(raw); err != nil || got != want {
			t.Fatalf("source %q: %q %v", raw, got, err)
		}
	}
	if _, err := NormalizeDownloadSource("https://example.com"); err == nil {
		t.Fatal("arbitrary download source accepted")
	}
}
