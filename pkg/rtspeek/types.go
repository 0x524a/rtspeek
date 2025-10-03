package rtspeek

import (
	"fmt"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
)

// StreamInfo exposes read-only accessors over details of a DESCRIBE operation.
// It purposefully hides the underlying implementation to allow future evolution.
type StreamInfo interface {
	// Basic metadata
	GetURLString() string
	IsReachable() bool
	GetProtocolName() string
	IsDescribeSucceeded() bool
	LatencyMs() float64
	GetDebugData() []string

	// Media collections
	GetVideoMedias() []MediaInfo
	GetAudioMedias() []MediaInfo
	GetOtherMedias() []MediaInfo
	GetMedias() []MediaInfo
	GetMediaCount() int

	// Helpers
	GetVideoResolutions() []Resolution
	GetVideoResolutionStrings() []string
	GetVideoResolutionString() string
	GetMediaTypes() []string
	HasVideo() bool
	GetFirstVideoMedia() *MediaInfo

	// Underlying raw description (may be nil)
	Raw() *description.Session
}

// streamInfo is the concrete implementation (unexported)
type streamInfo struct {
	URL            string               `json:"url"`
	Reachable      bool                 `json:"reachable"`
	Protocol       string               `json:"protocol"`
	DescribeOK     bool                 `json:"describe_ok"`
	Latency        float64              `json:"latency"`
	MediaCount     int                  `json:"media_count"`
	VideoMedias    []MediaInfo          `json:"video_medias,omitempty"`
	AudioMedias    []MediaInfo          `json:"audio_medias,omitempty"`
	OtherMedias    []MediaInfo          `json:"other_medias,omitempty"`
	DebugTrace     []string             `json:"debug_trace,omitempty"`
	RawDescription *description.Session `json:"-"`
}

// Accessor implementations
func (s *streamInfo) GetURLString() string        { return s.URL }
func (s *streamInfo) IsReachable() bool           { return s.Reachable }
func (s *streamInfo) GetProtocolName() string     { return s.Protocol }
func (s *streamInfo) IsDescribeSucceeded() bool   { return s.DescribeOK }
func (s *streamInfo) LatencyMs() float64          { return s.Latency }
func (s *streamInfo) GetDebugData() []string      { return s.DebugTrace }
func (s *streamInfo) GetVideoMedias() []MediaInfo { return s.VideoMedias }
func (s *streamInfo) GetAudioMedias() []MediaInfo { return s.AudioMedias }
func (s *streamInfo) GetOtherMedias() []MediaInfo { return s.OtherMedias }
func (s *streamInfo) GetMediaCount() int          { return s.MediaCount }
func (s *streamInfo) Raw() *description.Session   { return s.RawDescription }

func (s *streamInfo) GetMedias() []MediaInfo {
	allMedias := make([]MediaInfo, 0, s.MediaCount)
	allMedias = append(allMedias, s.VideoMedias...)
	allMedias = append(allMedias, s.AudioMedias...)
	allMedias = append(allMedias, s.OtherMedias...)
	return allMedias
}

func (s *streamInfo) GetVideoResolutions() []Resolution {
	res := make([]Resolution, 0, len(s.VideoMedias))
	for _, v := range s.VideoMedias {
		if v.Resolution != nil {
			res = append(res, *v.Resolution)
		}
	}
	return res
}

func (s *streamInfo) GetVideoResolutionStrings() []string {
	res := make([]string, 0, len(s.VideoMedias))
	for _, v := range s.VideoMedias {
		if v.Resolution != nil {
			res = append(res, v.Resolution.String())
		}
	}
	return res
}

func (s *streamInfo) GetMediaTypes() []string {
	types := make([]string, 0, s.MediaCount)
	for range s.VideoMedias {
		types = append(types, "video")
	}
	for range s.AudioMedias {
		types = append(types, "audio")
	}
	for range s.OtherMedias {
		types = append(types, "other")
	}
	return types
}

func (s *streamInfo) HasVideo() bool { return len(s.VideoMedias) > 0 }

func (s *streamInfo) GetFirstVideoMedia() *MediaInfo {
	if len(s.VideoMedias) > 0 {
		return &s.VideoMedias[0]
	}
	return nil
}

func (s *streamInfo) GetVideoResolutionString() string {
	if media := s.GetFirstVideoMedia(); media != nil && media.Resolution != nil {
		return media.Resolution.String()
	}
	return ""
}

// MediaInfo holds simplified per-media (track) information.
type MediaInfo struct {
	Index         int            `json:"index"`
	Type          string         `json:"type"`
	ClockRate     *int           `json:"clock_rate,omitempty"`
	Format        string         `json:"format,omitempty"`
	PayloadType   *uint8         `json:"payload_type,omitempty"`
	Resolution    *Resolution    `json:"resolution,omitempty"`
	CodecSpecific map[string]any `json:"codec_specific,omitempty"`
}

// Resolution expresses width x height.
type Resolution struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// String returns the resolution in "WIDTHxHEIGHT" format.
func (r Resolution) String() string {
	return fmt.Sprintf("%dx%d", r.Width, r.Height)
}

// Convenience helpers for users that prefer free functions instead of methods.
func GetVideoResolutions(si StreamInfo) []Resolution   { return si.GetVideoResolutions() }
func GetVideoResolutionStrings(si StreamInfo) []string { return si.GetVideoResolutionStrings() }
func GetVideoResolutionString(si StreamInfo) string    { return si.GetVideoResolutionString() }
func GetMediaTypes(si StreamInfo) []string             { return si.GetMediaTypes() }
func GetMedias(si StreamInfo) []MediaInfo              { return si.GetMedias() }
func HasVideo(si StreamInfo) bool                      { return si.HasVideo() }
func GetFirstVideoMedia(si StreamInfo) *MediaInfo      { return si.GetFirstVideoMedia() }

// Helper to get first video resolution (for backward compatibility)
func FirstVideoResolution(si StreamInfo) *Resolution {
	if media := si.GetFirstVideoMedia(); media != nil {
		return media.Resolution
	}
	return nil
}

// Helper to get first video resolution as string (for convenience)
func VideoResolutionString(si StreamInfo) string {
	return si.GetVideoResolutionString()
}
