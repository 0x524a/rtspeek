package rtspeek

import "testing"

func TestHasVideoAndFirstVideoResolution(t *testing.T) {
	s := &streamInfo{
		VideoMedias: []MediaInfo{{Index: 0, Type: "video", Resolution: &Resolution{Width: 1280, Height: 720}}},
		MediaCount:  1,
	}
	var si StreamInfo = s
	if !si.HasVideo() {
		t.Fatalf("expected HasVideo true")
	}
	fr := si.FirstVideoResolution()
	if fr == nil || fr.Width != 1280 || fr.Height != 720 {
		t.Fatalf("unexpected first resolution: %#v", fr)
	}
	vres := si.VideoResolutions()
	if len(vres) != 1 || vres[0].Width != 1280 {
		t.Fatalf("unexpected video resolutions: %#v", vres)
	}
}

func TestNoVideoResolution(t *testing.T) {
	s := &streamInfo{MediaCount: 0}
	var si StreamInfo = s
	if si.HasVideo() {
		t.Fatalf("expected HasVideo false")
	}
	if si.FirstVideoResolution() != nil {
		t.Fatalf("expected nil resolution")
	}
}
