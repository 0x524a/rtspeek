package rtspeek

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

// classifyMedia extracts simplified media info.
func classifyMedia(idx int, m *description.Media) (MediaInfo, error) {
	mi := MediaInfo{Index: idx, Type: string(m.Type)}
	if len(m.Formats) > 0 {
		f := m.Formats[0]
		pt := f.PayloadType()
		mi.PayloadType = &pt
		if cr := f.ClockRate(); cr != 0 {
			mi.ClockRate = &cr
		}
		mi.Format = extractFormatName(f)

		// For video media, only support H264 and H265
		if m.Type == description.MediaTypeVideo {
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
			default:
				return mi, errors.New(fmt.Sprintf("unsupported video format: %s", mi.Format))
			}
		}
	}
	return mi, nil
}

// extractFormatName extracts a clean format name from the format interface.
func extractFormatName(f format.Format) string {
	typeName := fmt.Sprintf("%T", f)

	// Remove package prefix and pointer indicator
	if lastDot := strings.LastIndex(typeName, "."); lastDot != -1 {
		typeName = typeName[lastDot+1:]
	}
	typeName = strings.TrimPrefix(typeName, "*")

	return typeName
}
