package rtspeek

import "errors"

var (
	ErrInvalidURL     = errors.New("invalid rtsp url")
	ErrUnreachable    = errors.New("rtsp endpoint unreachable")
	ErrDescribeFailed = errors.New("rtsp describe failed")
)
