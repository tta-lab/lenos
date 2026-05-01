package tui

// inputPane is a stub composition slot for Step 11 (input pane wire-up).
// It currently renders nothing and ignores all messages so Step 10's
// composition root compiles green; Step 11 fills in the editor + dialog +
// completions integration.
type inputPane struct{}

func newInputPane() *inputPane { return &inputPane{} }

// Render returns the empty string until Step 11 wires the editor.
func (p *inputPane) Render() string { return "" }
