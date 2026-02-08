package analysis

import (
	"context"
	"sync"

	"github.com/tphakala/birdnet-go/internal/analysis/processor"
	apiv2 "github.com/tphakala/birdnet-go/internal/api/v2"
	"github.com/tphakala/birdnet-go/internal/myaudio"
)

// startUiSpectrogramPublishers starts all UI spectrogram publishers with the given done channel
func startUiSpectrogramPublishers(wg *sync.WaitGroup, doneChan chan struct{}, proc *processor.Processor, spectrogramChan chan myaudio.UiSpectrogramData, apiController *apiv2.Controller) {
	// Create a merged quit channel that responds to both the done channel and global quit
	mergedQuitChan := make(chan struct{})
	go func() {
		<-doneChan
		close(mergedQuitChan)
	}()

	// Start SSE publisher if API is available
	if apiController != nil {
		startUiSpectrogramSSEPublisherWithDone(wg, mergedQuitChan, apiController, spectrogramChan)
	}
}

// startUiSpectrogramSSEPublisherWithDone starts SSE publisher with a custom done channel
// This is a compatibility wrapper that converts done channel to context for the refactored function
func startUiSpectrogramSSEPublisherWithDone(wg *sync.WaitGroup, doneChan chan struct{}, apiController *apiv2.Controller, spectrogramChan chan myaudio.UiSpectrogramData) {
	// Create context that gets canceled when done channel is closed
	ctx, cancel := context.WithCancel(context.Background())

	// Convert done channel to context cancellation
	go func() {
		select {
		case <-doneChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Call the refactored function with context and receive-only channel
	startUiSpectrogramSSEPublisher(wg, ctx, apiController, spectrogramChan)
}
