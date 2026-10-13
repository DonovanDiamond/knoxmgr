package knox

import "testing"

func TestIsEnrolled(t *testing.T) {
	for status, want := range map[string]bool{
		"A": true, "B": true, "BS": true, "BA": true, "BL": true,
		"I": false, "P": false, "": false, "X": false, "a": false,
	} {
		if got := IsEnrolled(status); got != want {
			t.Errorf("IsEnrolled(%q) = %v, want %v", status, got, want)
		}
	}
}
