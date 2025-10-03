package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/0x524A/rtspeek/pkg/rtspeek"
)

func main() {
	// Example showing how to use FirstVideoMedia() to get full media object
	ctx := context.Background()
	streamInfo, err := rtspeek.DescribeStream(ctx, "rtsp://example.com/stream", 5*time.Second)
	if err != nil {
		log.Printf("Error describing stream: %v", err)
		return
	}

	if !streamInfo.HasVideo() {
		log.Println("Stream has no video tracks")
		return
	}

	// Get the first video media object
	videoMedia := streamInfo.GetFirstVideoMedia()
	if videoMedia == nil {
		log.Println("No video media found")
		return
	}

	// Now you can interact with the full media object
	fmt.Printf("Video Media Info:\n")
	fmt.Printf("  Index: %d\n", videoMedia.Index)
	fmt.Printf("  Type: %s\n", videoMedia.Type)
	fmt.Printf("  Format: %s\n", videoMedia.Format)

	if videoMedia.ClockRate != nil {
		fmt.Printf("  Clock Rate: %d\n", *videoMedia.ClockRate)
	}

	if videoMedia.PayloadType != nil {
		fmt.Printf("  Payload Type: %d\n", *videoMedia.PayloadType)
	}

	if videoMedia.Resolution != nil {
		fmt.Printf("  Resolution: %dx%d\n", videoMedia.Resolution.Width, videoMedia.Resolution.Height)
		fmt.Printf("  Resolution (string): %s\n", videoMedia.Resolution.String())
	}

	if len(videoMedia.CodecSpecific) > 0 {
		fmt.Printf("  Codec Specific Data:\n")
		for key, value := range videoMedia.CodecSpecific {
			fmt.Printf("    %s: %v\n", key, value)
		}
	}

	// NEW: Get resolution as a string directly
	resolutionStr := streamInfo.GetVideoResolutionString()
	if resolutionStr != "" {
		fmt.Printf("  Video Resolution (string): %s\n", resolutionStr)
	}

	// NEW: Get all video resolutions as strings
	resolutionStrings := streamInfo.GetVideoResolutionStrings()
	if len(resolutionStrings) > 0 {
		fmt.Printf("  All Video Resolutions: %v\n", resolutionStrings)
	}

	// NEW: Get all media items (video, audio, other combined)
	allMedias := streamInfo.GetMedias()
	fmt.Printf("  Total Media Count: %d\n", len(allMedias))
	for i, media := range allMedias {
		fmt.Printf("    Media[%d]: Type=%s, Format=%s, Index=%d\n",
			i, media.Type, media.Format, media.Index)
	}

	// Using convenience functions
	fmt.Printf("  Video Resolution String (func): %s\n", rtspeek.VideoResolutionString(streamInfo))
	fmt.Printf("  All Resolution Strings (func): %v\n", rtspeek.GetVideoResolutionStrings(streamInfo))
	fmt.Printf("  All Medias (func): %d items\n", len(rtspeek.GetMedias(streamInfo)))

	// For backward compatibility, you can still get just the resolution
	if resolution := rtspeek.FirstVideoResolution(streamInfo); resolution != nil {
		fmt.Printf("  Resolution (legacy): %dx%d\n", resolution.Width, resolution.Height)
	}
}
