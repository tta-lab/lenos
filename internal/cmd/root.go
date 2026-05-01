package cmd

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	fang "charm.land/fang/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/tta-lab/lenos/internal/app"
	"github.com/tta-lab/lenos/internal/config"
	"github.com/tta-lab/lenos/internal/db"
	"github.com/tta-lab/lenos/internal/event"
	lenoslog "github.com/tta-lab/lenos/internal/log"
	"github.com/tta-lab/lenos/internal/session"
	"github.com/tta-lab/lenos/internal/tui"
	"github.com/tta-lab/lenos/internal/ui/common"
	"github.com/tta-lab/lenos/internal/version"
	"github.com/tta-lab/lenos/internal/workspace"
)

func init() {
	rootCmd.PersistentFlags().StringP("cwd", "c", "", "Current working directory")
	rootCmd.PersistentFlags().StringP("data-dir", "D", "", "Custom lenos data directory")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "Debug")
	rootCmd.Flags().BoolP("help", "h", false, "Help")
	rootCmd.Flags().BoolP("yolo", "y", false, "Automatically accept all permissions (dangerous mode)")
	rootCmd.Flags().StringP("session", "s", "", "Continue a previous session by ID")
	rootCmd.Flags().BoolP("continue", "C", false, "Continue the most recent session")
	rootCmd.Flags().StringP("agent", "a", "", "Agent identity file name (e.g. coder) to inject as context")
	rootCmd.Flags().StringArrayP("context-file", "f", nil, "Extra context file to inject at startup (repeatable)")
	rootCmd.MarkFlagsMutuallyExclusive("session", "continue")

	rootCmd.AddCommand(
		runCmd,
		dirsCmd,
		updateProvidersCmd,
		logsCmd,
		schemaCmd,
		loginCmd,
		statsCmd,
		sessionCmd,
	)
}

var rootCmd = &cobra.Command{
	Use:   "lenos",
	Short: "A terminal-first AI assistant for software development",
	Long:  "A glamorous, terminal-first AI assistant for software development and adjacent tasks",
	Example: `
# Run in interactive mode
lenos

# Run non-interactively
lenos run "Guess my 5 favorite Pokémon"

# Run a non-interactively with pipes and redirection
cat README.md | lenos run "make this more glamorous" > GLAMOROUS_README.md

# Run with debug logging in a specific directory
lenos --debug --cwd /path/to/project

# Run in yolo mode (auto-accept all permissions; use with care)
lenos --yolo

# Run with custom data directory
lenos --data-dir /path/to/custom/.lenos

# Continue a previous session
lenos --session {session-id}

# Continue the most recent session
lenos --continue
  `,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID, _ := cmd.Flags().GetString("session")
		continueLast, _ := cmd.Flags().GetBool("continue")
		agentName, _ := cmd.Flags().GetString("agent")
		contextFiles, _ := cmd.Flags().GetStringArray("context-file")
		if agentName == "" {
			agentName = os.Getenv("LENOS_AGENT")
		}
		if len(contextFiles) == 0 {
			if envVal := os.Getenv("LENOS_CONTEXT_FILE"); envVal != "" {
				contextFiles = []string{envVal}
			}
		}

		// Determine trigger message from positional args.
		triggerMessage := ""
		if len(args) > 0 {
			triggerMessage = strings.Join(args, " ")
		}

		ws, cleanup, err := setupWorkspaceWithProgressBar(cmd, agentName, contextFiles)
		if err != nil {
			return err
		}
		defer cleanup()

		if sessionID != "" {
			sess, err := resolveWorkspaceSessionID(cmd.Context(), ws, sessionID)
			if err != nil {
				return err
			}
			sessionID = sess.ID
		}

		event.AppInitialized()

		com := common.DefaultCommon(ws)
		model := tui.New(com, sessionID, continueLast, triggerMessage)

		var env uv.Environ = os.Environ()
		program := tea.NewProgram(
			model,
			tea.WithEnvironment(env),
			tea.WithContext(cmd.Context()),
		)
		go ws.Subscribe(program)

		if _, err := program.Run(); err != nil {
			event.Error(err)
			slog.Error("TUI run error", "error", err)
			return errors.New("Lenos crashed. If metrics are enabled, we were notified about it. If you'd like to report it, please copy the stacktrace above and open an issue at https://github.com/tta-lab/lenos/issues/new?template=bug.yml") //nolint:staticcheck
		}
		return nil
	},
}

