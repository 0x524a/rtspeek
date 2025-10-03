package rtspeek

import "testing"

func TestResolutionString(t *testing.T) {
	testCases := []struct {
		name     string
		res      Resolution
		expected string
	}{
		{"HD 720p", Resolution{Width: 1280, Height: 720}, "1280x720"},
		{"Full HD", Resolution{Width: 1920, Height: 1080}, "1920x1080"},
		{"4K", Resolution{Width: 3840, Height: 2160}, "3840x2160"},
		{"Square", Resolution{Width: 100, Height: 100}, "100x100"},
		{"Zero", Resolution{Width: 0, Height: 0}, "0x0"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.res.String()
			if got != tc.expected {
				t.Fatalf("expected %s, got: %s", tc.expected, got)
			}
		})
	}
}

func TestHasVideoAndFirstVideoMedia(t *testing.T) {
	s := &streamInfo{
		VideoMedias: []MediaInfo{{Index: 0, Type: "video", Resolution: &Resolution{Width: 1280, Height: 720}}},
		MediaCount:  1,
	}
	var si StreamInfo = s
	if !si.HasVideo() {
		t.Fatalf("expected HasVideo true")
	}

	// Test GetFirstVideoMedia()
	fm := si.GetFirstVideoMedia()
	if fm == nil {
		t.Fatalf("expected first video media to not be nil")
	}
	if fm.Type != "video" || fm.Index != 0 {
		t.Fatalf("unexpected first media: %#v", fm)
	}
	if fm.Resolution == nil || fm.Resolution.Width != 1280 || fm.Resolution.Height != 720 {
		t.Fatalf("unexpected first media resolution: %#v", fm.Resolution)
	}

	// Test backward compatibility with FirstVideoResolution()
	fr := FirstVideoResolution(si)
	if fr == nil || fr.Width != 1280 || fr.Height != 720 {
		t.Fatalf("unexpected first resolution: %#v", fr)
	}

	vres := si.GetVideoResolutions()
	if len(vres) != 1 || vres[0].Width != 1280 {
		t.Fatalf("unexpected video resolutions: %#v", vres)
	}

	// Test string resolution methods
	resStr := si.GetVideoResolutionString()
	if resStr != "1280x720" {
		t.Fatalf("expected video resolution string '1280x720', got: %s", resStr)
	}

	resStrings := si.GetVideoResolutionStrings()
	if len(resStrings) != 1 || resStrings[0] != "1280x720" {
		t.Fatalf("expected video resolution strings ['1280x720'], got: %#v", resStrings)
	}

	// Test convenience functions
	if VideoResolutionString(si) != "1280x720" {
		t.Fatalf("expected VideoResolutionString '1280x720', got: %s", VideoResolutionString(si))
	}

	gotStrings := GetVideoResolutionStrings(si)
	if len(gotStrings) != 1 || gotStrings[0] != "1280x720" {
		t.Fatalf("expected GetVideoResolutionStrings ['1280x720'], got: %#v", gotStrings)
	}

	// Test GetMedias()
	allMedias := si.GetMedias()
	if len(allMedias) != 1 {
		t.Fatalf("expected 1 media item, got: %d", len(allMedias))
	}
	if allMedias[0].Type != "video" || allMedias[0].Index != 0 {
		t.Fatalf("unexpected media item: %#v", allMedias[0])
	}

	// Test convenience function
	allMediasFunc := GetMedias(si)
	if len(allMediasFunc) != 1 || allMediasFunc[0].Type != "video" {
		t.Fatalf("expected GetMedias to return 1 video media, got: %#v", allMediasFunc)
	}
}

func TestNoVideoMedia(t *testing.T) {
	s := &streamInfo{MediaCount: 0}
	var si StreamInfo = s
	if si.HasVideo() {
		t.Fatalf("expected HasVideo false")
	}
	if si.GetFirstVideoMedia() != nil {
		t.Fatalf("expected nil media")
	}
	if FirstVideoResolution(si) != nil {
		t.Fatalf("expected nil resolution")
	}

	// Test string methods with no video
	if si.GetVideoResolutionString() != "" {
		t.Fatalf("expected empty string, got: %s", si.GetVideoResolutionString())
	}

	resStrings := si.GetVideoResolutionStrings()
	if len(resStrings) != 0 {
		t.Fatalf("expected empty slice, got: %#v", resStrings)
	}

	// Test convenience functions
	if VideoResolutionString(si) != "" {
		t.Fatalf("expected empty string, got: %s", VideoResolutionString(si))
	}

	gotStrings := GetVideoResolutionStrings(si)
	if len(gotStrings) != 0 {
		t.Fatalf("expected empty slice, got: %#v", gotStrings)
	}

	// Test GetMedias() with no media
	allMedias := si.GetMedias()
	if len(allMedias) != 0 {
		t.Fatalf("expected empty slice, got: %#v", allMedias)
	}
}

func TestGetMediasMultipleTypes(t *testing.T) {
	s := &streamInfo{
		VideoMedias: []MediaInfo{
			{Index: 0, Type: "video", Format: "H264", Resolution: &Resolution{Width: 1920, Height: 1080}},
			{Index: 1, Type: "video", Format: "H265", Resolution: &Resolution{Width: 1280, Height: 720}},
		},
		AudioMedias: []MediaInfo{
			{Index: 2, Type: "audio", Format: "AAC"},
		},
		OtherMedias: []MediaInfo{
			{Index: 3, Type: "application", Format: "text"},
		},
		MediaCount: 4,
	}
	var si StreamInfo = s

	// Test GetMedias() returns all media types in order: video, audio, other
	allMedias := si.GetMedias()
	expected := []struct {
		index  int
		mType  string
		format string
	}{
		{0, "video", "H264"},
		{1, "video", "H265"},
		{2, "audio", "AAC"},
		{3, "application", "text"},
	}

	if len(allMedias) != 4 {
		t.Fatalf("expected 4 media items, got: %d", len(allMedias))
	}

	for i, exp := range expected {
		if allMedias[i].Index != exp.index {
			t.Fatalf("expected media[%d].Index=%d, got: %d", i, exp.index, allMedias[i].Index)
		}
		if allMedias[i].Type != exp.mType {
			t.Fatalf("expected media[%d].Type=%s, got: %s", i, exp.mType, allMedias[i].Type)
		}
		if allMedias[i].Format != exp.format {
			t.Fatalf("expected media[%d].Format=%s, got: %s", i, exp.format, allMedias[i].Format)
		}
	}

	// Test convenience function
	allMediasFunc := GetMedias(si)
	if len(allMediasFunc) != 4 {
		t.Fatalf("expected GetMedias() to return 4 items, got: %d", len(allMediasFunc))
	}
}
