package agent

import (
	"context"
	"os/exec"
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

// BuildAllowedPaths returns the allowed paths for an agent running in cwd with given access.
// access is AccessModeRW or AccessModeRO. CWD is always the first element (temenos uses first path as WorkingDir).
// additionalReadOnlyPaths are added as read-only paths (useful for granting cross-project read access).
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