var heartbit = lipgloss.NewStyle().Foreground(lipgloss.Color("#6b2d3e")).SetString(`
lenos
`)

// copied from cobra:
const defaultVersionTemplate = `{{with .DisplayName}}{{printf "%s " .}}{{end}}{{printf "version %s" .Version}}
`

func Execute() {
	// FIXME: config.Load uses slog internally during provider resolution,
	// but the file-based logger isn't set up until after config is loaded
	// (because the log path depends on the data directory from config).
	// This creates a window where slog calls in config.Load leak to
	// stderr. We discard early logs here as a workaround. The proper
	// fix is to remove slog calls from config.Load and have it return
	// warnings/diagnostics instead of logging them as a side effect.
	slog.SetDefault(slog.New(slog.DiscardHandler))

	// NOTE: very hacky: we create a colorprofile writer with STDOUT, then make
	// it forward to a bytes.Buffer, write the colored heartbit to it, and then
	// finally prepend it in the version template.
	// Unfortunately cobra doesn't give us a way to set a function to handle
	// printing the version, and PreRunE runs after the version is already
	// handled, so that doesn't work either.
	// This is the only way I could find that works relatively well.
	if term.IsTerminal(os.Stdout.Fd()) {
		var b bytes.Buffer
		w := colorprofile.NewWriter(os.Stdout, os.Environ())
		w.Forward = &b
		_, _ = w.WriteString(heartbit.String())
		rootCmd.SetVersionTemplate(b.String() + "\n" + defaultVersionTemplate)
	}
	if err := fang.Execute(
		context.Background(),
		rootCmd,
		fang.WithVersion(version.Version),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}

// supportsProgressBar tries to determine whether the current terminal supports
// progress bars by looking into environment variables.
func supportsProgressBar() bool {
	if !term.IsTerminal(os.Stderr.Fd()) {
		return false
	}
	termProg := os.Getenv("TERM_PROGRAM")
	_, isWindowsTerminal := os.LookupEnv("WT_SESSION")

	return isWindowsTerminal || strings.Contains(strings.ToLower(termProg), "ghostty")
}

// setupWorkspaceWithProgressBar wraps setupWorkspace with an optional
// terminal progress bar shown during initialization.
func setupWorkspaceWithProgressBar(cmd *cobra.Command, agentName string, contextFiles []string) (workspace.Workspace, func(), error) {
	showProgress := supportsProgressBar()
	if showProgress {
		_, _ = fmt.Fprintf(os.Stderr, ansi.SetIndeterminateProgressBar)
	}

	ws, cleanup, err := setupWorkspace(cmd, agentName, contextFiles)

	if showProgress {
		_, _ = fmt.Fprintf(os.Stderr, ansi.ResetProgressBar)
	}

	return ws, cleanup, err
}

// setupWorkspace creates an in-process app.App and wraps it in an
// AppWorkspace.
func setupWorkspace(cmd *cobra.Command, agentName string, contextFiles []string) (workspace.Workspace, func(), error) {
	debug, _ := cmd.Flags().GetBool("debug")
	dataDir, _ := cmd.Flags().GetString("data-dir")
	ctx := cmd.Context()

	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return nil, nil, err
	}

	store, err := config.Init(cwd, dataDir, debug)
	if err != nil {
		return nil, nil, err
	}

	cfg := store.Config()

	// Resolve agent identity file if specified.
	if agentName != "" {
		agentContextFile, resolveErr := resolveAgentFile(agentName, cfg.Options.AgentPaths)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		store.Overrides().AgentName = agentName
		store.Overrides().AgentContextFile = agentContextFile
	}

	// Validate and store extra context files.
	for _, cf := range contextFiles {
		if _, statErr := os.Stat(cf); statErr != nil {
			return nil, nil, fmt.Errorf("context file not found: %s: %w", cf, statErr)
		}
		// Store extra context files in overrides; applied in SetupAgents.
		store.Overrides().ExtraContextFiles = append(store.Overrides().ExtraContextFiles, cf)
	}

	// Re-run SetupAgents now that overrides are set.
	store.SetupAgents()

	if err := os.MkdirAll(cfg.Options.DataDirectory, 0o700); err != nil {
		return nil, nil, fmt.Errorf("failed to create data directory: %q %w", cfg.Options.DataDirectory, err)
	}

	gitIgnorePath := filepath.Join(cfg.Options.DataDirectory, ".gitignore")
	if _, err := os.Stat(gitIgnorePath); os.IsNotExist(err) {
		if err := os.WriteFile(gitIgnorePath, []byte("*\n"), 0o644); err != nil {
			return nil, nil, fmt.Errorf("failed to create .gitignore file: %q %w", gitIgnorePath, err)
		}
	}

	conn, err := db.Connect(ctx, cfg.Options.DataDirectory)
	if err != nil {
		return nil, nil, err
	}

	logFile := filepath.Join(cfg.Options.DataDirectory, "logs", "lenos.log")
	lenoslog.Setup(logFile, debug)

	appInstance, err := app.New(ctx, conn, store)
	if err != nil {
		_ = conn.Close()
		slog.Error("Failed to create app instance", "error", err)
		return nil, nil, err
	}

	if shouldEnableMetrics(cfg) {
		event.Init()
	}

	ws := workspace.NewAppWorkspace(appInstance, store)
	cleanup := func() { appInstance.Shutdown() }
	return ws, cleanup, nil
}

