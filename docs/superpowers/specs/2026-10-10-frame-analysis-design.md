# Frame-level analysis (phase 2)

Date: 2026-10-10
Branch: `feat/analyze-frames`

## Goal

Extend `--analyze` (packet loss, jitter, bitrate) with frame-level checks so a
user can tell why a stream that answers still looks unhealthy: frame rate,
keyframe interval, timestamp jumps and structural bitstream errors.

## Scope

In:
- H.264 and H.265 video tracks.
- Frame rate, keyframe count and interval, RTP timestamp jumps.
- Structural bitstream errors: broken/incomplete frames, missing keyframe,
  keyframe without parameter sets.
- New fields on `TrackAnalysis`, new finding codes, new `Thresholds` fields.

Out (stay on the README roadmap):
- Slice-level parsing (`frame_num` gaps, missing references) and full decode.
- MJPEG, AV1, VP8, VP9. Audio.
- CLI flags for thresholds, RTCP sender-report checks, SRTP.

Unchanged behavior: analysis is off by default, findings never change the exit
code, a failed analysis does not fail DESCRIBE.

## Approach

Inline frame collector. A new `frameCollector` (file `frames.go`) is fed from
the existing `OnPacketRTPAny` callback right after `collector.packet`. Per
H.264/H.265 media it holds a gortsplib `rtph264.Decoder` / `rtph265.Decoder`,
seeded with the SDP parameter sets, and updates counters under one mutex, as
`collector` does.

Rejected: a buffered worker pipeline. A bounded channel can drop packets under
load, which would show up as false `broken_frame` findings and undermine the
accuracy this feature exists for.

## Output

`TrackAnalysis` gains an optional `frames` object, present only for analyzed
H.264/H.265 tracks and omitted otherwise:

```json
"frames": {
  "frames": 120,
  "fps": 24.0,
  "keyframes": 2,
  "keyframe_interval_ms": 2000,
  "timestamp_jumps": 0,
  "broken_frames": 0,
  "has_param_sets": true
}
```

Finding codes (stable once released):

| Code | Severity | Meaning |
|------|----------|---------|
| `no_keyframe` | warn (info if window is shorter than a typical GOP) | No IDR frame in the window |
| `keyframe_interval` | warn | Interval far above the threshold |
| `timestamp_jump` | warn | RTP timestamp went backwards or leapt |
| `broken_frame` | warn/error | Depacketizer failed mid-frame |
| `missing_param_sets` | warn | Keyframe with no SPS/PPS(/VPS) in-band or in SDP |
| `low_fps` | warn | Measured fps far below the rate the SPS (VUI) advertises |
| `frame_analysis_skipped` | info | Unsupported video codec or decoder setup failed |

`Thresholds` gains defaulted fields for keyframe-interval warn level, timestamp
jump size (in frame durations) and the `low_fps` ratio, plus broken-frame
warn/error levels. A zero field selects its default, as today.

## Data flow

1. `Analyze` creates a `frameCollector` beside the `collector`; for each
   H.264/H.265 media it builds a `trackFrames` with the right decoder.
2. The packet callback calls `col.packet(...)` then `fc.packet(m, f, p, now)`.
3. `trackFrames.packet` feeds the decoder.
   - Frame complete: record the RTP timestamp (backwards, or a leap beyond N
     frame durations, counts as a `timestamp_jump`); detect IDR from NAL types
     and update keyframe count and interval; note in-band SPS/PPS/VPS; count
     the frame. Interval and fps use RTP time, not arrival time, so jitter
     does not skew them.
   - `ErrMorePacketsNeeded` is normal and ignored; any other decoder error
     increments `broken_frames`.
4. After the window, `buildTracks` calls `fc.snapshot(m, durationMs)` to fill
   `TrackAnalysis.Frames`. fps = frames over the RTP time span.
5. `evaluate` stays pure and derives the new findings from `TrackAnalysis.Frames`
   and `Thresholds`.

## Error handling

- Frame analysis never fails the analysis. A decoder construction error yields
  an info `frame_analysis_skipped` for that track; packet stats still report.
- The existing `recover` in `Analyze` covers parser panics on malformed NALs.
- Fewer than 2 frames: no fps, no interval, no `low_fps`; never report 0 fps
  from too little data.
- Interrupted stream: `no_keyframe` is suppressed, since a missing keyframe
  then reflects the cut, not the camera.
- `low_fps` compares against SPS timing info (VUI) when present and is skipped
  when the camera advertises no rate, so no expected rate is guessed.

## Testing

- `evaluate`: hand-built `Frames` values, every new code at warn/error boundaries.
- Collector: synthetic packets from gortsplib encoders (`rtph264.Encoder`):
  clean stream, dropped FU-A fragment, timestamp jump, no IDR, IDR without
  SPS/PPS; same for H.265.
- Integration: extend the fake RTSP server in `analyze_server_test.go` to serve
  H.264 and assert `frames` JSON and findings end to end.
- Golden check that `frames` is omitted for audio and unsupported codecs.
- Update the README analyze section and mark the roadmap item done.
