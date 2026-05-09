package chat

// StickyAnchor is retained as a stub interface for backward compatibility.
// The original implementation in md_block.go has been removed as part of
// the .md transcript deletion. This stub keeps existing type assertions
// compiling until they are also removed in a follow-up.
type StickyAnchor interface {
	IsStickyAnchor() bool
	StickyLine() string
}
