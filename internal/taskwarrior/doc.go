// Package taskwarrior provides helpers for querying taskwarrior from lenos.
//
// IMPORTANT: `task ... export` always returns a JSON array — even for a
// single task. Always unmarshal into a slice, never an object. Force this
// behavior in exec calls with "rc.json.array=on" so user taskrc settings
// cannot change the output shape.
package taskwarrior
