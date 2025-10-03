package rtspeek

import (
	"fmt"

	"github.com/bluenviron/gortsplib/v4/pkg/base"
)

// DebugTracer captures RTSP request/response traces for debugging.
type DebugTracer struct {
	trace []string
}

// NewDebugTracer creates a new debug tracer.
func NewDebugTracer() *DebugTracer {
	return &DebugTracer{
		trace: make([]string, 0),
	}
}

// OnRequest captures outgoing RTSP requests.
func (dt *DebugTracer) OnRequest(req *base.Request) {
	dt.trace = append(dt.trace, fmt.Sprintf("--> %s %s", req.Method, req.URL))
	for k, v := range req.Header {
		dt.trace = append(dt.trace, fmt.Sprintf("--> H %s: %s", k, v))
	}
}

// OnResponse captures incoming RTSP responses.
func (dt *DebugTracer) OnResponse(res *base.Response) {
	dt.trace = append(dt.trace, fmt.Sprintf("<-- %d %s", res.StatusCode, res.StatusMessage))
	for k, v := range res.Header {
		dt.trace = append(dt.trace, fmt.Sprintf("<-- H %s: %s", k, v))
	}
}

// AddStage adds a stage marker to the trace.
func (dt *DebugTracer) AddStage(stage string) {
	dt.trace = append(dt.trace, fmt.Sprintf("STAGE: %s", stage))
}

// GetTrace returns a copy of the current trace.
func (dt *DebugTracer) GetTrace() []string {
	result := make([]string, len(dt.trace))
	copy(result, dt.trace)
	return result
}

// Clear resets the trace.
func (dt *DebugTracer) Clear() {
	dt.trace = dt.trace[:0]
}
