package transcript

// Wire-format constants for the .md transcript spec (57a09f51).
//
// Single source of truth for tokens used by both the writer (format.go,
// recorder.go) and the parser (blocks.go). New tokens added here as they
// migrate out of inline string literals.

// Lambda is the user-message anchor glyph. Used bare by the chat styler
// (which colours just the glyph without the markdown wrapping).
const Lambda = "λ"

// LambdaMsgPrefix is the bold-wrapped form of Lambda that opens every
// user-message block in the .md transcript. Writer (format.go) emits it,
// parser (blocks.go) detects it. Compile-time-folded from Lambda so the
// wire-format spec lives in exactly one place.
const LambdaMsgPrefix = "**" + Lambda + "**"
