package tools

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// webCLIPath caches the path to the web CLI once found.
var webCLIPath string

// findWebCLI looks for the web CLI in PATH and caches the result.
func findWebCLI() string {
	if webCLIPath != "" {
		return webCLIPath
	}

	path, err := exec.LookPath("web")
	if err != nil {
		webCLIPath = ""
		return ""
	}

	webCLIPath = path
	return webCLIPath
}

// webCLIAvailable checks if the web CLI is available.
func webCLIAvailable() bool {
	return findWebCLI() != ""
}

// tryWebSearch attempts to search using the web CLI and returns the result.
// Returns an empty string if the web CLI is not available or fails.
func tryWebSearch(ctx context.Context, query string) string {
	path := findWebCLI()
	if path == "" {
		return ""
	}

	cmd := exec.CommandContext(ctx, path, "search", query)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return ""
	}

	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return ""
	}

	return result
}

// tryWebFetch attempts to fetch a URL using the web CLI and returns the content.
// Returns an empty string if the web CLI is not available or fails.
func tryWebFetch(ctx context.Context, url string) string {
	path := findWebCLI()
	if path == "" {
		return ""
	}

	cmd := exec.CommandContext(ctx, path, "fetch", url)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return ""
	}

	result := strings.TrimSpace(stdout.String())
	if result == "" {
		return ""
	}

	return result
}

// isContextCanceled checks if the error is due to context cancellation.
func isContextCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}
