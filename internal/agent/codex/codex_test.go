package codex

import (
	"testing"

	"charm.land/catwalk/pkg/catwalk"
)

func TestEmbedded_ParsesProvider(t *testing.T) {
	p := Embedded()
	if p.ID != catwalk.InferenceProvider("codex") {
		t.Errorf("expected ID %q, got %q", "codex", p.ID)
	}
	if len(p.Models) < 3 {
		t.Errorf("expected at least 3 models, got %d", len(p.Models))
	}
	// Verify default large model is in the model list
	largeFound := false
	for _, m := range p.Models {
		if m.ID == p.DefaultLargeModelID {
			largeFound = true
			break
		}
	}
	if !largeFound {
		t.Errorf("default_large_model_id %q not found in models list", p.DefaultLargeModelID)
	}
	// Verify default small model is in the model list
	smallFound := false
	for _, m := range p.Models {
		if m.ID == p.DefaultSmallModelID {
			smallFound = true
			break
		}
	}
	if !smallFound {
		t.Errorf("default_small_model_id %q not found in models list", p.DefaultSmallModelID)
	}
}
