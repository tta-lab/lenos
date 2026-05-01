package tui

import (
	"fmt"
	"strings"

	"github.com/zeebo/xxh3"
)

// BlockKind classifies a transcript block by its role in the .md format.
type BlockKind int

const (
	BlockUnknown BlockKind = iota
	BlockUserMsg           // **λ** user message
	BlockBashCmd           // ```bash ... ``` agent emission
	BlockOutput            // ``` ... ``` command output
	BlockTrailer           // *[HH:MM:SS, Xs]* — duration footer
	BlockTurnEnd           // *(turn ended)*
	BlockRuntime           // > *runtime: ...*
	BlockProse             // narrate output / free prose between fences
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
	_, _ = h.WriteString(fmt.Sprintf("%d|", b.Kind))
	_, _ = h.WriteString(b.Source)
	return fmt.Sprintf("blk-%016x", h.Sum64())
}

// SplitBlocks walks the .md transcript and emits one Block per logical unit.
// It does not invoke Glamour — callers render each Block's Source as needed
// (typically once at SetMessages time, cached on the *mdBlockItem).
func SplitBlocks(body []byte) []Block {
	if len(body) == 0 {
		return nil
	}
	src := string(body)

	// Preserve the original line layout but split on runs of blank lines so
	// fence-internal blank lines are kept inside a single block.
	var blocks []Block
	var current []string
	currentInFence := false

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

	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "```") {
			currentInFence = !currentInFence
			current = append(current, line)
			if !currentInFence {
				// Closing fence — emit this block now (don't wait for blank).
				flush()
			}
			continue
		}
		if !currentInFence && strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	return blocks
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
	case strings.HasPrefix(first, "**λ**"):
		return BlockUserMsg
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
