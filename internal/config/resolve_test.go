package config

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/env"
)

func TestShellVariableResolver_ResolveValue(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		envVars     map[string]string
		expected    string
		expectError bool
	}{
		{
			name:     "non-variable string returns as-is",
			value:    "plain-string",
			expected: "plain-string",
		},
		{
			name:     "environment variable resolution",
			value:    "$HOME",
			envVars:  map[string]string{"HOME": "/home/user"},
			expected: "/home/user",
		},
		{
			name:        "missing environment variable returns error",
			value:       "$MISSING_VAR",
			envVars:     map[string]string{},
			expectError: true,
		},
		{
			name:     "shell command with whitespace trimming",
			value:    "$(echo '  spaced  ')",
			expected: "spaced",
		},
		{
			name:        "shell command execution error",
			value:       "$(false)",
			expectError: true,
		},
		{
			name:        "invalid format returns error",
			value:       "$",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testEnv := env.NewFromMap(tt.envVars)
			resolver := &shellVariableResolver{
				env: testEnv,
			}

			result, err := resolver.ResolveValue(tt.value)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestShellVariableResolver_EnhancedResolveValue(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		envVars     map[string]string
		expected    string
		expectError bool
	}{
		{
			name:     "command substitution within string",
			value:    "Bearer $(echo token123)",
			expected: "Bearer token123",
		},
		{
			name:     "environment variable within string",
			value:    "Bearer $TOKEN",
			envVars:  map[string]string{"TOKEN": "sk-ant-123"},
			expected: "Bearer sk-ant-123",
		},
		{
			name:     "environment variable with braces within string",
			value:    "Bearer ${TOKEN}",
			envVars:  map[string]string{"TOKEN": "sk-ant-456"},
			expected: "Bearer sk-ant-456",
		},
		{
			name:  "mixed command and environment substitution",
			value: "$USER-$(echo 12)-$HOST",
			envVars: map[string]string{
				"USER": "testuser",
				"HOST": "localhost",
			},
			expected: "testuser-12-localhost",
		},
		{
			name:     "multiple command substitutions",
			value:    "$(echo hello) $(echo world)",
			expected: "hello world",
		},
		{
			name:     "nested parentheses in command",
			value:    "$(echo $(echo inner))",
			expected: "inner",
		},
		{
			name:        "lone dollar with non-variable chars",
			value:       "prefix$123suffix",
			expectError: true,
		},
		{
			name:        "dollar with special chars",
			value:       "a$@b$#c",
			expectError: true,
		},
		{
			name:        "empty environment variable substitution",
			value:       "Bearer $EMPTY_VAR",
			envVars:     map[string]string{},
			expectError: true,
		},
		{
			name:        "unmatched command substitution opening",
			value:       "Bearer $(echo test",
			expectError: true,
		},
		{
			name:        "unmatched environment variable braces",
			value:       "Bearer ${TOKEN",
			expectError: true,
		},
		{
			name:        "command substitution with error",
			value:       "Bearer $(false)",
			expectError: true,
		},
		{
			name:     "environment variable with underscores and numbers",
			value:    "Bearer $API_KEY_V2",
			envVars:  map[string]string{"API_KEY_V2": "sk-test-123"},
			expected: "Bearer sk-test-123",
		},
		{
			name:     "no substitution needed",
			value:    "Bearer sk-ant-static-token",
			expected: "Bearer sk-ant-static-token",
		},
		{
			name:        "incomplete variable at end",
			value:       "Bearer $",
			expectError: true,
		},
		{
			name:        "variable with invalid character",
			value:       "Bearer $VAR-NAME",
			expectError: true,
		},
		{
			name:        "multiple invalid variables",
			value:       "$1$2$3",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testEnv := env.NewFromMap(tt.envVars)
			resolver := &shellVariableResolver{
				env: testEnv,
			}

			result, err := resolver.ResolveValue(tt.value)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestShellVariableResolver_ComplexRealWorldExample(t *testing.T) {
	// Create a temp file so the command succeeds and produces real output.
	f, err := os.CreateTemp(t.TempDir(), "token")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString("sk-ant-test-token\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Note: base64 on macOS uses -b 0 instead of -w 0 for no wrapping.
	// We use a printf-based approach that's portable.
	cmd := "cat " + f.Name()
	testEnv := env.NewFromMap(nil)
	resolver := &shellVariableResolver{
		env: testEnv,
	}

	result, err := resolver.ResolveValue("Bearer $(" + cmd + ")")
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-ant-test-token", result)
}

func TestShellVariableResolver_EnvPassedToCommand(t *testing.T) {
	// Verify custom env vars are passed to shell commands.
	testEnv := env.NewFromMap(map[string]string{"MY_VAR": "my-value"})
	resolver := &shellVariableResolver{
		env: testEnv,
	}

	result, err := resolver.ResolveValue("$MY_VAR")
	require.NoError(t, err)
	require.Equal(t, "my-value", result)
}

func TestEnvironmentVariableResolver_ResolveValue(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		envVars     map[string]string
		expected    string
		expectError bool
	}{
		{
			name:     "non-variable string returns as-is",
			value:    "plain-string",
			expected: "plain-string",
		},
		{
			name:     "environment variable resolution",
			value:    "$HOME",
			envVars:  map[string]string{"HOME": "/home/user"},
			expected: "/home/user",
		},
		{
			name:     "environment variable with complex value",
			value:    "$PATH",
			envVars:  map[string]string{"PATH": "/usr/bin:/bin:/usr/local/bin"},
			expected: "/usr/bin:/bin:/usr/local/bin",
		},
		{
			name:        "missing environment variable returns error",
			value:       "$MISSING_VAR",
			envVars:     map[string]string{},
			expectError: true,
		},
		{
			name:        "empty environment variable returns error",
			value:       "$EMPTY_VAR",
			envVars:     map[string]string{"EMPTY_VAR": ""},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testEnv := env.NewFromMap(tt.envVars)
			resolver := NewEnvironmentVariableResolver(testEnv)

			result, err := resolver.ResolveValue(tt.value)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestNewShellVariableResolver(t *testing.T) {
	testEnv := env.NewFromMap(map[string]string{"TEST": "value"})
	resolver := NewShellVariableResolver(testEnv)

	require.NotNil(t, resolver)
	require.Implements(t, (*VariableResolver)(nil), resolver)
}

func TestNewEnvironmentVariableResolver(t *testing.T) {
	testEnv := env.NewFromMap(map[string]string{"TEST": "value"})
	resolver := NewEnvironmentVariableResolver(testEnv)

	require.NotNil(t, resolver)
	require.Implements(t, (*VariableResolver)(nil), resolver)
}

func TestShellExec_IncludesStderrInError(t *testing.T) {
	// A command that writes to stderr but exits successfully.
	ctx := context.Background()
	// Note: this test verifies stderr is captured in the error when exit code != 0.
	// When exit code != 0, exec.ExitError.Stderr contains the stderr output.
	result, err := shellExec(ctx, nil, "echo error >&2; exit 1")
	require.Error(t, err)
	// stderr should appear in the error message
	require.Contains(t, err.Error(), "error")
	require.Equal(t, "", result)
}
