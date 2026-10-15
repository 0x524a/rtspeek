# End-to-end test plan: `--analyze`

Covers describe, packet/jitter/bitrate analysis and frame-level analysis (fps,
keyframes, B-frame reordering, timestamp jumps, broken frames) against live RTSP
streams. Unit and fake-server tests run in CI; this plan is the manual layer on
top, using real encoders, a real RTSP server and, optionally, a real camera.

## 1. Environment

| Item | Setup |
|---|---|
| Binary | `go build -o /tmp/rtspeek ./cmd/rtspeek` |
| RTSP server | mediamtx v1.21.2 or later, separate from any production instance |
| Encoder | ffmpeg 6.x with libx264 and libx265 |
| Tools | `jq`, `ss` |

mediamtx config (`test.yml`). It needs UDP and TCP, and nothing else:

```yaml
api: no
metrics: no
playback: no
rtmp: no
hls: no
webrtc: no
srt: no
rtsp: yes
rtspTransports: [udp, multicast, tcp]
rtspAddress: :8554
rtpAddress: :8000
rtcpAddress: :8001
paths:
  all_others:
```

Start: `./mediamtx test.yml`. Check: `ss -lntu | grep -E ':(8554|8000|8001)\b'` shows
TCP 8554 and UDP 8000 and 8001.

Common shell variables:

```bash
B=rtsp://127.0.0.1:8554
S="testsrc2=size=640x360:rate=25"
R=/tmp/rtspeek
pub() { n=$1; shift; setsid nohup ffmpeg -loglevel error -re -f lavfi -i "$S" "$@" \
        -f rtsp -rtsp_transport tcp $B/$n >/tmp/$n.log 2>&1 & }
an()  { $R --url "$1" --analyze --analyze-duration "${2:-10s}" --analyze-transport "${3:-tcp}"; }
```

Start each publisher, wait about 3 s, then analyze. Stop publishers afterwards:
`pgrep -f "^ffmpeg.*$B/" | xargs -r kill`.

Use `jq` to read `.analysis.tracks[0]` and `.analysis.findings[].code`.

## 2. Scenarios

Each row: publish command, analyze command, expected result. "No findings" means
`.analysis.findings == []`.

### 2.1 Baseline and existing features

| ID | Scenario | Setup | Expected |
|---|---|---|---|
| B1 | Describe only | `$R --url $B/s` (no `--analyze`) | `reachable` true, `describe_ok` true, no `analysis` key |
| B2 | Unreachable | `$R --url rtsp://127.0.0.1:1/x --timeout 2s` | `reachable` false, clear connection error |
| B3 | Duration cap | `--analyze-duration 90s` | Rejected: "exceeds the maximum of 1m0s" |
| B4 | Clean video | `pub b4 -c:v libx264 -preset veryfast -g 50 -bf 0`, analyze 10 s | lost 0, jitter under 5 ms, fps 25, keyframe interval 2000 ms, no findings |
| B5 | Audio and video | add `-f lavfi -i sine=frequency=440 -c:a aac` to a video publisher | Video track has `frames`; audio track has none and no skip note |
| B6 | Unsupported video codec | publish MPEG-4 or MJPEG | `frames_skipped` set, info finding `frame_analysis_skipped` |

### 2.2 Transport

| ID | Scenario | Setup | Expected |
|---|---|---|---|
| T1 | UDP | analyze B4 with `udp` | `transport` udp, packets above 0, RTCP packets at least 1 |
| T2 | TCP | analyze B4 with `tcp` | `transport` tcp, same packet count as T1 within 5% |
| T3 | Auto picks UDP | analyze B4 with `auto` | `transport` udp, no `transport_fallback` |
| T4 | Auto falls back | TCP-only server (section 4.1), `auto` | `transport` tcp, info `transport_fallback` |
| T5a | UDP forced, refused | TCP-only server (section 4.1), `udp` | error `setup_failed`, "461 (Unsupported Transport)" |
| T5b | UDP forced, packets blocked | impairment proxy `-loss 1` (section 4), `udp` | error `no_packets` |

### 2.3 Frame analysis

