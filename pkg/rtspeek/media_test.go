package rtspeek

import (
	"strings"
	"testing"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
)

func TestClassifyMediaUnsupportedVideoFormat(t *testing.T) {
	// Test with a generic video format that's not H264 or H265
	genericMedia := &description.Media{
		Type: description.MediaTypeVideo,
		Formats: []format.Format{
			&format.Generic{
				PayloadTyp: 96,
				RTPMa:      "MJPEG/90000",
			},
		},
	}

	_, err := classifyMedia(0, genericMedia)
	if err == nil {
		t.Fatal("expected error for unsupported video format, got nil")
	}

	expectedMsg := "unsupported video format"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Fatalf("expected error message to contain '%s', got: %s", expectedMsg, err.Error())
	}
}

func TestClassifyMediaSupportedVideoFormats(t *testing.T) {
	testCases := []struct {
		name   string
		format format.Format
	}{
		{
			name: "H264",
			format: &format.H264{
				PayloadTyp: 96,
				SPS:        []byte{0x67, 0x42, 0x00, 0x1f},
				PPS:        []byte{0x68, 0xce, 0x06, 0xe2},
			},
		},
		{
			name: "H265",
			format: &format.H265{
				PayloadTyp: 96,
				SPS:        []byte{0x40, 0x01, 0x0c, 0x01, 0xff, 0xff, 0x01, 0x60, 0x00, 0x00, 0x03, 0x00, 0x90, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x5d, 0xa0, 0x02, 0x80, 0x80, 0x2d, 0x16, 0x59, 0x59, 0xa4, 0x93, 0x2b, 0x80, 0x40, 0x00, 0x00, 0xfa, 0x40, 0x00, 0x17, 0x70, 0x02},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			media := &description.Media{
				Type:    description.MediaTypeVideo,
				Formats: []format.Format{tc.format},
			}

			mediaInfo, err := classifyMedia(0, media)
			if err != nil {
				t.Fatalf("unexpected error for %s format: %v", tc.name, err)
			}

			if mediaInfo.Type != "video" {
				t.Fatalf("expected type 'video', got: %s", mediaInfo.Type)
			}

			if mediaInfo.Format != tc.name {
				t.Fatalf("expected format '%s', got: %s", tc.name, mediaInfo.Format)
			}
		})
	}
}

func TestClassifyMediaAudioFormatsStillWork(t *testing.T) {
	// Audio formats should not be affected by the video format restriction
	audioMedia := &description.Media{
		Type: description.MediaTypeAudio,
		Formats: []format.Format{
			&format.Generic{
				PayloadTyp: 97,
				RTPMa:      "PCMU/8000",
			},
		},
	}

	mediaInfo, err := classifyMedia(0, audioMedia)
	if err != nil {
		t.Fatalf("unexpected error for audio format: %v", err)
	}

	if mediaInfo.Type != "audio" {
		t.Fatalf("expected type 'audio', got: %s", mediaInfo.Type)
	}
}
