package analysis

import (
	"context"
	"sync"
	"time"

	apiv2 "github.com/tphakala/birdnet-go/internal/api/v2"
	"github.com/tphakala/birdnet-go/internal/logger"
	"github.com/tphakala/birdnet-go/internal/myaudio"
)

// startUiSpectrogramSSEPublisher starts a goroutine to consume UI spectrogram data and publish via SSE
func startUiSpectrogramSSEPublisher(wg *sync.WaitGroup, ctx context.Context, apiController *apiv2.Controller, spectrogramChan <-chan myaudio.UiSpectrogramData) {
	if apiController == nil {
		GetLogger().Warn("SSE API controller not available, UI spectrogram SSE publishing disabled")
		return
	}

	wg.Go(func() {
		GetLogger().Info("Started UI spectrogram SSE publisher")

		for {
			select {
			case <-ctx.Done():
				GetLogger().Info("Stopping UI spectrogram SSE publisher")
				return
			case spectrogramData := <-spectrogramChan:
				// Publish spectrogram data via SSE
				if err := apiController.BroadcastSpectrogram(&spectrogramData); err != nil {
					// Only log errors occasionally to avoid spam
					if time.Now().Unix()%60 == 0 { // Log once per minute at most
						GetLogger().Warn("Error broadcasting UI spectrogram data via SSE",
							logger.Error(err))
					}
				}
			}
		}
	})
}
