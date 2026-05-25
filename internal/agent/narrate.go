package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/tta-lab/temenos/client"

	"github.com/tta-lab/lenos/internal/message"
)

const (
	narrateDirEnv       = "LENOS_NARRATE_DIR"
	narrateDefaultToEnv = "LENOS_NARRATE_DEFAULT_TO"
)

type narrateInvocation struct {
	bash    string
	env     map[string]string
	paths   []client.AllowedPath
	dir     string
	cleanup func()
}

func newNarrateInvocation(emit string, env map[string]string, paths []client.AllowedPath, defaultTo string) (narrateInvocation, error) {
	dir, err := os.MkdirTemp("", "lenos-narrate-*")
	if err != nil {
		return narrateInvocation{}, err
	}

	runEnv := cloneStringMap(env)
	runEnv[narrateDirEnv] = dir
	if strings.TrimSpace(defaultTo) != "" {
		runEnv[narrateDefaultToEnv] = strings.TrimSpace(defaultTo)
	}

	return narrateInvocation{
		bash:    narrateShellPrelude + "\n" + emit,
		env:     runEnv,
		paths:   withTempWritePath(paths),
		dir:     dir,
		cleanup: func() { _ = os.RemoveAll(dir) },
	}, nil
}

const narrateShellPrelude = `LENOS_NARRATE_SEQ=0
narrate() {
  local to=""
  local continue_loop=0
  while [ "$#" -gt 0 ]; do
    case "${1:-}" in
      --to)
        if [ "$#" -lt 2 ] || [ -z "${2:-}" ]; then
          printf '%s\n' "narrate: --to requires an addressee" >&2
          return 2
        fi
        to="${2:-}"
        shift 2
        ;;
      --continue)
        continue_loop=1
        shift
        ;;
      --)
        shift
        break
        ;;
      *)
        break
        ;;
    esac
  done
  if [ "$#" -ne 0 ]; then
    printf '%s\n' "narrate: message body must be provided on stdin; use a heredoc" >&2
    return 2
  fi
  if [ -z "$to" ] && [ -n "${LENOS_NARRATE_DEFAULT_TO:-}" ]; then
    to="${LENOS_NARRATE_DEFAULT_TO:-}"
  fi

  LENOS_NARRATE_SEQ=$((LENOS_NARRATE_SEQ + 1))
  local prefix event_dir
  prefix="$(printf '%06d' "$LENOS_NARRATE_SEQ")"
  event_dir="$(mktemp -d "$LENOS_NARRATE_DIR/${prefix}.XXXXXX")" || return 1

  if [ -n "$to" ]; then
    printf '%s' "$to" > "$event_dir/to" || return 1
  fi
  if [ "$continue_loop" = "1" ]; then
    printf '%s' "1" > "$event_dir/continue" || return 1
  fi
  cat > "$event_dir/body" || return 1
  if [ ! -s "$event_dir/body" ]; then
    rm -rf "$event_dir"
    printf '%s\n' "narrate: empty message body" >&2
    return 2
  fi
}`

