package agent

import (
	"context"
	"os/exec"
	"strings"

	"github.com/tta-lab/logos"
)

// BuildAllowedPaths returns the allowed paths for an agent running in cwd with given access.
// access is "rw" or "ro". CWD is always the first element (temenos uses first path as WorkingDir).
// additionalReadOnlyPaths are added as read-only paths (useful for granting cross-project read access).
func BuildAllowedPaths(cwd, access string, additionalReadOnlyPaths ...string) []logos.AllowedPath {
	readOnly := access != "rw"
	paths := []logos.AllowedPath{{Path: cwd, ReadOnly: readOnly}}

	gitDir := resolveGitCommonDir(cwd)
	if gitDir != "" && gitDir != cwd+"/.git" {
		paths = append(paths, logos.AllowedPath{Path: gitDir, ReadOnly: false})
	}

	for _, p := range additionalReadOnlyPaths {
		if p != cwd {
			paths = append(paths, logos.AllowedPath{Path: p, ReadOnly: true})
		}
	}

	return paths
}

// resolveGitCommonDir returns the git common dir for the given cwd.
func resolveGitCommonDir(cwd string) string {
	cmd := exec.CommandContext(context.Background(), "git", "-C", cwd, "rev-parse", "--git-common-dir")
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
