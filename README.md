<div align="center">

# RTSPeek

Small, fast RTSP inspection toolkit (library + CLI) built on top of [`gortsplib`](https://github.com/bluenviron/gortsplib).

Inspect a stream URL, perform RTSP handshake (OPTIONS + DESCRIBE), classify tracks, extract codec + (heuristic) resolution info, and emit structured JSON for automation.

[![CI](https://github.com/0x524A/rtspeek/actions/workflows/sonarcloud.yml/badge.svg)](https://github.com/0x524A/rtspeek/actions/workflows/sonarcloud.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=0x524a_rtspeek&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=0x524a_rtspeek)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=0x524a_rtspeek&metric=coverage)](https://sonarcloud.io/summary/new_code?id=0x524a_rtspeek)
[![Go Reference](https://pkg.go.dev/badge/github.com/0x524A/rtspeek.svg)](https://pkg.go.dev/github.com/0x524A/rtspeek)
[![License](https://img.shields.io/github/license/0x524A/rtspeek)](LICENSE)

</div>

---

## Contents
- [Key Features](#user-content-key-features)
- [Installation](#user-content-installation)
- [Quick Start (CLI)](#user-content-quick-start-cli)
- [Programmatic Usage](#user-content-programmatic-usage)
- [JSON Output Schema](#user-content-json-output-schema)
- [Authentication](#user-content-authentication)
- [Debugging Toolkit](#user-content-debugging-toolkit)
- [Media & Resolution Extraction](#user-content-media-resolution-extraction)
- [Testing & Coverage](#user-content-testing-coverage)
- [CI & Code Quality](#user-content-ci-code-quality)
- [Troubleshooting](#user-content-troubleshooting)
- [FAQ](#user-content-faq)
- [Roadmap Ideas](#user-content-roadmap-ideas)
- [License](#user-content-license)
- [Contributing](#user-content-contributing)
- [Acknowledgments](#user-content-acknowledgments)

---

<a id="key-features"></a>
## ✨ Key Features

| Area | Capabilities |
|------|--------------|
| Validation | Basic scheme check (`rtsp://`, `rtsps://`) w/ early rejection |
| Reachability | TCP preflight + timed DESCRIBE with overall timeout |
| Media Summary | Track type, payload type, clock rate, codec name, basic H264/H265 SPS-derived resolution |
| Diagnostics | Raw error string + optional RTSP trace |
| Auth Retry | Automatic single retry on 401 (Digest) when credentials embedded in URL |
| Debugging | `--debug` flag yields ordered request/response trace + stage markers |
| Library API | Clean interface (`StreamInfo`) with helper methods (HasVideo, FirstVideoMedia, VideoResolutions, MediaTypes) |
| CLI Output | Deterministic JSON (optionally pretty) for integration with scripts / services |

---

<a id="installation"></a>
## 📦 Installation

Library only:
```bash
go get github.com/0x524A/rtspeek
```

CLI (from repo):
```bash
git clone https://github.com/0x524A/rtspeek.git
cd rtspeek
go build ./cmd/rtspeek
./rtspeek --help
```

Add to PATH:
```bash
go install ./cmd/rtspeek
# binary now at $(go env GOPATH)/bin/rtspeek
```

Prebuilt binaries (Linux/macOS/Windows, amd64+arm64) are published for every tagged
release on the [Releases page](https://github.com/0x524A/rtspeek/releases).

Docker (multi-arch, linux/amd64 + linux/arm64):
```bash
docker pull ghcr.io/0x524a/rtspeek:latest
docker run --rm ghcr.io/0x524a/rtspeek:latest --url rtsp://camera.local/stream --timeout 8s
```
Pin to a specific release instead of `latest` with `ghcr.io/0x524a/rtspeek:vX.Y.Z`.
Note: the container has no network access to `camera.local`-style hostnames unless
the target RTSP server is reachable from inside the container (e.g. use `--network host`
on Linux, or the camera's LAN-routable IP).

---

<a id="quick-start-cli"></a>
## 🚀 Quick Start (CLI)

```bash
# Basic probe
rtspeek --url rtsp://camera.local/stream

# Increase timeout
rtspeek --url rtsp://camera.local/stream --timeout 8s

# Verbose diagnostic (stderr) + JSON
rtspeek --url rtsp://bad.host/stream --timeout 3s --verbose

# Include RTSP handshake trace
rtspeek --url rtsp://camera.local/stream --debug --timeout 5s --verbose

# Disable pretty JSON
rtspeek --url rtsp://camera.local/stream --pretty=false
```

Flags:
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--url` | string | (required) | RTSP / RTSPS URL to inspect (credentials may be embedded) |
| `--timeout` | duration | `5s` | Overall deadline (dial + OPTIONS + DESCRIBE + retry) |
| `--pretty` | bool | `true` | Indent JSON output |
| `--verbose` | bool | `false` | Emit failure summary to stderr when applicable |
| `--debug` | bool | `false` | Capture RTSP request/response trace + stage markers |
| `--log-level` | string | `disabled` | Structured log level: `disabled`, `error`, `warn`, `info`, `debug`, `trace`. Only takes effect when `--log-console` is also set |
| `--log-console` | bool | `false` | Enable pretty console logging to stderr (required for `--log-level` to have any effect) |

Exit codes: `0` success (describe may still fail; see `describe_ok`), `1` internal/usage error.

---

<a id="programmatic-usage"></a>
## 🧪 Programmatic Usage

Basic probe:
```go
package main

import (
        "context"
        "fmt"
        "time"
    sd "github.com/0x524A/rtspeek/pkg/rtspeek"
)

func main() {
        ctx := context.Background()
        info, err := sd.DescribeStream(ctx, "rtsp://user:pass@host:554/stream", 5*time.Second)
        if err != nil {
                fmt.Println("describe error:", err)
        }
        fmt.Println("Describe OK:", info.IsDescribeSucceeded())
        fmt.Println("Video Resolutions:", info.GetVideoResolutions())
}
```

Handling partial results — `DescribeStream` can return a non-nil `info` *and* a non-nil `err` at the same time (e.g. TCP connected but DESCRIBE failed), so check `err` and still inspect `info` for whatever was reachable:
```go
info, err := sd.DescribeStream(ctx, url, 5*time.Second)
if err != nil {
        if info != nil {
                fmt.Printf("reachable=%v describe_ok=%v error=%v\n", info.IsReachable(), info.IsDescribeSucceeded(), err)
        } else {
                fmt.Println("connection failed before any RTSP exchange:", err)
        }
        return
}
```

Iterating tracks with `MediaInfo`/`Resolution`:
```go
for _, m := range info.GetMedias() {
        line := fmt.Sprintf("[%d] %s codec=%s", m.Index, m.Type, m.Format)
        if m.Resolution != nil {
                line += " res=" + m.Resolution.String() // e.g. "1920x1080"
        }
        fmt.Println(line)
}
```

See [`examples/video_media_example.go`](examples/video_media_example.go) for a complete runnable example (`go run ./examples`) covering `HasVideo`, `GetFirstVideoMedia`, and the free-function helper variants side by side.

### Interface Surface (`StreamInfo`)

Core accessors (selected):
```go
GetURLString() string
IsReachable() bool
GetProtocolName() string
IsDescribeSucceeded() bool
LatencyMs() float64
GetDebugData() []string
GetVideoMedias() []MediaInfo
GetAudioMedias() []MediaInfo
GetOtherMedias() []MediaInfo
GetMedias() []MediaInfo
GetMediaCount() int
GetVideoResolutions() []Resolution
GetVideoResolutionStrings() []string
GetVideoResolutionString() string
GetMediaTypes() []string
HasVideo() bool
GetFirstVideoMedia() *MediaInfo
Raw() *description.Session // underlying SDP model (not JSON encoded)
```

Helper free functions mirror methods: `GetVideoResolutions(si)`, `GetVideoResolutionStrings(si)`, `GetVideoResolutionString(si)`, `GetMedias(si)`, `HasVideo(si)`, `GetFirstVideoMedia(si)`, `VideoResolutionString(si)` etc.

---

<a id="json-output-schema"></a>
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
    "error": "401 Unauthorized",
    "debug_trace": [
        "STAGE: start",
        "STAGE: options",
        "--> OPTIONS rtsp://camera.local/stream",
        "← 200 OK",
        "STAGE: describe",
        "--> DESCRIBE rtsp://camera.local/stream",
        "← 401 Unauthorized"
    ]
}
```

Key fields:
| Field | Description |
|-------|-------------|
| `reachable` | TCP connect succeeded pre-describe |
| `describe_ok` | DESCRIBE completed with 2xx and SDP parsed |
| `error` | Raw underlying error string (present only on failure) |
| `latency` | Milliseconds from start to final state (float) |
| `debug_trace` | Present only with `--debug`; stage markers + request/response lines (no header detail) |

There is currently no `failure_reason` classification field in the output — inspect `error` directly, or match on the strings above.

---

<a id="authentication"></a>
## 🔐 Authentication
Embed credentials in the URL: `rtsp://user:pass@host:554/stream`.
If first DESCRIBE returns 401 with a digest challenge, a single retry is attempted.

Future improvements (roadmap): multi-round auth, Basic fallback, custom headers.

---

<a id="debugging-toolkit"></a>
## 🛠 Debugging Toolkit
Use `--debug` to capture, in `debug_trace`:
1. Stage markers: `STAGE: start`, `STAGE: options`, `STAGE: describe`, `STAGE: auth-retry`.
2. Every RTSP request line (`--> METHOD url`).
3. Every response status line (`← code message`).

Header-level detail is not included in the trace. This lets you pinpoint stalls (e.g., missing DESCRIBE response).

---

<a id="media-resolution-extraction"></a>
## 🧬 Media & Resolution Extraction
H264 / H265 SPS parsing is used (via mediacommon) to derive width/height when available.
If SPS is absent or parse fails, `resolution` is omitted.

---

<a id="testing-coverage"></a>
## 🧪 Testing & Coverage

Run unit tests:
```bash
go test ./...
```

Generate coverage:
```bash
go test -coverprofile=coverage.out ./pkg/rtspeek
go tool cover -func=coverage.out | head
```

Current indicative coverage (may differ as project evolves): ~50%+ of `pkg/rtspeek` with table-driven RTSP server tests (success, not_found, auth retry) and SPS parsing.

Integration test (tagged):
```bash
go test -tags=integration -run TestDescribeStreamIntegration ./pkg/rtspeek
```

---

<a id="ci-code-quality"></a>
## 🔍 CI & Code Quality

GitHub Actions runs on every push/PR to `main`:

| Workflow | What it does |
|----------|---------------|
| [`sonarcloud.yml`](.github/workflows/sonarcloud.yml) | Runs `go test -coverprofile=coverage.out ./...`, then submits code + coverage to [SonarCloud](https://sonarcloud.io/summary/new_code?id=0x524a_rtspeek) (project `0x524a_rtspeek`, org `0x524a`) for static analysis and quality gate status — see badges above |
| [`lint.yml`](.github/workflows/lint.yml) | Runs [`golangci-lint`](https://golangci-lint.run/) (config in [`.golangci.yml`](.golangci.yml)) — the standard linter set (`errcheck`, `govet`, `staticcheck`, `unused`, etc.) plus `gofmt`/`goimports` formatting checks and `misspell` |
| [`release-dry-run.yml`](.github/workflows/release-dry-run.yml) | Validates [`.goreleaser.yaml`](.goreleaser.yaml) and builds all release binaries + Docker images with `goreleaser release --snapshot --skip=publish` — nothing is published. Catches a broken release/Docker config before merge instead of at tag time |
| [`black-duck-security-scan-ci.yml`](.github/workflows/black-duck-security-scan-ci.yml) | SCA/SAST scanning via Black Duck (SCA, Coverity, Polaris, SRM). **Currently disabled** — it requires license credentials (`BLACKDUCKSCA_TOKEN`, `COVERITY_USER`/`COVERITY_PASSPHRASE`, `POLARIS_ACCESS_TOKEN`, `SRM_API_KEY` secrets, plus matching `*_URL` variables) that aren't configured on this repo. Re-enable with `gh workflow enable "CI Black Duck security scan"` once credentials are set |

Run the same lint checks locally before pushing:
```bash
golangci-lint run ./...
```

**Releasing** (maintainers): push a semver tag to build & publish binaries and a
multi-arch Docker image (to [GHCR](https://github.com/0x524A/rtspeek/pkgs/container/rtspeek))
via [GoReleaser](https://goreleaser.com/) — see [`.goreleaser.yaml`](.goreleaser.yaml),
[`Dockerfile`](Dockerfile), and [`release.yml`](.github/workflows/release.yml):
```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

SonarCloud project settings must have **Automatic Analysis disabled** (Administration → Analysis Method) since analysis is driven by CI here — SonarCloud rejects a CI-based scan while its own automatic scanner is also active for the same project.

---

<a id="troubleshooting"></a>
## 🩹 Troubleshooting
| Symptom (`error` contains) | Likely Cause | Suggested Action |
|---------|--------------|------------------|
| `timed out` / `i/o timeout` | Slow or no DESCRIBE response | Increase `--timeout`, enable `--debug` |
| `401` / `Unauthorized` (w/ creds) | Wrong credentials or unsupported auth scheme | Verify user/pass; server may need Basic; multi-round not yet implemented |
| `connection refused` | Port closed / firewall | Confirm RTSP port; try :554 explicitly |
| `no such host` | Hostname resolution failure | Use IP or fix DNS / /etc/hosts |
| `404` / `Not Found` | Wrong path | Check camera channel/path syntax |
| `resolution` missing | No SPS / parse fail | Ensure stream actually sending SPS NALs |

---

<a id="faq"></a>
## ❓ FAQ
**Q: Does it perform SETUP/PLAY?**  
Not currently; it stops after DESCRIBE.

**Q: Why is latency a float in milliseconds?**  
To provide a human-friendly unit directly without post-processing (higher-level tools can format / round as needed).

**Q: How do I add custom headers?**  
Not exposed yet; will be part of a future extension (see roadmap).

---

<a id="roadmap-ideas"></a>
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

<a id="license"></a>
## 🔑 License
[MIT](LICENSE)

---

<a id="contributing"></a>
## 🤝 Contributing
1. Fork & branch
2. Add tests for new behavior
3. Run `go vet` & `go test`
4. Open PR with clear description / motivation

---

<a id="acknowledgments"></a>
## ❤️ Acknowledgments
Built atop the excellent [`gortsplib`](https://github.com/bluenviron/gortsplib) ecosystem.

---

Enjoy! Feel free to open issues for feature requests or edge cases you encounter.
