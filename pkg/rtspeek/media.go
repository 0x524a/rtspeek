package rtspeek

import (
	"fmt"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

// classifyMedia extracts simplified media info.
func classifyMedia(idx int, m *description.Media) MediaInfo {
	mi := MediaInfo{Index: idx, Type: string(m.Type)}
	if len(m.Formats) > 0 {
		f := m.Formats[0]
		pt := f.PayloadType()
		mi.PayloadType = &pt
		if cr := f.ClockRate(); cr != 0 {
			mi.ClockRate = &cr
		}
		mi.Format = fmt.Sprintf("%T", f)

		// Try resolution for known codecs
		switch ct := f.(type) {
		case *format.H264:
			if ct.SPS != nil {
				if r := bestResolution(parseH264SPS, [][]byte{ct.SPS}); r != nil {
					mi.Resolution = r
				}
			}
		case *format.H265:
			if ct.SPS != nil {
				if r := bestResolution(parseH265SPS, [][]byte{ct.SPS}); r != nil {
					mi.Resolution = r
				}
			}
		}
	}
	return mi
}
