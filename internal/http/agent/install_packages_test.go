package agent

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestInstallWrapperChecksSystemPackageSupportBeforeExecution(t *testing.T) {
	st, mux, _ := fixture(t)
	const pair = "PACKAGE-PAIR"
	if err := st.CreateNode(context.Background(), &domain.Node{Name: "package-test", DownloadSource: "captain"}, pair, time.Hour); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/api/agent/install.sh?pair="+pair, nil))
	if response.Code != 200 {
		t.Fatalf("wrapper HTTP status: %d", response.Code)
	}
	for _, tt := range []struct {
		name, marker, option string
		failDownload         bool
		wantRun              bool
	}{
		{name: "legacy default", wantRun: true},
		{name: "legacy explicit install", option: "install"},
		{name: "legacy explicit skip", option: "skip"},
		{name: "wrong marker", marker: "# bosun-installer-feature: system-packages-v10", option: "install"},
		{name: "new install", marker: "# bosun-installer-feature: system-packages-v1", option: "install", wantRun: true},
		{name: "new skip", marker: "# bosun-installer-feature: system-packages-v1", option: "skip", wantRun: true},
		{name: "failed download", marker: "# bosun-installer-feature: system-packages-v1", option: "install", failDownload: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name, content string) string {
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
				return path
			}
			write("id", "#!/bin/sh\nprintf '0\\n'\n")
			write("curl", `#!/bin/sh
[ "$1" = -q ] || exit 99
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then shift; cp "$FIXTURE_SCRIPT" "$1"; fi
  shift
done
exit "$CURL_STATUS"
`)
			fixtureScript := write("fixture", "#!/bin/sh\n"+tt.marker+"\nprintf '%s\\n' \"$@\" > \"$CALLED_FILE\"\n")
			wrapper := write("wrapper", response.Body.String())
			called := filepath.Join(dir, "called")
			args := []string{wrapper}
			if tt.option != "" {
				args = append(args, "--system-packages", tt.option)
			}
			cmd := exec.Command("sh", args...)
			status := "0"
			if tt.failDownload {
				status = "22"
			}
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "FIXTURE_SCRIPT="+fixtureScript, "CALLED_FILE="+called, "CURL_STATUS="+status, "TMPDIR="+dir)
			out, err := cmd.CombinedOutput()
			if strings.Contains(string(out), pair) {
				t.Fatal("pair code appeared in wrapper diagnostics")
			}
			arguments, readErr := os.ReadFile(called)
			if tt.wantRun {
				if err != nil || readErr != nil {
					t.Fatalf("installer did not run: %v, %v; %s", err, readErr, out)
				}
				if tt.option != "" && !strings.Contains(string(arguments), "--system-packages\n"+tt.option+"\n") {
					t.Fatal("installer did not receive the selected package option")
				}
			} else {
				if err == nil || !os.IsNotExist(readErr) {
					t.Fatalf("unsupported installer was executed: %v %v", err, readErr)
				}
				if !tt.failDownload && !strings.Contains(string(out), "does not support") {
					t.Fatalf("missing actionable compatibility message: %s", out)
				}
			}
		})
	}
}

// Keep the pinned bosun bootstrap in sync with the installation choices exposed
// by Captain. This must run in ordinary CI, without a local workspace override.
func TestSystemPackagesEmbeddedInstaller(t *testing.T) {
	st, mux, _ := fixture(t)
	const pair = "EMBEDDED-PACKAGES"
	if err := st.CreateNode(context.Background(), &domain.Node{Name: "embedded-package-test", DownloadSource: "captain"}, pair, time.Hour); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/agent/downloads/bootstrap/install.sh", nil)
	request.Header.Set("X-Bosun-Pair-Code", pair)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("authenticated installer status: %d", response.Code)
	}
	body := response.Body.String()
	for _, required := range []string{"# bosun-installer-feature: system-packages-v1", "prepare_system_packages", "iproute2-tc", "--system-packages"} {
		if !strings.Contains(body, required) {
			t.Fatalf("embedded installer lacks %q; fix the bosun dependency before release", required)
		}
	}
	if strings.Contains(body, pair) {
		t.Fatal("generic embedded installer contains a pair credential")
	}
}
