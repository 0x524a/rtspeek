<div align="center">

<img src="docs/hero.svg" alt="rtspeek: probe an RTSP stream, get JSON" width="100%">

[![CI](https://github.com/0x524A/rtspeek/actions/workflows/sonarcloud.yml/badge.svg)](https://github.com/0x524A/rtspeek/actions/workflows/sonarcloud.yml) [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=0x524a_rtspeek&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=0x524a_rtspeek) [![Coverage](https://sonarcloud.io/api/project_badges/measure?project=0x524a_rtspeek&metric=coverage)](https://sonarcloud.io/summary/new_code?id=0x524a_rtspeek) [![codecov](https://codecov.io/gh/0x524a/rtspeek/branch/main/graph/badge.svg)](https://codecov.io/gh/0x524a/rtspeek) [![Go Reference](https://pkg.go.dev/badge/github.com/0x524A/rtspeek.svg)](https://pkg.go.dev/github.com/0x524A/rtspeek) [![License](https://img.shields.io/github/license/0x524A/rtspeek)](LICENSE)

</div>

rtspeek checks whether an RTSP stream is up and what is in it. It opens the connection, runs OPTIONS and DESCRIBE, and reports each track's type, codec and, for H.264 and H.265, resolution. Output is JSON, so scripts and services can read it. It is built on [`gortsplib`](https://github.com/bluenviron/gortsplib) and ships as a Go library and a CLI.

By default it stops after DESCRIBE and never pulls video. With `--analyze` it also receives the stream for a few seconds and reports packet loss, jitter and bitrate; see [Analyze a stream](#analyze-a-stream).

## Install

Prebuilt binaries for Linux, macOS and Windows (amd64 and arm64) are on the [Releases page](https://github.com/0x524A/rtspeek/releases).

```bash
# Docker (linux/amd64, linux/arm64)
docker run --rm ghcr.io/0x524a/rtspeek:latest --url rtsp://camera.local/stream

# From source
go install github.com/0x524A/rtspeek/cmd/rtspeek@latest

# As a library
go get github.com/0x524A/rtspeek
```

Pin a Docker release with `ghcr.io/0x524a/rtspeek:vX.Y.Z`. The container can only reach hosts that are routable from inside it, so `camera.local` will not resolve unless you use `--network host` (Linux) or the camera's LAN IP.

## Use the CLI

```bash
rtspeek --url rtsp://camera.local/stream
rtspeek --url rtsp://user:pass@camera.local/stream --timeout 8s
rtspeek --url rtsp://camera.local/stream --debug      # include the handshake trace
rtspeek --url rtsp://bad.host/stream --verbose        # failure summary on stderr
rtspeek --url rtsp://camera.local/stream --pretty=false
```

| Flag | Default | What it does |
|------|---------|--------------|
| `--url` | required | RTSP or RTSPS URL. Credentials may be embedded. |
| `--timeout` | `5s` | One deadline for dial, OPTIONS, DESCRIBE and the auth retry. |
| `--pretty` | `true` | Indent the JSON. |
| `--verbose` | `false` | Print a failure summary to stderr. |
| `--debug` | `false` | Add `debug_trace` to the JSON: stage markers plus each request and response line. |
| `--analyze` | `false` | Receive the stream after DESCRIBE and add an `analysis` object to the JSON. **Pulls live video and uses a camera session.** |
| `--analyze-duration` | `5s` | How long to receive media once the first packet arrives. Maximum `60s`. |
| `--analyze-transport` | `auto` | `auto` (UDP, falling back to TCP), `udp` or `tcp`. |
| `--log-level` | `disabled` | `disabled`, `error`, `warn`, `info`, `debug` or `trace`. Only applies with `--log-console`. |
| `--log-console` | `false` | Write pretty structured logs to stderr. |

Exit code `0` means the tool ran, even if the stream did not answer. Check `describe_ok` for that. Exit code `1` means a usage or internal error.

## Read the output

A healthy stream:

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

A stream that answered the TCP connection but rejected DESCRIBE, with `--debug`:

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

| Field | Meaning |
|-------|---------|
| `reachable` | The TCP connection succeeded before DESCRIBE. |
| `describe_ok` | DESCRIBE returned 2xx and the SDP parsed. |
| `latency` | Milliseconds from start to the final state. |
| `media_count` | Number of tracks. |
| `video_medias`, `audio_medias`, `other_medias` | Tracks by kind. Empty groups are left out. |
| `error` | The underlying error text. Present only on failure. |
| `debug_trace` | Present only with `--debug`. Header contents are not included. |

Resolution comes from the H.264 or H.265 SPS. If the stream sends no SPS, or it does not parse, `resolution` is left out.

The JSON has no failure-category field. Go callers can get one from the error; see [Classify failures](#classify-failures).

### Authentication

Put credentials in the URL: `rtsp://user:pass@host:554/stream`. If the first DESCRIBE returns 401 with a digest challenge, rtspeek retries once. Multi-round auth and Basic fallback are not implemented.

## Analyze a stream

`--analyze` goes past DESCRIBE: it runs SETUP and PLAY, receives media for `--analyze-duration`, then tears the session down. Use it when a stream answers but looks unhealthy.

> **Warning:** analysis pulls live video and holds a camera session for the whole window. Some cameras allow only a few concurrent sessions, so analyzing one can push off a recorder that is already connected. It is off unless you ask for it, and the default output is unchanged.

```bash
rtspeek --url rtsp://camera.local/stream --analyze
rtspeek --url rtsp://camera.local/stream --analyze --analyze-duration 15s --analyze-transport udp
```

```json
"analysis": {
  "duration_ms": 5003.1,
  "requested_duration_ms": 5000,
  "transport": "udp",
  "tracks": [
    {
      "index": 0,
      "type": "video",
      "format": "H264",
      "packets": 4120,
      "lost": 37,
      "loss_percent": 0.89,
      "jitter_ms": 4.1,
      "bytes": 1445000,
      "bitrate_kbps": 2310.5,
      "rtcp_packets": 2,
      "decode_errors": 0
    }
  ],
  "findings": [
    { "severity": "warn", "code": "packet_loss", "track": 0, "value": 0.89, "message": "0.89% RTP packet loss on track 0" }
  ]
}
```

`findings` is always an array, sorted by severity. Findings never change the exit code; read them in the output. A failed analysis does not fail the describe: `describe_ok` stays as DESCRIBE left it, and `analysis.error` says what went wrong.

| Code | Severity | When |
|------|----------|------|
| `packet_loss` | warn at 0.5%, error at 2% | RTP packets missing from the sequence |
| `high_jitter` | warn at 30 ms, error at 100 ms | RFC 3550 interarrival jitter |
| `no_packets` | error | No RTP arrived on any track, or on one track while others delivered |
| `stream_interrupted` | error | The connection ended during the window |
| `setup_failed` | error | The server rejected SETUP |
| `play_failed` | error | The server rejected PLAY |
| `transport_fallback` | info | No UDP packets arrived, so the client switched to TCP |

These codes are stable. Thresholds are set in the library through `AnalyzeOptions.Thresholds`; CLI flags for them are planned.

**Transport matters.** UDP shows real loss on the path from the camera to this machine. TCP retransmits, so it hides network loss and mostly shows problems at the source. `auto` tries UDP first and falls back to TCP after about 3 seconds of silence, and reports that as `transport_fallback`.

**Time limit.** One analysis takes at most `--timeout` + the first-packet wait (5s, plus 3s in `auto`) + `--analyze-duration` + 2s. A stream that never delivers packets ends after the first-packet wait with `no_packets`.

**Jitter** is measured from packet arrival times. Video encoders send each frame as a burst, which raises it slightly even on a clean network.

Out-of-order and duplicate packet counts are not reported: the underlying RTSP library reorders and drops duplicates before rtspeek sees them.

From Go:

```go
info, err := rtspeek.DescribeStreamWithOptions(ctx, url, rtspeek.Options{
    Timeout: 5 * time.Second,
    Analyze: &rtspeek.AnalyzeOptions{Duration: 10 * time.Second, Transport: rtspeek.TransportUDP},
})
if a := info.GetAnalysis(); a != nil {
    for _, f := range a.Findings {
        fmt.Println(f.Severity, f.Code, f.Message)
    }
}
```

## Use the library

```go
import (
    "context"
    "fmt"
    "time"

    "github.com/0x524A/rtspeek/pkg/rtspeek"
)

func main() {
    info, err := rtspeek.DescribeStream(context.Background(),
        "rtsp://user:pass@host:554/stream", 5*time.Second)
    if err != nil {
        // info can be non-nil here: TCP connected, DESCRIBE failed.
        if info != nil {
            fmt.Printf("reachable=%v describe_ok=%v err=%v\n",
                info.IsReachable(), info.IsDescribeSucceeded(), err)
        } else {
            fmt.Println("failed before any RTSP exchange:", err)
        }
        return
    }
    for _, m := range info.GetMedias() {
        line := fmt.Sprintf("[%d] %s %s", m.Index, m.Type, m.Format)
        if m.Resolution != nil {
            line += " " + m.Resolution.String() // "1920x1080"
        }
        fmt.Println(line)
    }
}
```

A complete example with `HasVideo`, `GetFirstVideoMedia` and the free-function helpers is in [`examples/video_media_example.go`](examples/video_media_example.go). Run it with `go run ./examples`.

### StreamInfo

```go
GetURLString() string
IsReachable() bool
GetProtocolName() string
IsDescribeSucceeded() bool
LatencyMs() float64
GetDebugData() []string
GetMedias() []MediaInfo
GetVideoMedias() []MediaInfo
GetAudioMedias() []MediaInfo
GetOtherMedias() []MediaInfo
GetMediaCount() int
GetMediaTypes() []string
GetVideoResolutions() []Resolution
GetVideoResolutionStrings() []string
GetVideoResolutionString() string
HasVideo() bool
GetFirstVideoMedia() *MediaInfo
GetAnalysis() *Analysis      // nil unless analysis was requested
Raw() *description.Session // underlying SDP model, not JSON encoded
```

Each method has a free-function twin that takes a `StreamInfo`, such as `HasVideo(si)` and `GetMedias(si)`.

### Classify failures

Errors from `DescribeStream` carry a reason. Read it with `FailureReason`, or unwrap to `*StreamError` with `errors.As`.

```go
_, err := rtspeek.DescribeStream(ctx, url, 5*time.Second)
switch rtspeek.FailureReason(err) {
case rtspeek.ReasonAuthRequired:
    // ask for credentials
case rtspeek.ReasonTimeout, rtspeek.ReasonConnectionRefused:
    // retry later
}
```

| Reason | Meaning |
|--------|---------|
| `connection_refused` | Port closed or a firewall is in the way. |
| `timeout` | No answer inside the deadline. |
| `dns_error` | The hostname did not resolve. |
| `connection_closed` | The server closed the connection mid-exchange. |
| `auth_required` | 401 or an authentication challenge. |
| `not_found` | 404, usually a wrong path. |
| `unsupported_scheme` | The scheme is not `rtsp://` or `rtsps://`. |
| `other` | Anything else, including some invalid-URL and media-processing errors. |

`StreamError.Error()` returns the original message, so existing string matching keeps working.

### Logging

Pass a logger through the context to trace a call:

```go
logger := rtspeek.NewLogger(rtspeek.LogLevelDebug, os.Stderr, false)
ctx := rtspeek.WithLogger(context.Background(), logger)
info, err := rtspeek.DescribeStream(ctx, url, 5*time.Second)
```

## Troubleshoot

| `error` contains | Likely cause | What to try |
|------------------|--------------|-------------|
| `timed out`, `i/o timeout` | Slow or missing DESCRIBE response | Raise `--timeout`, add `--debug` to see where it stalls. |
| `401`, `Unauthorized` | Wrong credentials, or an auth scheme other than digest | Check user and password. |
| `connection refused` | Port closed or blocked | Confirm the RTSP port; try `:554` explicitly. |
| `no such host` | DNS failure | Use the IP address or fix DNS. |
| `404`, `Not Found` | Wrong stream path | Check the camera's channel and path syntax. |
| `resolution` missing | No SPS in the SDP, or it failed to parse | Confirm the stream sends SPS NAL units. |

## Develop

```bash
go test ./...
golangci-lint run ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1
go test -tags=integration -run TestDescribeStreamIntegration ./pkg/rtspeek
```

GitHub Actions runs on every push and PR to `main`:

| Workflow | What it does |
|----------|--------------|
| [`sonarcloud.yml`](.github/workflows/sonarcloud.yml) | Runs `go test -coverprofile=coverage.out ./...` and sends code and coverage to [SonarCloud](https://sonarcloud.io/summary/new_code?id=0x524a_rtspeek) (project `0x524a_rtspeek`, org `0x524a`). |
| [`lint.yml`](.github/workflows/lint.yml) | Runs [`golangci-lint`](https://golangci-lint.run/) with [`.golangci.yml`](.golangci.yml): the standard linters plus `gofmt`, `goimports` and `misspell`. |
| [`release-dry-run.yml`](.github/workflows/release-dry-run.yml) | Builds all release binaries and Docker images with `goreleaser release --snapshot --skip=publish`, so a broken release config fails before merge. Nothing is published. |
| [`black-duck-security-scan-ci.yml`](.github/workflows/black-duck-security-scan-ci.yml) | Black Duck SCA and SAST scanning. **Disabled**: the license credentials are not configured on this repo. Re-enable with `gh workflow enable "CI Black Duck security scan"` once they are. |

[Dependabot](.github/dependabot.yml) opens weekly PRs for Go modules (minor and patch grouped), GitHub Actions (grouped) and the `Dockerfile` base image.

SonarCloud must have **Automatic Analysis disabled** (Administration → Analysis Method). CI drives the analysis, and SonarCloud rejects a CI scan while its own scanner is also active.

**Releasing** (maintainers): push a semver tag to publish binaries and a multi-arch image to [GHCR](https://github.com/0x524A/rtspeek/pkgs/container/rtspeek) through [GoReleaser](https://goreleaser.com/). See [`.goreleaser.yaml`](.goreleaser.yaml), [`Dockerfile`](Dockerfile) and [`release.yml`](.github/workflows/release.yml).

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

## FAQ

**Does it SETUP or PLAY?** Only with `--analyze`. Without it, rtspeek stops after DESCRIBE.

**Why is latency a float?** It is milliseconds, so you can read it directly. Round it however you like downstream.

**Can I set custom headers?** Not yet.

## Roadmap

| Idea | Status |
|------|--------|
| Separate dial and describe timeouts | Planned |
| Multi-round auth and Basic fallback | Planned |
| Custom headers and User-Agent | Planned |
| Export the raw SDP as JSON (opt-in) | Planned |
| Frame-level analysis: frame rate, keyframe interval, timestamp jumps, bitstream errors | Planned |
| Parameter-set, resolution and RTCP sender-report checks in analysis | Planned |
| Threshold flags for `--analyze` | Planned |
| SRTP (`rtsps://`) support in analysis | Planned |

## Contributing

1. Fork and branch.
2. Add tests for new behavior.
3. Run `go vet ./...` and `go test ./...`.
4. Open a PR that says what changed and why.

## License

[MIT](LICENSE). Built on [`gortsplib`](https://github.com/bluenviron/gortsplib).
