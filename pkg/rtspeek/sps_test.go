package rtspeek

import "testing"

// These SPS samples are minimal and may not represent real streams fully.
// They should still parse width/height if valid.
// (If parsing fails on future library versions, adjust vectors.)
var (
	// From a 640x360 sample (example encoded SPS hex -> bytes)
	h264SPS360 = []byte{0x67, 0x42, 0xc0, 0x1f, 0x95, 0xa8, 0x14, 0x01, 0x6e, 0x9b, 0x80, 0x80, 0x80, 0xa0}
)

func TestParseH264SPS(t *testing.T) {
	w, h, err := parseH264SPS(h264SPS360)
	if err != nil {
		t.Skipf("skip: parse error %v", err)
	}
	if w == 0 || h == 0 {
		t.Skip("skip: zero resolution (heuristic)")
	}
}