func readNarrationEvents(dir string) ([]message.CommandNarration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	narrations := make([]message.CommandNarration, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		eventDir := filepath.Join(dir, entry.Name())
		body, err := os.ReadFile(filepath.Join(eventDir, "body"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		narration := message.CommandNarration{Body: string(body)}
		to, err := os.ReadFile(filepath.Join(eventDir, "to"))
		if err == nil {
			narration.To = strings.TrimSpace(string(to))
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(eventDir, "continue")); err == nil {
			narration.Continue = true
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		narrations = append(narrations, narration)
	}

	return narrations, nil
}

func deliverNarrations(
	ctx context.Context,
	runner Runner,
	env map[string]string,
	paths []client.AllowedPath,
	narrations []message.CommandNarration,
) ([]message.CommandNarration, bool) {
	if len(narrations) == 0 {
		return narrations, false
	}

	out := make([]message.CommandNarration, len(narrations))
	copy(out, narrations)

	var failed bool
	for i := range out {
		if strings.TrimSpace(out[i].To) == "" {
			continue
		}
		res := runner.Run(ctx, ttalSendCommand(out[i].To, out[i].Body), env, paths)
		exitCode := res.ExitCode
		out[i].DeliveryExitCode = &exitCode
		out[i].DeliveryOutput = string(combine(res.Stdout, res.Stderr))
		if res.Err != nil && out[i].DeliveryOutput == "" {
			out[i].DeliveryOutput = res.Err.Error()
		}
		if res.Err != nil || res.ExitCode != 0 {
			failed = true
		}
	}

	return out, failed
}

func ttalSendCommand(to, body string) string {
	delimiter := narrateHeredocDelimiter(body)
	command := "cat <<'" + delimiter + "' | ttal send --to " + shellQuote(to) + "\n"
	if strings.HasSuffix(body, "\n") {
		return command + body + delimiter
	}
	return command + body + "\n" + delimiter
}

func appendNarrationObservation(body string, narrations []message.CommandNarration) string {
	if len(narrations) == 0 {
		return body
	}

	lines := make([]string, 0, len(narrations))
	for _, narration := range narrations {
		if narration.To == "" {
			line := "narration rendered to user"
			if narration.Continue {
				line += "; continue requested"
			}
			lines = append(lines, line+"; body omitted from result replay")
			continue
		}
		to := html.EscapeString(narration.To)
		if narration.DeliveryExitCode != nil && *narration.DeliveryExitCode != 0 {
			line := fmt.Sprintf("narration delivery failed for %s (exit code: %d)",
				to, *narration.DeliveryExitCode)
			if narration.Continue {
				line += "; continue requested"
			}
			line += "; body omitted from result replay"
			if narration.DeliveryOutput != "" {
				line += "\nDELIVERY OUTPUT:\n" + html.EscapeString(narration.DeliveryOutput)
			}
			lines = append(lines, line)
			continue
		}
		line := "narration delivered to " + to
		if narration.Continue {
			line += "; continue requested"
		}
		lines = append(lines, line+"; body omitted from result replay")
	}

	return body + "\n" + strings.Join(lines, "\n")
}

func shouldStopAfterNarration(narrations []message.CommandNarration, exitCode int, deliveryFailed bool) bool {
	if len(narrations) == 0 || exitCode != 0 || deliveryFailed {
		return false
	}
	for _, narration := range narrations {
		if narration.Continue {
			return false
		}
	}
	return true
}

func narrateCommandForBody(body string) string {
	delimiter := narrateHeredocDelimiter(body)
	if strings.HasSuffix(body, "\n") {
		return "cat <<'" + delimiter + "' | narrate\n" + body + delimiter
	}
	return "cat <<'" + delimiter + "' | narrate\n" + body + "\n" + delimiter
}

func narrateHeredocDelimiter(body string) string {
	for i := 0; i < 16; i++ {
		var b [4]byte
		if _, err := rand.Read(b[:]); err == nil {
			delimiter := "LENOS_NARRATE_EOF_" + strings.ToUpper(hex.EncodeToString(b[:]))
			if !bodyHasDelimiterLine(body, delimiter) {
				return delimiter
			}
		}
	}
	for i := 0; ; i++ {
		delimiter := fmt.Sprintf("LENOS_NARRATE_EOF_FALLBACK_%d", i)
		if !bodyHasDelimiterLine(body, delimiter) {
			return delimiter
		}
	}
}

func bodyHasDelimiterLine(body, delimiter string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if line == delimiter {
			return true
		}
	}
	return false
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src)+1)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func withTempWritePath(paths []client.AllowedPath) []client.AllowedPath {
	tmp := filepath.Clean(os.TempDir())
	for _, p := range paths {
		if filepath.Clean(p.Path) == tmp && !p.ReadOnly {
			return paths
		}
	}
	out := make([]client.AllowedPath, 0, len(paths)+1)
	out = append(out, paths...)
	out = append(out, client.AllowedPath{Path: tmp, ReadOnly: false})
	return out
}