func shouldEnableMetrics(cfg *config.Config) bool {
	if v, _ := strconv.ParseBool(os.Getenv("LENOS_DISABLE_METRICS")); v {
		return false
	}
	if v, _ := strconv.ParseBool(os.Getenv("DO_NOT_TRACK")); v {
		return false
	}
	if cfg.Options.DisableMetrics {
		return false
	}
	return true
}

func MaybePrependStdin(prompt string) (string, error) {
	if term.IsTerminal(os.Stdin.Fd()) {
		return prompt, nil
	}
	fi, err := os.Stdin.Stat()
	if err != nil {
		return prompt, err
	}
	// Check if stdin is a named pipe ( | ) or regular file ( < ).
	if fi.Mode()&os.ModeNamedPipe == 0 && !fi.Mode().IsRegular() {
		return prompt, nil
	}
	bts, err := io.ReadAll(os.Stdin)
	if err != nil {
		return prompt, err
	}
	return string(bts) + "\n\n" + prompt, nil
}

// resolveWorkspaceSessionID resolves a session ID that may be a full
// UUID, full hash, or hash prefix. Works against the Workspace
// interface so both local and client/server paths get hash prefix
// support.
func resolveWorkspaceSessionID(ctx context.Context, ws workspace.Workspace, id string) (session.Session, error) {
	if sess, err := ws.GetSession(ctx, id); err == nil {
		return sess, nil
	}

	sessions, err := ws.ListSessions(ctx)
	if err != nil {
		return session.Session{}, err
	}

	var matches []session.Session
	for _, s := range sessions {
		hash := session.HashID(s.ID)
		if hash == id || strings.HasPrefix(hash, id) {
			matches = append(matches, s)
		}
	}

	switch len(matches) {
	case 0:
		return session.Session{}, fmt.Errorf("session not found: %s", id)
	case 1:
		return matches[0], nil
	default:
		return session.Session{}, fmt.Errorf("session ID %q is ambiguous (%d matches)", id, len(matches))
	}
}

func ResolveCwd(cmd *cobra.Command) (string, error) {
	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd != "" {
		err := os.Chdir(cwd)
		if err != nil {
			return "", fmt.Errorf("failed to change directory: %v", err)
		}
		return cwd, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %v", err)
	}
	return cwd, nil
}

// resolveAgentFile searches agent_paths for an agent.md file matching the given name.
// Returns the absolute path of the first match or an error.
func resolveAgentFile(agentName string, agentPaths []string) (string, error) {
	filename := agentName + ".md"
	var searched []string
	for _, dir := range agentPaths {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, filename)
		searched = append(searched, dir)
		if _, err := os.Stat(path); err == nil {
			absPath, err := filepath.Abs(path)
			if err != nil {
				return "", fmt.Errorf("resolving agent path: %w", err)
			}
			return absPath, nil
		}
	}
	return "", fmt.Errorf("agent file %q not found in agent_paths: %v", filename, searched)
}

func createDotLenosDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create data directory: %q %w", dir, err)
	}

	gitIgnorePath := filepath.Join(dir, ".gitignore")
	content, err := os.ReadFile(gitIgnorePath)

	// create or update if old version
	if os.IsNotExist(err) || string(content) == oldGitIgnore {
		if err := os.WriteFile(gitIgnorePath, []byte(defaultGitIgnore), 0o644); err != nil {
			return fmt.Errorf("failed to create .gitignore file: %q %w", gitIgnorePath, err)
		}
	}

	return nil
}

//go:embed gitignore/old
var oldGitIgnore string

//go:embed gitignore/default
var defaultGitIgnore string
