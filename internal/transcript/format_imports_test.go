package transcript

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// stdlib prefixes that are acceptable in format.go.
var stdlibPrefixes = []string{
	"unsafe",
	"builtin",
	"appengine",
	"golang.org/x/",
}

func TestFormatImportsStdlibOnly(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "format.go", nil, parser.ImportsOnly)
	require.NoError(t, err)

	for _, imp := range f.Imports {
		require.NotNil(t, imp.Path.Value, "import path missing")
		path := imp.Path.Value // quoted string, e.g. "\"fmt\""

		// Strip quotes
		path = path[1 : len(path)-1]

		isStdlib := false
		for _, prefix := range stdlibPrefixes {
			if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
				isStdlib = true
				break
			}
		}

		// Allow standard library packages (no dot in path means no third-party)
		// stdlib packages may have subpaths like "fmt", "time", "strings"
		// but "github.com/..." or "charm.land/..." are third-party
		hasDotSlash := len(path) > 0 && (path[0] == '.' || path[0] == '/')
		isThirdParty := hasDotSlash || (len(path) > 3 && (path[:3] == "github" || path[:3] == "git"))

		require.True(t, isStdlib || !isThirdParty,
			"format.go imports non-stdlib package %q; format.go must be stdlib-only for cmd/narrate (Phase 3)", path)
	}

	// Verify we actually have imports (sanity check)
	require.NotEmpty(t, f.Imports, "format.go should have at least one import")
}
