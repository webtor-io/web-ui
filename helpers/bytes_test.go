package helpers

import "testing"

// The space before the unit is U+00A0: a phone must not split "1.0" from "MB".
func TestBytes_NoBreakSpaceBeforeUnit(t *testing.T) {
	cases := map[uint64]string{
		5:                 "5\u00a0B",
		1024:              "1.0\u00a0kB",
		1536 * 1024:       "1.5\u00a0MB",
		150 * 1024 * 1024: "150\u00a0MB",
	}
	for in, want := range cases {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", in, got, want)
		}
	}
}
