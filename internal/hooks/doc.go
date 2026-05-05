// Package hooks defines lifecycle hooks fired by the agent loop.
//
// The primary hook is PostStep, a shell command that runs after each model
// step with a JSON envelope on stdin. See ShellRunner for details.
//
// Plane: worker
package hooks
