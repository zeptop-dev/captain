package installtest

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the actual POSIX installer and its embedded deployment helper with
// isolated files and fake host tools. No Docker daemon or root access is used.
func TestAgentHTTPSDockerInstaller(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", filepath.Join("..", "..", "scripts", "test-agent-https-install.py"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Agent HTTPS installer fixtures failed: %v\n%s", err, out)
	}
}
