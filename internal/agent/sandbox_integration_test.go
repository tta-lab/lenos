//go:build integration

package agent_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/temenos/client"
)

// TestSandboxIntegration_PathEnforcement runs against a real temenos daemon.
// It is triple-guarded so CI can never run it:
//  1. //go:build integration         — `go test ./...` excludes the file.
//  2. LENOS_TEMENOS_INTEGRATION=1    — opt-in env var.
//  3. client.Health() probe           — skip if no daemon listening.
//
// Run locally:
//
//	LENOS_TEMENOS_INTEGRATION=1 go test -tags=integration -run=Sandbox ./internal/agent/
func TestSandboxIntegration_PathEnforcement(t *testing.T) {
	if os.Getenv("LENOS_TEMENOS_INTEGRATION") != "1" {
		t.Skip("set LENOS_TEMENOS_INTEGRATION=1 to run this integration test locally")
	}

	// Env isolation: client.New("") falls through TEMENOS_LISTEN_ADDR →
	// TEMENOS_SOCKET_PATH → ~/.temenos/daemon.sock. If the test runner has
	// either env var set (common on dev machines), an empty addr could pick
	// up the wrong daemon. Force resolution to the default socket only.
	t.Setenv("TEMENOS_LISTEN_ADDR", "")
	t.Setenv("TEMENOS_SOCKET_PATH", "")

	c, err := client.New("")
	require.NoError(t, err)

	probeCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := c.Health(probeCtx); err != nil {
		t.Skipf("temenos daemon unreachable: %v", err)
	}

	tmp := t.TempDir()
	runCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("command inside AllowedPaths succeeds", func(t *testing.T) {
		resp, err := c.Run(runCtx, client.RunRequest{
			Command:      "echo ok > " + tmp + "/marker && cat " + tmp + "/marker",
			AllowedPaths: []client.AllowedPath{{Path: tmp, ReadOnly: false}},
			Timeout:      5,
		})
		require.NoError(t, err, "client.Run roundtrip should succeed")
		require.Equal(t, 0, resp.ExitCode, "in-bounds command should exit 0; stderr=%q", resp.Stderr)
		require.Contains(t, resp.Stdout, "ok")
	})

	t.Run("command escaping AllowedPaths fails", func(t *testing.T) {
		// /etc/hosts exists on every supported platform but is outside tmp.
		resp, err := c.Run(runCtx, client.RunRequest{
			Command:      "cat /etc/hosts",
			AllowedPaths: []client.AllowedPath{{Path: tmp, ReadOnly: false}},
			Timeout:      5,
		})
		require.NoError(t, err, "client.Run roundtrip should succeed (sandbox returns the failed exit, not a transport error)")
		require.NotEqual(t, 0, resp.ExitCode,
			"sandbox MUST block out-of-bounds read; got exit=0 stdout=%q — this means sandbox isolation is BROKEN",
			resp.Stdout)
	})

	t.Run("write escaping AllowedPaths fails", func(t *testing.T) {
		resp, err := c.Run(runCtx, client.RunRequest{
			Command:      "echo secret > /tmp/sandbox_test_escape_marker",
			AllowedPaths: []client.AllowedPath{{Path: tmp, ReadOnly: false}},
			Timeout:      5,
		})
		require.NoError(t, err, "client.Run roundtrip should succeed")
		require.NotEqual(t, 0, resp.ExitCode,
			"sandbox MUST block write outside AllowedPaths; got exit=0 — isolation is BROKEN")
	})
}
