package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/tta-lab/lenos/internal/config"
)

// bashOutputDir returns the managed output directory for bounded bash output.
func bashOutputDir(dataDir string) string {
	return filepath.Join(dataDir, "bash-output")
}

// boundedOutput holds the result of applying bash output bounding.
type boundedOutput struct {
	Preview  string // The model-visible preview (may be truncated).
	FullPath string // Path to the full output file, empty if no truncation.
}

// boundOutput applies line and byte limits to stdout. If the output
// exceeds either limit, it saves the full content to a managed file under
// .lenos/bash-output/ and returns a tail-biased preview plus the file path.
// If limits are nil or output fits, it returns the full output unchanged.
func boundOutput(stdout []byte, limits *config.BashOutputConfig, dataDir string) boundedOutput {
	if limits == nil {
		return boundedOutput{Preview: string(stdout)}
	}

	maxLines := limits.MaxLines
	maxBytes := limits.MaxBytes
	if maxLines <= 0 {
		maxLines = 2000
	}
	if maxBytes <= 0 {
		maxBytes = 51200
	}

	lines := countLines(stdout)
	byteLen := len(stdout)

	if lines <= maxLines && byteLen <= maxBytes {
		return boundedOutput{Preview: string(stdout)}
	}

	// Save full output using a temp file for atomic, collision-free naming.
	dir := bashOutputDir(dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return boundedOutput{Preview: string(stdout) + fmt.Sprintf("\n\nFailed to save full output: %v", err)}
	}

	f, err := os.CreateTemp(dir, "bash_*.log")
	if err != nil {
		return boundedOutput{Preview: string(stdout) + fmt.Sprintf("\n\nFailed to save full output: %v", err)}
	}
	fullPath := f.Name()
	if _, err := f.Write(stdout); err != nil {
		f.Close()
		return boundedOutput{Preview: string(stdout) + fmt.Sprintf("\n\nFailed to save full output: %v", err)}
	}
	if err := f.Close(); err != nil {
		return boundedOutput{Preview: string(stdout) + fmt.Sprintf("\n\nFailed to save full output: %v", err)}
	}

	preview := buildTailBiasedPreview(stdout, maxLines, maxBytes)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "...bash output truncated (%d lines, %d bytes)...\n", lines, byteLen)
	fmt.Fprintf(&buf, "Full output saved to: %s\n", fullPath)
	fmt.Fprintf(&buf, "Use grep on this file to search for relevant errors, or use tail/read with offsets to inspect specific sections. Do not rerun the command just to recover hidden output.\n")
	buf.WriteString(preview)

	return boundedOutput{
		Preview:  buf.String(),
		FullPath: fullPath,
	}
}

// buildTailBiasedPreview extracts a tail-biased UTF-8-safe preview.
func buildTailBiasedPreview(content []byte, maxLines, maxBytes int) string {
	// Use roughly 80% of limits for the preview.
	previewLines := maxLines * 80 / 100
	if previewLines < 3 {
		previewLines = 3
	}
	previewBytes := maxBytes * 80 / 100
	if previewBytes < 512 {
		previewBytes = 512
	}

	lineStarts := findLineStarts(content)
	if len(lineStarts) <= previewLines {
		return truncateToValidUTF8(content, previewBytes)
	}

	// Tail-biased: take the last previewLines lines.
	tailStart := lineStarts[len(lineStarts)-previewLines]
	tail := content[tailStart:]

	return truncateToValidUTF8(tail, previewBytes)
}

// findLineStarts returns offsets of each line start in content.
func findLineStarts(content []byte) []int {
	if len(content) == 0 {
		return []int{0}
	}
	starts := []int{0}
	for i, b := range content {
		if b == '\n' && i+1 < len(content) {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// countLines counts lines in content.
func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := 0
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	// If content doesn't end with newline, count the last line.
	if len(content) > 0 && content[len(content)-1] != '\n' {
		count++
	}
	return count
}

// truncateToValidUTF8 truncates content to at most maxBytes bytes without
// splitting a multi-byte UTF-8 sequence.
func truncateToValidUTF8(content []byte, maxBytes int) string {
	if len(content) <= maxBytes {
		return string(content)
	}
	// Walk backward from maxBytes to find a valid UTF-8 boundary.
	for i := maxBytes; i > 0; i-- {
		if utf8.RuneStart(content[i]) {
			return string(content[:i])
		}
	}
	return string(content[:maxBytes])
}
