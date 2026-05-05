package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tta-lab/temenos/client"
)

// AccessMode represents the filesystem access mode for the working directory inside the temenos sandbox.
type AccessMode string

const (
	AccessModeRW AccessMode = "rw"
	AccessModeRO AccessMode = "ro"
)

// sessionsDirSubpath is the cwd-relative subpath that always gets RW access
// inside the sandbox, regardless of the cwd access mode. This lets lenos's
// own session machinery (narrate, transcript writers) record session.md
// files even when the agent is running --readonly.
const sessionsDirSubpath = ".lenos/sessions"

// BuildAllowedPaths returns the allowed paths for an agent running in cwd with given access.
// access is AccessModeRW or AccessModeRO. CWD is always the first element (temenos uses first path as WorkingDir).
// additionalReadOnlyPaths are added as read-only paths (useful for granting cross-project read access).
//
// Carve-out: cwd/.lenos/sessions is always appended as RW. Lenos's session writers (narrate,
// transcript) need to append to <cwd>/.lenos/sessions/<session-id>.md throughout the agent
// loop; without this carve-out, --readonly would block the agent's own session log writes.
// The directory is created on the host first so the bind/mount succeeds inside the sandbox.
func BuildAllowedPaths(ctx context.Context, cwd string, access AccessMode, additionalReadOnlyPaths ...string) []client.AllowedPath {
	readOnly := access != AccessModeRW
	paths := []client.AllowedPath{{Path: cwd, ReadOnly: readOnly}}

	gitDir := resolveGitCommonDir(ctx, cwd)
	if gitDir != "" && gitDir != cwd+"/.git" {
		paths = append(paths, client.AllowedPath{Path: gitDir, ReadOnly: false})
	}

	for _, p := range additionalReadOnlyPaths {
		if p != cwd {
			paths = append(paths, client.AllowedPath{Path: p, ReadOnly: true})
		}
	}

	// Always carve out cwd/.lenos/sessions as RW. Best-effort MkdirAll on the host
	// so bwrap (Linux) doesn't skip the mount and seatbelt (macOS) has a real path
	// to allow rules against. Failure to create is non-fatal — the sandbox just
	// won't have the carve-out, and the existing failure mode (write blocked) reasserts.
	sessionsDir := filepath.Join(cwd, sessionsDirSubpath)
	_ = os.MkdirAll(sessionsDir, 0o755)
	paths = append(paths, client.AllowedPath{Path: sessionsDir, ReadOnly: false})

	return paths
}

// resolveGitCommonDir returns the git common dir for the given cwd.
func resolveGitCommonDir(ctx context.Context, cwd string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" || dir == ".git" {
		return cwd + "/.git"
	}
	if !strings.HasPrefix(dir, "/") {
		return cwd + "/" + dir
	}
	return dir
}
