package rtspeek

import "testing"

func TestValidateURL(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"", false},
		{"http://example.com", false},
		{"rtsp://", false},
		{"rtsp://example.com/stream", true},
		{"rtsps://example.com", true},
	}
	for _, c := range cases {
		if got := ValidateURL(c.in); got != c.ok {
			t.Fatalf("ValidateURL(%q)=%v expected %v", c.in, got, c.ok)
		}
	}
}