| ID | Scenario | Publish flags | Expected |
|---|---|---|---|
| F1 | No B-frames | `-c:v libx264 -g 50 -bf 0 -profile:v baseline` | `reordered_frames` 0, `declared_reorder_depth` 0, no findings |
| F2 | B-frames, depth 1 | `-c:v libx264 -g 50 -bf 2 -x264-params scenecut=0` | `reordered_frames` over 100 in 10 s, `max_reorder_depth` 1, info `frame_reordering`, no `timestamp_jump` |
| F3 | B-frames, depth 2 | `-c:v libx264 -preset slow -x264-params bframes=4:b-pyramid=normal:b-adapt=0:keyint=50:min-keyint=50:scenecut=0` | `max_reorder_depth` 2 equals `declared_reorder_depth` |
| F4 | H.265 with B-frames | `-c:v libx265 -preset ultrafast -x265-params bframes=3:keyint=50:min-keyint=50:scenecut=0:log-level=error` | depth 2, fps about 25, keyframe interval 2000 ms |
| F5 | Frame rate | `-vf fps=10 -c:v libx264 -g 20` | fps about 10, `advertised_fps` 10, no `low_fps` |
| F6 | Long GOP | `-g 250 -bf 0 -x264-params keyint=250:min-keyint=250:scenecut=0`, analyze 25 s | `keyframe_interval` warn at 10000 ms |
| F7 | Short window, long GOP | same as F6, analyze 6 s | one keyframe at most, info `no_keyframe`, never a warn |
| F8 | Timestamp leap | `-vf "setpts='PTS+if(gte(N,250),100000,0)'" -fps_mode passthrough -g 50 -bf 0`, analyze 22 s from publisher start | `timestamp_jumps` exactly 1, fps still 25, warn `timestamp_jump` |
| F9 | Frame rate drop | `-vf "setpts='if(lt(N,250),PTS,PTS+(N-250)*4)'" -fps_mode passthrough -g 50 -bf 0`, analyze 22 s | no `timestamp_jump`, fps between 5 and 25, warn `low_fps`. A `high_jitter` warn (about 35 ms) is expected too: the rate change shifts RFC 3550 transit time and the estimate decays slowly at 6 packets/s. A constant `-vf fps=6` stream shows 6 ms |
| F10 | Missing parameter sets | not reproducible with mediamtx, which adds SPS/PPS from the SDP to keyframes | covered by unit tests only |

### 2.4 Jitter

| ID | Scenario | Setup | Expected |
|---|---|---|---|
| J1 | B-frames do not look like jitter | analyze F2 and F3 | jitter under 10 ms, no `high_jitter` (regression for the B-frame jitter fix) |
| J2 | Intermittent delay | proxy with `-spike-prob 0.05 -spike 300ms -seed 7` | `jitter_ms` stays low (about 2 ms) but `jitter_peak_ms` is 30 or more and `high_jitter` warns ("peaked at") |
| J3 | Heavy delay | proxy with `-delay-max 1500ms -reorder` | `high_jitter` error, peak 100 ms or more |
| J4 | Clean streams stay quiet | B-pyramid (F3) and a real camera, TCP and UDP | `jitter_peak_ms` under 20 ms, no `high_jitter` |

### 2.5 Loss and broken frames

Uses the impairment proxy (section 4). These cannot be produced on plain loopback.

| ID | Scenario | Impairment | Expected |
|---|---|---|---|
| L1 | Light loss | drop 1% of RTP packets | `packet_loss` warn, `lost` above 0, some `broken_frames` |
| L2 | Heavy loss | drop 5% | `packet_loss` error, `broken_frame` warn or error by share of frames |
| L3 | Lost fragment | drop one fragment of a large keyframe | `lost` 1, `broken_frames` 1. One frame in 300 is 0.33%, under the 0.5% limit, so no `broken_frame` finding |
| L4 | Stream cut | kill the publisher mid-window | `stream_interrupted`, no `no_keyframe` finding, partial stats still reported |

### 2.6 Real camera

Run only with the owner's permission. It opens a live session and pulls video.

| ID | Scenario | Expected |
|---|---|---|
| C1 | TCP, 15 s | fps matches the camera setting, keyframe interval plausible, no findings on a healthy stream |
| C2 | UDP, 15 s | same result as C1, or `transport_fallback` / `no_packets` if the network blocks UDP |
| C3 | Camera with B-frames | `frame_reordering` info and no `high_jitter` caused by it |

### 2.7 CLI

| ID | Scenario | Expected |
|---|---|---|
| V1 | `--version` on a plain `go build` | `rtspeek version v0.x.y-...` from build info |
| V2 | build with `-ldflags "-X main.version=v9.9.9"` | `rtspeek version v9.9.9` |
| V3 | `go run ./cmd/rtspeek --version` | `rtspeek version dev` |

### 2.8 Robustness

| ID | Scenario | Expected |
|---|---|---|
| R1 | 8 parallel analyses of different streams | all complete, no crashes, results match single runs |
| R2 | Same stream analyzed back to back 10 times | stable fps, no goroutine or session leaks (`ss -tn` shows no stuck connections) |
| R3 | Run with the race detector | `go test -race -count=1 ./...` passes |

## 3. Pass criteria

