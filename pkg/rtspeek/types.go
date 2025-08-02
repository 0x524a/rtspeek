package rtspeek

import (
	"github.com/bluenviron/gortsplib/v4/pkg/description"
)

// StreamInfo exposes read-only accessors over details of a DESCRIBE operation.
// It purposefully hides the underlying implementation to allow future evolution.
type StreamInfo interface {
	// Basic metadata
	URLString() string
	IsReachable() bool
	ProtocolName() string
	DescribeSucceeded() bool
	LatencyMs() float64
	Failure() string
	Error() string
	Debug() []string

	// Media collections
	Video() []MediaInfo
	Audio() []MediaInfo
	Other() []MediaInfo
	MediaTotal() int

	// Helpers
	VideoResolutions() []Resolution
	MediaTypes() []string
	HasVideo() bool
	FirstVideoResolution() *Resolution

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
	FailureReason  string               `json:"failure_reason,omitempty"`
	ErrorMessage   string               `json:"error_message,omitempty"`
	DebugTrace     []string             `json:"debug_trace,omitempty"`
	RawDescription *description.Session `json:"-"`
}

// Accessor implementations
func (s *streamInfo) URLString() string         { return s.URL }
func (s *streamInfo) IsReachable() bool         { return s.Reachable }
func (s *streamInfo) ProtocolName() string      { return s.Protocol }
func (s *streamInfo) DescribeSucceeded() bool   { return s.DescribeOK }
func (s *streamInfo) LatencyMs() float64        { return s.Latency }
func (s *streamInfo) Failure() string           { return s.FailureReason }
func (s *streamInfo) Error() string             { return s.ErrorMessage }
func (s *streamInfo) Debug() []string           { return s.DebugTrace }
func (s *streamInfo) Video() []MediaInfo        { return s.VideoMedias }
func (s *streamInfo) Audio() []MediaInfo        { return s.AudioMedias }
func (s *streamInfo) Other() []MediaInfo        { return s.OtherMedias }
func (s *streamInfo) MediaTotal() int           { return s.MediaCount }
func (s *streamInfo) Raw() *description.Session { return s.RawDescription }

func (s *streamInfo) VideoResolutions() []Resolution {
	res := make([]Resolution, 0, len(s.VideoMedias))
	for _, v := range s.VideoMedias {
		if v.Resolution != nil {
			res = append(res, *v.Resolution)
		}
	}
	return res
}

func (s *streamInfo) MediaTypes() []string {
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

func (s *streamInfo) FirstVideoResolution() *Resolution {
	for _, v := range s.VideoMedias {
		if v.Resolution != nil {
			return v.Resolution
		}
	}
	return nil
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

// Convenience helpers for users that prefer free functions instead of methods.
func GetVideoResolutions(si StreamInfo) []Resolution { return si.VideoResolutions() }
func GetMediaTypes(si StreamInfo) []string           { return si.MediaTypes() }
func HasVideo(si StreamInfo) bool                    { return si.HasVideo() }
func FirstVideoResolution(si StreamInfo) *Resolution { return si.FirstVideoResolution() }
