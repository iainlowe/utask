package utask

import "testing"

func TestMatchPrefix(t *testing.T) {
	keys := []string{"abc12345deadbeef", "abc9ffff0000", "beadfeed"}

	if _, _, err := matchPrefix(keys, "dead"); err == nil {
		t.Fatalf("expected not found for unmatched prefix")
	}

	if full, cands, err := matchPrefix(keys, "bead"); err != nil || len(cands) != 0 || full != "beadfeed" {
		t.Fatalf("expected unique match, got full=%q cands=%v err=%v", full, cands, err)
	}

	if _, cands, err := matchPrefix(keys, "abc"); err == nil || len(cands) < 2 {
		t.Fatalf("expected ambiguous with candidates, got err=%v cands=%v", err, cands)
	}
}
