package installtest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the actual POSIX installer and its embedded deployment helper with
// isolated files and fake host tools. No Docker daemon or root access is used.
func TestAgentHTTPSDockerInstaller(t *testing.T) {
	// The Python subprocess reads these inputs outside Go's test file tracking.
	// Read them here as well so edits invalidate Go's cached successful result.
	for _, name := range []string{"install.sh", filepath.Join("scripts", "test-agent-https-install.py")} {
		if _, err := os.ReadFile(filepath.Join("..", "..", name)); err != nil {
			t.Fatalf("read installer test input: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", filepath.Join("..", "..", "scripts", "test-agent-https-install.py"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Agent HTTPS installer fixtures failed: %v\n%s", err, out)
	}
}
