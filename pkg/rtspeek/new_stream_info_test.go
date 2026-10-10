package rtspeek

import "testing"

func TestNewStreamInfo(t *testing.T) {
	info := NewStreamInfo(StreamInfoParams{
		URL:        "rtsp://cam/stream",
		Reachable:  true,
		Protocol:   "rtsp",
		DescribeOK: true,
		LatencyMs:  12.5,
		VideoMedias: []MediaInfo{
			{Index: 0, Type: "video", Format: "H264", Resolution: &Resolution{Width: 1280, Height: 720}},
		},
		AudioMedias: []MediaInfo{{Index: 1, Type: "audio", Format: "MPEG4Audio"}},
		DebugTrace:  []string{"STAGE: start"},
	})

	if info.GetURLString() != "rtsp://cam/stream" || !info.IsReachable() || !info.IsDescribeSucceeded() {
		t.Fatalf("basic fields not carried over: %+v", info)
	}
	if info.GetProtocolName() != "rtsp" || info.LatencyMs() != 12.5 {
		t.Fatalf("protocol/latency not carried over")
	}
	if got := info.GetMediaCount(); got != 2 {
		t.Fatalf("MediaCount = %d, want 2 (derived from media slices)", got)
	}
	if len(info.GetMedias()) != info.GetMediaCount() {
		t.Fatalf("GetMedias length %d disagrees with MediaCount %d", len(info.GetMedias()), info.GetMediaCount())
	}
	if !info.HasVideo() || info.GetVideoResolutionString() != "1280x720" {
		t.Fatalf("video helpers wrong: has=%v res=%q", info.HasVideo(), info.GetVideoResolutionString())
	}
	if len(info.GetDebugData()) != 1 || info.Raw() != nil {
		t.Fatalf("debug/raw wrong")
	}
}

func TestNewStreamInfoEmpty(t *testing.T) {
	info := NewStreamInfo(StreamInfoParams{URL: "rtsp://x"})
	if info.GetMediaCount() != 0 || info.HasVideo() || info.GetFirstVideoMedia() != nil {
		t.Fatalf("empty info should have no media")
	}
}
