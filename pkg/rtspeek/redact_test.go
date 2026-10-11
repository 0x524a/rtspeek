package rtspeek

import "testing"

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"rtsp://user:pass@127.0.0.1:8602/x": "rtsp://user:***@127.0.0.1:8602/x",
		"rtsps://u:p%40ss@host/a?b=1":       "rtsps://u:***@host/a?b=1",
		"rtsp://user@host/x":                "rtsp://user@host/x",
		"rtsp://host/x":                     "rtsp://host/x",
		"not a url":                         "not a url",
	}
	for in, want := range cases {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactText(t *testing.T) {
	in := `dial rtsp://admin:s3cret@10.0.0.5:554/s failed; retry RTSP://a:b@h/x`
	want := `dial rtsp://admin:***@10.0.0.5:554/s failed; retry RTSP://a:***@h/x`
	if got := RedactText(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDescribeStreamDoesNotEchoPassword(t *testing.T) {
	info, _ := DescribeStream(t.Context(), "rtsp://user:topsecret@127.0.0.1:1/x", 0)
	if info == nil {
		t.Skip("failed before any RTSP exchange")
	}
	if got := info.GetURLString(); got != "rtsp://user:***@127.0.0.1:1/x" {
		t.Errorf("url = %q", got)
	}
}
