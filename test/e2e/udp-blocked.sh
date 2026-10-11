#!/usr/bin/env bash
# E2E T4/T5: needs an RTSP server that refuses UDP. Starts a throwaway
# TCP-only mediamtx on :9554, runs the checks, then removes everything.
# Usage: MEDIAMTX=/path/to/mediamtx RTSPEEK=/tmp/rtspeek test/e2e/udp-blocked.sh
set -u
MEDIAMTX=${MEDIAMTX:-mediamtx}
R=${RTSPEEK:-/tmp/rtspeek}
PORT=9554
B=rtsp://127.0.0.1:$PORT
W=$(mktemp -d)
fail=0

cleanup() {
	[ -n "${PUB:-}" ] && kill "$PUB" 2>/dev/null
	[ -n "${MTX:-}" ] && kill "$MTX" 2>/dev/null
	wait 2>/dev/null
	rm -rf "$W"
}
trap cleanup EXIT

check() { # name, jq filter, expected
	local got
	got=$(jq -c "$2" <<<"$OUT")
	if [ "$got" = "$3" ]; then echo "PASS $1"; else echo "FAIL $1: got $got, want $3"; fail=1; fi
}

cat >"$W/tcp.yml" <<YML
api: no
metrics: no
playback: no
rtmp: no
hls: no
webrtc: no
srt: no
moq: no
rtsp: yes
rtspTransports: [tcp]
rtspAddress: :$PORT
paths:
  all_others:
YML
"$MEDIAMTX" "$W/tcp.yml" >"$W/mtx.log" 2>&1 &
MTX=$!
for _ in $(seq 50); do ss -lnt | grep -q ":$PORT\b" && break; sleep 0.2; done
ss -lnt | grep -q ":$PORT\b" || { echo "mediamtx did not start"; cat "$W/mtx.log"; exit 1; }

ffmpeg -loglevel error -re -f lavfi -i testsrc2=size=640x360:rate=25 \
	-c:v libx264 -preset veryfast -g 50 -bf 0 -f rtsp -rtsp_transport tcp "$B/t4" >"$W/ff.log" 2>&1 &
PUB=$!
sleep 3

OUT=$(timeout 40 "$R" --url "$B/t4" --analyze --analyze-duration 5s --analyze-transport auto)
check "T4 auto falls back to tcp" '[.analysis.transport,.analysis.transport_fallback]' '["tcp",true]'
check "T4 transport_fallback info" '[.analysis.findings[].code]' '["transport_fallback"]'

OUT=$(timeout 40 "$R" --url "$B/t4" --analyze --analyze-duration 5s --analyze-transport udp)
check "T5a udp refused (461)" '[.analysis.findings[].code]' '["setup_failed"]'

exit $fail
