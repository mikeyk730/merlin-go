package analysis

import (
	"sync"
	"time"

	"github.com/tphakala/birdnet-go/internal/analysis/processor"
	apiv2 "github.com/tphakala/birdnet-go/internal/api/v2"
	"github.com/tphakala/birdnet-go/internal/logger"
	"github.com/tphakala/birdnet-go/internal/myaudio"
	"github.com/tphakala/birdnet-go/internal/observability"
)

// UiSpectrogramManager manages the lifecycle of UI spectrogram monitoring components
type UiSpectrogramManager struct {
	mutex          sync.Mutex
	isRunning      bool
	doneChan       chan struct{}
	wg             sync.WaitGroup
	spectrogramChan chan myaudio.UiSpectrogramData
	proc           *processor.Processor
	apiController  *apiv2.Controller
	metrics        *observability.Metrics
}

// NewUiSpectrogramManager creates a new UI spectrogram manager
func NewUiSpectrogramManager(spectrogramChan chan myaudio.UiSpectrogramData, proc *processor.Processor, apiController *apiv2.Controller, metrics *observability.Metrics) *UiSpectrogramManager {
	return &UiSpectrogramManager{
		spectrogramChan: spectrogramChan,
		proc:           proc,
		apiController:  apiController,
		metrics:        metrics,
	}
}

// Start starts UI spectrogram monitoring if enabled in settings
func (m *UiSpectrogramManager) Start() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	log := GetLogger()
	if m.isRunning {
		log.Debug("UI spectrogram monitoring is already running")
		return nil
	}
	
	// Create done channel for this session
	m.doneChan = make(chan struct{})

	// Start publishers
	startUiSpectrogramPublishers(&m.wg, m.doneChan, m.proc, m.spectrogramChan, m.apiController)

	m.isRunning = true
	log.Info("UI spectrogram monitoring started")
	return nil
}

// Stop stops all UI spectrogram monitoring components
func (m *UiSpectrogramManager) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	log := GetLogger()
	if !m.isRunning {
		log.Debug("UI spectrogram monitoring is not running")
		return
	}

	log.Info("stopping UI spectrogram monitoring")

	// Signal all goroutines to stop
	if m.doneChan != nil {
		close(m.doneChan)
	}

	// Wait for all goroutines to finish with timeout to prevent hanging
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished cleanly
		log.Debug("all UI spectrogram monitoring goroutines stopped cleanly")
	case <-time.After(30 * time.Second):
		// Timeout occurred - force shutdown
		log.Warn("UI spectrogram monitoring shutdown timed out, forcing cleanup",
			logger.Duration("timeout", 30*time.Second))
		// Continue with cleanup anyway - don't hang the system
	}

	// Note: With the centralized logger, file handle cleanup is managed by the central logger
	// No explicit close is needed here

	m.isRunning = false
	m.doneChan = nil
	log.Info("UI spectrogram monitoring stopped")
}

// Restart stops and starts UI spectrogram monitoring with current settings
func (m *UiSpectrogramManager) Restart() error {
	GetLogger().Info("restarting UI spectrogram monitoring")
	m.Stop()
	return m.Start()
}

// IsRunning returns whether UI spectrogram monitoring is currently active
func (m *UiSpectrogramManager) IsRunning() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.isRunning
}
