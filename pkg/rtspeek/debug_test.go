package rtspeek

import (
	"testing"

	"github.com/bluenviron/gortsplib/v4/pkg/base"
)

func TestDebugTracer(t *testing.T) {
	tracer := NewDebugTracer()

	// Test initial state
	if len(tracer.GetTrace()) != 0 {
		t.Error("New tracer should have empty trace")
	}

	// Test adding stages
	tracer.AddStage("test_stage")
	trace := tracer.GetTrace()
	if len(trace) != 1 || trace[0] != "STAGE: test_stage" {
		t.Errorf("Expected trace to contain 'STAGE: test_stage', got %v", trace)
	}

	// Test OnRequest
	req := &base.Request{
		Method: "OPTIONS",
		URL:    &base.URL{Scheme: "rtsp", Host: "example.com", Path: "/stream"},
		Header: base.Header{
			"User-Agent": base.HeaderValue{"test-agent"},
		},
	}
	tracer.OnRequest(req)

	trace = tracer.GetTrace()
	if len(trace) < 2 {
		t.Fatalf("Expected at least 2 trace entries, got %d", len(trace))
	}

	found := false
	for _, entry := range trace {
		if entry == "--> OPTIONS rtsp://example.com/stream" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Request trace not found in debug output")
	}

	// Test OnResponse
	res := &base.Response{
		StatusCode:    200,
		StatusMessage: "OK",
		Header: base.Header{
			"Content-Type": base.HeaderValue{"application/sdp"},
		},
	}
	tracer.OnResponse(res)

	trace = tracer.GetTrace()
	found = false
	for _, entry := range trace {
		if entry == "<-- 200 OK" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Response trace not found in debug output")
	}

	// Test Clear
	tracer.Clear()
	if len(tracer.GetTrace()) != 0 {
		t.Error("Trace should be empty after Clear()")
	}
}

func TestDebugTracer_GetTrace_Copy(t *testing.T) {
	tracer := NewDebugTracer()
	tracer.AddStage("test")

	trace1 := tracer.GetTrace()
	trace2 := tracer.GetTrace()

	// Modify one copy
	trace1[0] = "modified"

	// Original tracer should be unaffected
	if tracer.GetTrace()[0] != "STAGE: test" {
		t.Error("GetTrace should return a copy, not a reference to the internal slice")
	}

	// Second copy should also be unaffected
	if trace2[0] != "STAGE: test" {
		t.Error("GetTrace should return independent copies")
	}
}
