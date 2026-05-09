package transcript

import (
	"fmt"
	"strings"

	"github.com/zeebo/xxh3"
)

// BlockKind classifies a transcript block by its role in the .md format.
type BlockKind int

const (
	BlockUnknown   BlockKind = iota
	BlockUserMsg             // **λ** user message
	BlockBashCmd             // ```bash ... ``` agent emission
	BlockOutput              // ``` ... ``` command output
	BlockTrailer             // *[HH:MM:SS, Xs]* — duration footer
	BlockTurnEnd             // *(turn ended)*
	BlockRuntime             // > *runtime: ...*
	BlockProse               // free prose between fences
	BlockMdMessage           // :md protocol message — first line starts with :md
)

// Block is one navigable unit of the transcript: the body the user can
// select, copy, and (eventually) highlight in the chat list.
//
// Source is the raw markdown slice (verbatim — what `RawRender` returns).
// Rendered is the Glamour-styled version (what list.List displays). Width
// is captured so the cache key is unambiguous; consumers re-render when
// the layout width changes.
type Block struct {
	Kind     BlockKind
	Source   string // raw .md text (no surrounding blank lines)
	Rendered string // pre-rendered output for display
	Width    int    // width Rendered was computed at; 0 means unset
}

// ID returns a stable identifier for the block — xxh3 of (kind|source).
// Kind is included so a bash block and output block with the same body
// (rare but possible: empty fences) don't collide.
func (b Block) ID() string {
	h := xxh3.New()
	_, _ = fmt.Fprintf(h, "%d|", b.Kind)
	_, _ = h.WriteString(b.Source)
	return fmt.Sprintf("blk-%016x", h.Sum64())
}

// SplitBlocks walks the .md transcript and emits one Block per logical unit.
// It does not invoke Glamour — callers render each Block's Source as needed
// (typically once at SetMessages time, cached on the *mdBlockItem).
//
// lenos-bash composite rule: when a ```lenos-bash ... ``` fence closes,
// subsequent non-blank blocks are absorbed into the same composite Block
// (their Source appended) until we hit a boundary: another ```lenos-bash
// fence, a **λ** user message, a > *runtime: event, or *(turn ended)*.
// Legacy ```bash fences (old sessions) do NOT composite-absorb — each gets
// its own BlockBashCmd item.
func SplitBlocks(body []byte) []Block {
	if len(body) == 0 {
		return nil
	}
	src := string(body)

	var blocks []Block
	var current []string
	currentInFence := false
	inLenosFence := false // true when the current fence opened with lenos-bash
	compositing := false  // true when absorbing blocks into a lenos-bash composite

	flush := func() {
		if len(current) == 0 {
			return
		}
		text := strings.TrimRight(strings.Join(current, "\n"), "\n")
		if strings.TrimSpace(text) == "" {
			current = current[:0]
			return
		}
		blocks = append(blocks, Block{
			Kind:   classifyBlock(text),
			Source: text,
		})
		current = current[:0]
	}

	absorbIntoComposite := func() {
		text := strings.TrimRight(strings.Join(current, "\n"), "\n")
		if strings.TrimSpace(text) == "" {
			current = current[:0]
			return
		}
		kind := classifyBlock(text)
		if compositing && isCompositeBoundary(kind, text) {
			compositing = false
			flush()
			return
		}
		if compositing && len(blocks) > 0 {
			last := &blocks[len(blocks)-1]
			last.Source += "\n\n" + text
			current = current[:0]
			return
		}
		flush()
	}

	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") {
			currentInFence = !currentInFence
			current = append(current, line)
			if len(current) == 1 && strings.HasPrefix(strings.TrimSpace(line), "```") {
				// Opening fence — detect if it's lenos-bash.
				if isLenosBashFence(line) {
					inLenosFence = true
				}
			}
			if !currentInFence {
				// Closing fence.
				if inLenosFence {
					flush()
					compositing = true
					inLenosFence = false
				} else {
					absorbIntoComposite()
				}
			}
			continue
		}
		if !currentInFence && strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				absorbIntoComposite()
			}
			current = current[:0]
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		absorbIntoComposite()
	}
	return blocks
}

