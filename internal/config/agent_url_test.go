package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentURLConfigPreservesPanelURLAndLegacyDefault(t *testing.T) {
	for _, tc := range []struct {
		name, extra, want string
		valid             bool
	}{
		{"legacy", "", "", true},
		{"private", "agent_url: https://10.10.0.2:18443/\n", "https://10.10.0.2:18443", true},
		{"plaintext", "agent_url: http://10.10.0.2:8080\n", "", false},
		{"path", "agent_url: https://example.com/api\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("base_url: http://example.com:8080\n"+tc.extra), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if !tc.valid {
				if err == nil {
					t.Fatal("invalid agent URL accepted")
				}
				return
			}
			if err != nil || c.AgentURL != tc.want || c.BaseURL != "http://example.com:8080" {
				t.Fatalf("config %+v: %v", c, err)
			}
		})
	}
}
