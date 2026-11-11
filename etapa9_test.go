package main

import "testing"

func TestParseRAPL(t *testing.T) {
	uj, max, err := parseRAPL("energia 123456789 262143328850\n")
	if err != nil || uj != 123456789 || max != 262143328850 {
		t.Fatalf("%d %d %v", uj, max, err)
	}
	for _, bad := range []string{"", "pedido inválido", "energia x 1"} {
		if _, _, err := parseRAPL(bad); err == nil {
			t.Errorf("%q deveria dar erro", bad)
		}
	}
}
