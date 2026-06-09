package trustpins

import "testing"

func TestBootstrapEmptyByDefault(t *testing.T) {
	old := bootstrapPins
	defer func() { bootstrapPins = old }()
	bootstrapPins = ""
	if pins := Bootstrap(); pins != nil {
		t.Errorf("empty bootstrapPins must yield nil (fail-closed), got %v", pins)
	}
	bootstrapPins = "   "
	if pins := Bootstrap(); pins != nil {
		t.Errorf("blank bootstrapPins must yield nil, got %v", pins)
	}
}

func TestBootstrapParses(t *testing.T) {
	old := bootstrapPins
	defer func() { bootstrapPins = old }()
	bootstrapPins = "sha256:aaa, sha256:bbb\nsha256:ccc"
	got := Bootstrap()
	if len(got) != 3 || got[0] != "sha256:aaa" || got[2] != "sha256:ccc" {
		t.Errorf("parse = %v, want [sha256:aaa sha256:bbb sha256:ccc]", got)
	}
}
