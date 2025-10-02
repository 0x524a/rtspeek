<div align="center">

# RTSPeek

Small, fast RTSP inspection toolkit (library + CLI) built on top of [`gortsplib`](https://github.com/bluenviron/gortsplib).

Inspect a stream URL, perform RTSP handshake (OPTIONS + DESCRIBE), classify tracks, extract codec + (heuristic) resolution info, and emit structured JSON for automation.

</div>

---

## ✨ Key Features

| Area | Capabilities |
|------|--------------|
| Validation | Basic scheme check (`rtsp://`, `rtsps://`) w/ early rejection |
| Reachability | TCP preflight + timed DESCRIBE with overall timeout |
| Media Summary | Track type, payload type, clock rate, codec name, basic H264/H265 SPS-derived resolution |
| Diagnostics | Failure cause classification + raw error string + optional RTSP trace |
| Auth Retry | Automatic single retry on 401 (Digest) when credentials embedded in URL |
| Debugging | `--debug` flag yields ordered request/response header trace + stage markers |
| Library API | Clean interface (`StreamInfo`) with helper methods (HasVideo, FirstVideoResolution, VideoResolutions, MediaTypes) |
| CLI Output | Deterministic JSON (optionally pretty) for integration with scripts / services |

---

## 📦 Installation

Library only:
```bash
go get github.com/example/rtpeek
```

CLI (from repo):
```bash
git clone https://github.com/example/rtpeek.git
cd rtpeek
go build ./cmd/rtpeek
./rtpeek --help
```

Add to PATH:
```bash
go install ./cmd/rtpeek
# binary now at $(go env GOPATH)/bin/rtpeek
```

---

## 🚀 Quick Start (CLI)

```bash
# Basic probe
rtpeek --url rtsp://camera.local/stream

# Increase timeout
rtpeek --url rtsp://camera.local/stream --timeout 8s

# Verbose diagnostic (stderr) + JSON
rtpeek --url rtsp://bad.host/stream --timeout 3s --verbose

# Include RTSP handshake trace
rtpeek --url rtsp://camera.local/stream --debug --timeout 5s --verbose

# Disable pretty JSON
rtpeek --url rtsp://camera.local/stream --pretty=false
```

Flags:
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--url` | string | (required) | RTSP / RTSPS URL to inspect (credentials may be embedded) |
| `--timeout` | duration | `5s` | Overall deadline (dial + OPTIONS + DESCRIBE + retry) |
| `--pretty` | bool | `true` | Indent JSON output |
| `--verbose` | bool | `false` | Emit failure summary to stderr when applicable |
| `--debug` | bool | `false` | Capture RTSP request/response headers + stage markers |

Exit codes: `0` success (describe may still fail; see `describe_ok`), `1` internal/usage error.

---

## 🧪 Programmatic Usage

```go
package main

import (
        "context"
        "fmt"
        "time"
    sd "github.com/example/rtpeek/pkg/rtpeek"
)

func main() {
        ctx := context.Background()
        info, err := sd.DescribeStream(ctx, "rtsp://user:pass@host:554/stream", 5*time.Second)
        if err != nil {
                fmt.Println("describe error:", err)
        }
        fmt.Println("Describe OK:", info.DescribeSucceeded())
        fmt.Println("Video Resolutions:", info.VideoResolutions())
}
```

### Interface Surface (`StreamInfo`)

Core accessors (selected):
```go
URLString() string
IsReachable() bool
DescribeSucceeded() bool
LatencyMs() float64
Failure() string        // classification
Error() string          // raw error string
Video() []MediaInfo
Audio() []MediaInfo
VideoResolutions() []Resolution
HasVideo() bool
FirstVideoResolution() *Resolution
Raw() *description.Session // underlying SDP model (not JSON encoded)
```

Helper free functions mirror methods: `GetVideoResolutions(si)`, `HasVideo(si)` etc.

---

## 📄 JSON Output Schema

Example (success):
```json
{
    "url": "rtsp://camera.local/stream",
    "reachable": true,
    "protocol": "rtsp",
    "describe_ok": true,
    "latency": 74.2,
    "media_count": 1,
    "video_medias": [
        {
            "index": 0,
            "type": "video",
            "payload_type": 96,
            "format": "H264",
            "resolution": { "width": 1920, "height": 1080 }
        }
    ]
}
```

Example (failure with debug):
```json
{
    "url": "rtsp://camera.local/stream",
    "reachable": true,
    "protocol": "rtsp",
    "describe_ok": false,
    "latency": 5001.3,
    "media_count": 0,
    "failure_reason": "auth_required",
    "error_message": "401 Unauthorized",
    "debug_trace": [
        "STAGE: start",
        "--> OPTIONS rtsp://camera.local/stream",
        "<-- 401 Unauthorized",
        "STAGE: describe",
        "STAGE: auth-retry",
        "--> DESCRIBE rtsp://camera.local/stream",
        "<-- 200 OK"
    ]
}
```

Key fields:
| Field | Description |
|-------|-------------|
| `reachable` | TCP connect succeeded pre-describe |
| `describe_ok` | DESCRIBE completed with 2xx and SDP parsed |
| `failure_reason` | Short classification (see below) |
| `error_message` | Raw underlying error string |
| `latency` | Milliseconds from start to final state (float) |
| `debug_trace` | Present only with `--debug` |

Failure reason values: `timeout`, `connection_refused`, `dns_error`, `auth_required`, `not_found`, `connection_closed`, `unsupported_scheme`, `other`.

---

## 🔐 Authentication
Embed credentials in the URL: `rtsp://user:pass@host:554/stream`.
If first DESCRIBE returns 401 with a digest challenge, a single retry is attempted.

Future improvements (roadmap): multi-round auth, Basic fallback, custom headers.

---

## 🛠 Debugging Toolkit
Use `--debug` to capture:
1. Stage markers: `STAGE: start`, `STAGE: options`, `STAGE: describe`, `STAGE: auth-retry`.
2. Every RTSP request line, then headers (prefixed `--> H`).
3. Every response line + headers (prefixed `<-- H`).

This lets you pinpoint stalls (e.g., missing DESCRIBE response).

---

## 🧬 Media & Resolution Extraction
H264 / H265 SPS parsing is used (via mediacommon) to derive width/height when available.
If SPS is absent or parse fails, `resolution` is omitted.

---

## 🧪 Testing & Coverage

Run unit tests:
```bash
go test ./...
```

Generate coverage:
```bash
go test -coverprofile=coverage.out ./pkg/rtpeek
go tool cover -func=coverage.out | head
```

Current indicative coverage (may differ as project evolves): ~50%+ of `pkg/rtpeek` with table-driven RTSP server tests (success, not_found, auth retry) and SPS parsing.

Integration test (tagged):
```bash
go test -tags=integration -run TestDescribeStreamIntegration ./pkg/rtpeek
```

---

## 🩹 Troubleshooting
| Symptom | Likely Cause | Suggested Action |
|---------|--------------|------------------|
| `failure_reason=timeout` | Slow or no DESCRIBE response | Increase `--timeout`, enable `--debug` |
| `failure_reason=auth_required` w/ creds | Wrong credentials or unsupported auth scheme | Verify user/pass; server may need Basic; multi-round not yet implemented |
| `failure_reason=connection_refused` | Port closed / firewall | Confirm RTSP port; try :554 explicitly |
| `failure_reason=dns_error` | Hostname resolution failure | Use IP or fix DNS / /etc/hosts |
| `failure_reason=not_found` | Wrong path | Check camera channel/path syntax |
| `resolution` missing | No SPS / parse fail | Ensure stream actually sending SPS NALs |

---

## ❓ FAQ
**Q: Does it perform SETUP/PLAY?**  
Not currently; it stops after DESCRIBE.

**Q: Why is latency a float in milliseconds?**  
To provide a human-friendly unit directly without post-processing (higher-level tools can format / round as needed).

**Q: How do I add custom headers?**  
Not exposed yet; will be part of a future extension (see roadmap).

---

## 🗺 Roadmap Ideas
| Feature | Status |
|---------|--------|
| Separate dial vs describe timeouts | Planned |
| Multi-round auth & Basic fallback | Planned |
| Custom headers / User-Agent | Planned |
| Optional SETUP/PLAY probe (RTCP stats) | Exploratory |
| Structured logging hooks | Exploratory |
| Export RawDescription JSON (opt-in) | Planned |

---

## 🔑 License
MIT (add LICENSE file if distributing)

---

## 🤝 Contributing
1. Fork & branch
2. Add tests for new behavior
3. Run `go vet` & `go test`
4. Open PR with clear description / motivation

---

## ❤️ Acknowledgments
Built atop the excellent [`gortsplib`](https://github.com/bluenviron/gortsplib) ecosystem.

---

Enjoy! Feel free to open issues for feature requests or edge cases you encounter.
