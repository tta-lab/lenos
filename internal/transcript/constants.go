package transcript

// Wire-format constants for the .md transcript spec (57a09f51).
//
// Single source of truth for tokens used by both the writer (format.go,
// recorder.go) and the parser (blocks.go). New tokens added here as they
// migrate out of inline string literals.

// Lambda is the user-message anchor — opens every turn in the transcript.
// Writer renders `**λ** <text>`; parser detects the same prefix.
const Lambda = "λ"
