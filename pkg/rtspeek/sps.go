package rtspeek

import (
	"errors"

	h264conf "github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	h265conf "github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
)

// parseH264SPS extracts width/height from a raw H264 SPS NAL unit
func parseH264SPS(sps []byte) (w int, h int, err error) {
	var conf h264conf.SPS
	if err = conf.Unmarshal(sps); err != nil {
		return 0, 0, err
	}
	return conf.Width(), conf.Height(), nil
}

// parseH265SPS extracts width/height from a raw H265 SPS NAL unit
func parseH265SPS(sps []byte) (w int, h int, err error) {
	var conf h265conf.SPS
	if err = conf.Unmarshal(sps); err != nil {
		return 0, 0, err
	}
	// H265 width/height directly available
	return conf.Width(), conf.Height(), nil
}

// bestResolution returns first successful parsed resolution among provided SPS units.
func bestResolution(parser func([]byte) (int, int, error), list [][]byte) *Resolution {
	for _, b := range list {
		if w, h, err := parser(b); err == nil && w > 0 && h > 0 {
			return &Resolution{Width: w, Height: h}
		}
	}
	return nil
}

// extractResolutionFromCodecData attempts to parse standard codec-specific SPS arrays.
func extractResolutionFromCodecData(codecSpecific map[string]any) (*Resolution, error) {
	return nil, errors.New("not implemented generic extraction")
}