// MdPrefix is the first line prefix that identifies an :md protocol message.
// Bare `:md` sends to the session owner; `:md @agent` sends to a named agent.
const MdPrefix = ":md"

// StripMdPrefixLine removes the first line from a block's source if it
// starts with the :md prefix. Returns the body text after the :md line.
// Returns "" for a bare `:md` line with no body. If the source doesn't start
// with :md, returns source unchanged.
func StripMdPrefixLine(source string) string {
	first, rest, found := strings.Cut(source, "\n")
	if !found {
		// Single line with only the :md prefix — empty body.
		if strings.HasPrefix(strings.TrimSpace(first), MdPrefix) {
			return ""
		}
		return source
	}
	if strings.HasPrefix(strings.TrimSpace(first), MdPrefix) {
		return strings.TrimLeft(rest, "\n")
	}
	return source
}

// ParseMdAddressee extracts the agent name after `:md @` from the first line
// of an :md protocol message. Returns "" for bare `:md` (routed to owner),
// for text without an `:md` prefix, or for `:md @` with no name.
//
// Used by both classifyBlock() (to confirm :md classification) and the chat
// renderer's linePrefix() (to display the addressee).
func ParseMdAddressee(text string) string {
	first := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		first = text[:i]
	}
	first = strings.TrimSpace(first)

	if first == MdPrefix {
		return "" // bare :md → owner
	}
	// Check for :md @agent-name
	prefix := MdPrefix + " @"
	if strings.HasPrefix(first, prefix) {
		after := strings.TrimPrefix(first, prefix)
		// Take only the first word (agent name)
		if i := strings.IndexAny(after, " \t"); i >= 0 {
			after = after[:i]
		}
		if after == "" {
			return ""
		}
		return after
	}
	return ""
}

// classifyBlock inspects the first / last lines of a block's raw text to
// decide what kind of block it is.
func classifyBlock(text string) BlockKind {
	first := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		first = text[:i]
	}
	first = strings.TrimSpace(first)

	switch {
	case strings.HasPrefix(first, MdPrefix):
		// :md (bare) or :md @agent — the first line starts with :md.
		// Broad match (any text starting with :md) because the protocol
		// only cares about the leading prefix — ":md to owner" and ":md @agent"
		// are the two valid forms. Check fires before LambdaMsgPrefix since
		// :md has no ** wrapper.
		return BlockMdMessage
	case strings.HasPrefix(first, LambdaMsgPrefix):
		return BlockUserMsg
	case isLenosBashFence(first):
		return BlockBashCmd
	case strings.HasPrefix(first, "```bash"), strings.HasPrefix(first, "``` bash"):
		return BlockBashCmd
	case strings.HasPrefix(first, "```"):
		return BlockOutput
	case strings.HasPrefix(first, "*[") && strings.Contains(first, "s]*"):
		return BlockTrailer
	case first == "*(turn ended)*":
		return BlockTurnEnd
	case strings.HasPrefix(first, "> *runtime:"):
		return BlockRuntime
	default:
		return BlockProse
	}
}

// isLenosBashFence reports whether the given line opens (or closes) a
// lenos-bash fence. Accepts the canonical `\`\`\`lenos-bash` form and the
// space-separated `\`\`\` lenos-bash` form so legacy or hand-edited
// transcripts still parse. Single source of truth for fence detection so
// the parser's three call sites can't drift.
func isLenosBashFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```lenos-bash") ||
		strings.HasPrefix(trimmed, "``` lenos-bash")
}

// isCompositeBoundary reports whether the given block (text + classified
// kind) terminates an in-progress lenos-bash composite. Boundaries: a new
// turn (user msg), a runtime event, a turn-end marker, or another
// lenos-bash fence — anything that says "this isn't the previous cmd's
// output anymore."
func isCompositeBoundary(kind BlockKind, text string) bool {
	switch kind {
	case BlockUserMsg, BlockRuntime, BlockTurnEnd:
		return true
	case BlockBashCmd:
		first := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			first = text[:i]
		}
		return isLenosBashFence(first)
	default:
		return false
	}
}