- Every expected result above holds.
- No panic, hang or leaked ffmpeg or RTSP session.
- No false positives on clean streams: B1 to B5 and F1 to F5 produce no warn or
  error findings, apart from the info `frame_reordering` where B-frames exist.
- `go test -race -count=1 ./...` and `golangci-lint run ./...` are clean, and
  coverage stays at or above the repo target.

## 4. Impairment proxy

`test/udpproxy` sits between the analyzer and mediamtx. RTSP control and
TCP-interleaved media pass through; the UDP RTP path towards the client is
impaired. It rewrites URLs and Transport headers, so it does not work with RTSP
authentication.

```bash
go build -o /tmp/udpproxy ./test/udpproxy
/tmp/udpproxy -listen 127.0.0.1:8555 -upstream 127.0.0.1:8554 [flags]   # Ctrl-C prints totals
$R --url rtsp://127.0.0.1:8555/PATH --analyze --analyze-transport udp
```

| Flag | Effect |
|---|---|
| `-loss p` | drop each RTP packet with probability p |
| `-delay-min`, `-delay-max` | hold each packet for a random time in that range |
| `-spike-prob p`, `-spike d` | add an extra delay d to a fraction p of packets |
| `-reorder` | let delayed packets overtake earlier ones (default: keep order) |
| `-drop-idr-fragment N` | drop one middle fragment of the Nth H.264 IDR frame |
| `-seed n` | repeatable runs |

Results with a 25 fps H.264 stream, UDP, 10 to 12 s:

| Case | Flags | Observed |
|---|---|---|
| Passthrough | none | 0 lost, jitter 3 ms, no findings |
| L1 | `-loss 0.01` | 1.2% lost, 7 broken frames, warn `packet_loss`, `broken_frame` |
| L2 | `-loss 0.05` | 5.0% lost, 30 broken frames, fps 21.5, `packet_loss`, `broken_frame` |
| L3 | `-drop-idr-fragment 2` | 1 lost packet, exactly 1 broken frame |
| Heavy reorder | `-delay-max 1500ms -reorder` | 473 lost, jitter 98 ms, fps 1.7; `packet_loss`, `broken_frame` (error), `high_jitter`, `low_fps`, `no_keyframe` |
| TCP through the proxy | `-loss 0.5` | no effect, 0 lost (TCP path is untouched) |

**Jitter limits.** `jitter_ms` is the RFC 3550 estimate at the end of the window,
measured after gortsplib's RTP reorder buffer. With in-order delivery every packet
behind a late one waits for it, and the end value hides a burst that ended earlier
(`-spike-prob 0.05 -spike 300ms`: 2 to 4 ms at the end). `jitter_peak_ms` is the
highest value during the window, with the first 32 packets skipped, and the finding
uses the higher of the two. Measured on 25 fps H.264, UDP, 12 s:

| Case | Flags | `jitter_ms` | `jitter_peak_ms` |
|---|---|---|---|
| Passthrough | none | 0.4 to 5 | 9 to 12 |
| Delay in order | `-delay-max 500ms` | 22 to 29 | 30 to 34 |
| Spikes | `-spike-prob 0.05 -spike 300ms` | 2 to 4 | 34 to 35 |
| Heavy | `-delay-max 1500ms -reorder` | 48 | 121 |
| B-pyramid, no proxy | none | 5 to 11 | 14 to 21 |
| Real camera, TCP | none | 0 to 2.6 | 11 to 12 |

A per-frame estimate (one sample per RTP timestamp) was tried and dropped: it reads
about 31 ms on healthy B-frame and camera streams, so it cannot carry a 30 ms
threshold.

### 4.1 TCP-only server (T4, T5a)

`test/e2e/udp-blocked.sh` runs these without touching the main test server. It
starts a throwaway mediamtx with `rtspTransports: [tcp]` on port 9554 (config in a
temp directory, `moq: no` so it does not clash with another instance on 8892),
publishes one H.264 stream, runs T4 and T5a, then stops mediamtx and ffmpeg and
deletes the temp directory.

```bash
MEDIAMTX=/path/to/mediamtx RTSPEEK=/tmp/rtspeek test/e2e/udp-blocked.sh
```

It exits 1 if a check fails. Afterwards `ss -lnt | grep 9554` shows nothing.
T5b needs no extra server: use the impairment proxy with `-loss 1` against the
normal test server.

## 5. Known limits

- Frame analysis covers H.264 and H.265 only.
- The structural checks use the gortsplib decoders; slice-level corruption is out
  of scope.
- RTP carries no DTS, so decode timestamps are not reported.
- Reorder depth is capped at 16 frames, and a short window can miss B-frames that
  are used rarely.
- The camera test on a private network cannot run in CI.
