# Expects the rtspeek binary to already be built (by GoReleaser) for the target
# platform and present in the build context root — this is not a multi-stage
# Go build, see .goreleaser.yaml's `dockers` section.
FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 rtspeek

COPY rtspeek /usr/local/bin/rtspeek

USER rtspeek

ENTRYPOINT ["/usr/local/bin/rtspeek"]
