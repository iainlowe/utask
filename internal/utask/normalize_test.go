package utask

import "testing"

func TestNormalizeInput_DeterministicAndCanonical(t *testing.T) {
	in1 := TaskInput{
		Text: "  Buy   milk\n",
		Tags: []string{"Errand", "shopping", "errand", "  ", "SHOPPING"},
	}
	c1, id1 := NormalizeInput(in1)

	if c1.Text != "Buy milk" {
		t.Fatalf("text canonicalization failed: %q", c1.Text)
	}
	if len(c1.Tags) != 2 || c1.Tags[0] != "errand" || c1.Tags[1] != "shopping" {
		t.Fatalf("tags canonicalization failed: %#v", c1.Tags)
	}
	// No extended/description field in canonical anymore

	// Same semantic input in different order produces same ID
	in2 := TaskInput{Text: "Buy milk", Tags: []string{"shopping", "errand"}}
	_, id2 := NormalizeInput(in2)
	if id1 != id2 {
		t.Fatalf("expected deterministic id, got %q vs %q", id1, id2)
	}
}
