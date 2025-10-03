package rtspeek

import (
	"fmt"

	"github.com/bluenviron/gortsplib/v4/pkg/description"
)

// MediaProcessor handles classification and processing of media streams.
type MediaProcessor struct{}

// NewMediaProcessor creates a new media processor.
func NewMediaProcessor() *MediaProcessor {
	return &MediaProcessor{}
}

// ProcessMedias processes all media streams in a description and populates the stream info.
func (mp *MediaProcessor) ProcessMedias(desc *description.Session, info *streamInfo) error {
	if desc == nil {
		return nil
	}

	for i, media := range desc.Medias {
		mediaInfo, err := mp.classifyMedia(i, media)
		if err != nil {
			return fmt.Errorf("failed to classify media %d: %w", i, err)
		}
		info.MediaCount++

		switch media.Type {
		case description.MediaTypeVideo:
			info.VideoMedias = append(info.VideoMedias, mediaInfo)
		case description.MediaTypeAudio:
			info.AudioMedias = append(info.AudioMedias, mediaInfo)
		default:
			info.OtherMedias = append(info.OtherMedias, mediaInfo)
		}
	}
	return nil
}

// ProcessMediasWithLogging processes media streams and logs the results.
func (mp *MediaProcessor) ProcessMediasWithLogging(desc *description.Session, info *streamInfo, logger *Logger) error {
	if desc == nil {
		return nil
	}

	for i, media := range desc.Medias {
		mediaInfo, err := mp.classifyMedia(i, media)
		if err != nil {
			return fmt.Errorf("failed to classify media %d: %w", i, err)
		}
		info.MediaCount++

		// Log media processing
		if logger != nil {
			resolution := "unknown"
			if mediaInfo.Resolution != nil {
				resolution = fmt.Sprintf("%dx%d", mediaInfo.Resolution.Width, mediaInfo.Resolution.Height)
			}
			logger.MediaProcessing(string(media.Type), i, mediaInfo.Format, resolution)
		}

		switch media.Type {
		case description.MediaTypeVideo:
			info.VideoMedias = append(info.VideoMedias, mediaInfo)
		case description.MediaTypeAudio:
			info.AudioMedias = append(info.AudioMedias, mediaInfo)
		default:
			info.OtherMedias = append(info.OtherMedias, mediaInfo)
		}
	}
	return nil
}

// classifyMedia is a wrapper around the existing classifyMedia function.
// This allows for future enhancement of media processing logic.
func (mp *MediaProcessor) classifyMedia(idx int, m *description.Media) (MediaInfo, error) {
	return classifyMedia(idx, m)
}
