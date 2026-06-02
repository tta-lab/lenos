package agent

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AllowedPath describes a filesystem path the sandbox can access.
type AllowedPath struct {
	Path     string
	ReadOnly bool
}

// AccessMode represents the filesystem access mode for the working directory inside the temenos sandbox.
type AccessMode string

const (
	AccessModeRW AccessMode = "rw"
	AccessModeRO AccessMode = "ro"
)

// sessionsDirSubpath is the cwd-relative subpath that always gets RW access
// inside the sandbox, regardless of the cwd access mode. This lets lenos's
// own session machinery records session data
// files even when the agent is running --readonly.
const sessionsDirSubpath = ".lenos/sessions"

// BuildAllowedPaths returns the allowed paths for an agent running in cwd with given access.
// access is AccessModeRW or AccessModeRO. CWD is always the first element (temenos uses first path as WorkingDir).
//
// Carve-out: cwd/.lenos/sessions is always appended as RW. Lenos's session writers
// need to append to <cwd>/.lenos/sessions/<session-id>.md throughout the agent
// loop; without this carve-out, --readonly would block the agent's own session log writes.
// Runtime init is responsible for creating the directory before any agent run.
func BuildAllowedPaths(ctx context.Context, cwd string, access AccessMode) []AllowedPath {
	readOnly := access != AccessModeRW
	paths := []AllowedPath{{Path: cwd, ReadOnly: readOnly}}

	gitDir := resolveGitCommonDir(ctx, cwd)
	if gitDir != "" && gitDir != cwd+"/.git" {
		paths = append(paths, AllowedPath{Path: gitDir, ReadOnly: false})
	}

	// Always carve out cwd/.lenos/sessions as RW. Runtime init owns creation of
	// the directory; this function only declares the carve-out path.
	sessionsDir := filepath.Join(cwd, sessionsDirSubpath)
	paths = append(paths, AllowedPath{Path: sessionsDir, ReadOnly: false})

	return paths
}

// resolveGitCommonDir returns the git common dir for the given cwd.
func resolveGitCommonDir(ctx context.Context, cwd string) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return ""
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(cwd, common)
	}
	return filepath.Clean(common)
}
