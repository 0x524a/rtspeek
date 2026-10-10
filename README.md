<div align="center">

<img src="docs/hero.svg" alt="rtspeek: probe an RTSP stream, get JSON" width="100%">

[![CI](https://github.com/0x524A/rtspeek/actions/workflows/sonarcloud.yml/badge.svg)](https://github.com/0x524A/rtspeek/actions/workflows/sonarcloud.yml) [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=0x524a_rtspeek&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=0x524a_rtspeek) [![Coverage](https://sonarcloud.io/api/project_badges/measure?project=0x524a_rtspeek&metric=coverage)](https://sonarcloud.io/summary/new_code?id=0x524a_rtspeek) [![codecov](https://codecov.io/gh/0x524a/rtspeek/branch/main/graph/badge.svg)](https://codecov.io/gh/0x524a/rtspeek) [![Go Reference](https://pkg.go.dev/badge/github.com/0x524A/rtspeek.svg)](https://pkg.go.dev/github.com/0x524A/rtspeek) [![License](https://img.shields.io/github/license/0x524A/rtspeek)](LICENSE)

</div>

rtspeek checks whether an RTSP stream is up and what is in it. It opens the connection, runs OPTIONS and DESCRIBE, and reports each track's type, codec and, for H.264 and H.265, resolution. Output is JSON, so scripts and services can read it. It is built on [`gortsplib`](https://github.com/bluenviron/gortsplib) and ships as a Go library and a CLI.

It stops after DESCRIBE. It does not SETUP or PLAY, so it never pulls video.

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

**Does it SETUP or PLAY?** No. It stops after DESCRIBE.

**Why is latency a float?** It is milliseconds, so you can read it directly. Round it however you like downstream.

**Can I set custom headers?** Not yet.

## Roadmap

| Idea | Status |
|------|--------|
| Separate dial and describe timeouts | Planned |
| Multi-round auth and Basic fallback | Planned |
| Custom headers and User-Agent | Planned |
| Export the raw SDP as JSON (opt-in) | Planned |
| Optional SETUP and PLAY probe with RTCP stats | Exploratory |

## Contributing

1. Fork and branch.
2. Add tests for new behavior.
3. Run `go vet ./...` and `go test ./...`.
4. Open a PR that says what changed and why.

## License

[MIT](LICENSE). Built on [`gortsplib`](https://github.com/bluenviron/gortsplib).
