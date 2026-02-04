<!--
MerlinPage.svelte - Main dashboard page with bird detection summaries

Purpose:
- Central dashboard displaying daily species summaries and recent detections
- Provides real-time updates via Server-Sent Events (SSE)

Features:
- Real-time detection updates via SSE with animations

Props: None (Page component)

State Management:
- speciesSummary: Array of species detection summaries for the selected date

Performance Optimizations:
- Efficient animation cleanup with requestAnimationFrame
- Map-based lookups for O(1) species updates
-->
<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import ReconnectingEventSource from 'reconnecting-eventsource';
  import MerlinResultsGrid from '$lib/desktop/features/dashboard/components/MerlinResultsGrid.svelte';
  import { t } from '$lib/i18n';
  import type { MerlinSpeciesSummary, ModelPredictions, SoundRecognition } from '$lib/types/detection.types';
  import { getLogger } from '$lib/utils/logger';
  import { safeArrayAccess, isPlainObject } from '$lib/utils/security';
  import { api } from '$lib/utils/api';

  const logger = getLogger('app');

  function isModelPredictions(v: unknown): v is ModelPredictions {
    if (!isPlainObject(v)) return false;
    return true;
    //const o = v as Record<string, unknown>;
    //const dateOk = typeof o.Date === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(o.Date);
    //const timeOk = typeof o.Time === 'string' && /^\d{2}:\d{2}:\d{2}$/.test(o.Time);
    //return (
    //  typeof o.ID === 'number' &&
    //  typeof o.CommonName === 'string' &&
    //  o.CommonName.length > 0 &&
    //  typeof o.ScientificName === 'string' &&
    //  o.ScientificName.length > 0 &&
    //  typeof o.Confidence === 'number' &&
    //  dateOk &&
    //  timeOk &&
    //  typeof o.scientificName === 'string' &&
    //  o.scientificName.length > 0
    //);
  }

  // State management
  let speciesSummary = $state<MerlinSpeciesSummary[]>([]);

  // Animation state for new detections
  let newDetectionIds = $state(new Set<string>()); //todo:mdk use this?

  // Manual refresh function that works with both SSE and polling
  function handleManualRefresh() {
    // Clear animation state on manual refresh
    newDetectionIds.clear();
  }

  // Animation cleanup timers and RAF manager - use $state.raw() for performance
  let animationCleanupTimers = $state.raw(new Set<ReturnType<typeof setTimeout>>());
  let animationFrame: number | null = null;
  let pendingCleanups = $state.raw(new Map<string, { fn: () => void; timestamp: number }>());

  // Clear animation states from daily summary
  function clearDailySummaryAnimations() {
    speciesSummary = speciesSummary.map(species => ({
      ...species,
      countIncreased: false,
    }));

    // Clear any pending animation cleanup timers
    animationCleanupTimers.forEach(timer => clearTimeout(timer));
    animationCleanupTimers.clear();
  }

  // Process pending cleanups using requestAnimationFrame
  function processCleanups(currentTime: number) {
    const toExecute: Array<() => void> = [];

    pendingCleanups.forEach((cleanup, key) => {
      if (currentTime >= cleanup.timestamp) {
        toExecute.push(cleanup.fn);
        pendingCleanups.delete(key);
      }
    });

    // Execute cleanups in batch
    toExecute.forEach(fn => fn());

    // Continue if there are more pending cleanups
    if (pendingCleanups.size > 0) {
      animationFrame = window.requestAnimationFrame(processCleanups);
    } else {
      animationFrame = null;
    }
  }

  // Centralized animation cleanup with RAF batching
  function scheduleAnimationCleanup(cleanupFn: () => void, delay: number, key?: string) {
    // Use species code as key if available, otherwise generate one
    const cleanupKey = key || `cleanup-${Date.now()}-${Math.random()}`;

    // Performance: Limit concurrent animations to prevent overwhelming the UI
    if (pendingCleanups.size > 50) {
      logger.warn('Too many concurrent animations, clearing oldest to prevent performance issues');
      const oldestKey = pendingCleanups.keys().next().value;
      if (oldestKey) {
        pendingCleanups.delete(oldestKey);
      }
    }

    // Schedule cleanup
    pendingCleanups.set(cleanupKey, {
      fn: cleanupFn,
      timestamp: window.performance.now() + delay,
    });

    // Start RAF loop if not already running
    if (animationFrame === null) {
      animationFrame = window.requestAnimationFrame(processCleanups);
    }
  }

  // SSE connection for real-time detection updates
  let eventSource: ReconnectingEventSource | null = null;

  // Connect to SSE stream for real-time updates using ReconnectingEventSource
  function connectToDetectionStream() {
    logger.debug('Connecting to SSE stream at /api/v2/merlin/stream');

    // Clean up existing connection
    if (eventSource) {
      eventSource.close();
      eventSource = null;
    }

    try {
      // ReconnectingEventSource with configuration
      eventSource = new ReconnectingEventSource('/api/v2/merlin/stream', {
        max_retry_time: 30000, // Max 30 seconds between reconnection attempts
        withCredentials: false,
      });

      eventSource.onopen = () => {
        logger.debug('SSE connection opened');
      };

      eventSource.onmessage = event => {
        try {
          const data = JSON.parse(event.data);

          // Check if this is a structured message with eventType
          if (data.eventType) {
            switch (data.eventType) {
              case 'connected':
                logger.debug('Connected to detection stream:', data);
                break;

              case 'detection':
                handleSSEDetection(data);
                break;

              case 'heartbeat':
                logger.debug('SSE heartbeat received, clients:', data.clients);
                break;

              default:
                logger.debug('Unknown event type:', data.eventType);
            }
          } else if (data.ID && data.CommonName) {
            // This looks like a direct detection event
            handleSSEDetection(data);
          }
        } catch (error) {
          logger.error('Failed to parse SSE message:', error);
        }
      };

      // Handle specific event types
      // Handle specific event types
      eventSource.addEventListener('connected', (event: Event) => {
        try {
          // eslint-disable-next-line no-undef
          const messageEvent = event as MessageEvent;
          const data = JSON.parse(messageEvent.data);
          logger.debug('Connected event received:', data);
        } catch (error) {
          logger.error('Failed to parse connected event:', error);
        }
      });

      eventSource.addEventListener('merlin', (event: Event) => {
        try {
          // eslint-disable-next-line no-undef
          const messageEvent = event as MessageEvent;
          const data = JSON.parse(messageEvent.data);
          handleSSEDetection(data);
        } catch (error) {
          logger.error('Failed to parse detection event:', error);
        }
      });

      eventSource.addEventListener('heartbeat', (event: Event) => {
        try {
          // eslint-disable-next-line no-undef
          const messageEvent = event as MessageEvent;
          const data = JSON.parse(messageEvent.data);
          logger.debug('Heartbeat event received, clients:', data.clients);
        } catch (error) {
          logger.error('Failed to parse heartbeat event:', error);
        }
      });

      eventSource.onerror = (error: Event) => {
        logger.error('SSE connection error:', error);
        // ReconnectingEventSource handles reconnection automatically
        // No need for manual reconnection logic
      };
    } catch (error) {
      logger.error('Failed to create ReconnectingEventSource:', error);
      // Try again in 5 seconds if initial setup fails
      setTimeout(() => connectToDetectionStream(), 5000);
    }
  }

  // Helper function to process SSE detection data
  function handleSSEDetection(detectionData: unknown) {
    if (!isModelPredictions(detectionData)) {
      const keys =
        typeof detectionData === 'object' && detectionData !== null
          ? Object.keys(detectionData as Record<string, unknown>)
          : [];
      logger.warn('SSE detection payload missing required fields', { keys });
      return;
    }
    try {
      handleNewPrediction(detectionData);
    } catch (error) {
      logger.error('Error processing detection data:', error);
    }
  }
  
  let spectrogramEventSource: ReconnectingEventSource | null = null;
  
  // Connect to SSE stream for real-time updates using ReconnectingEventSource
  function connectToSpectrogramStream() {
    logger.debug('Connecting to SSE stream at /api/v2/spectrogram/stream');

    // Clean up existing connection
    if (spectrogramEventSource) {
      spectrogramEventSource.close();
      spectrogramEventSource = null;
    }

    try {
      // ReconnectingEventSource with configuration
      spectrogramEventSource = new ReconnectingEventSource('/api/v2/spectrogram/stream', {
        max_retry_time: 30000, // Max 30 seconds between reconnection attempts
        withCredentials: false,
      });

      spectrogramEventSource.onopen = () => {
        logger.debug('SSE connection opened for spectrogram');
      };

      spectrogramEventSource.onmessage = event => {
        console.log('foo')
        try {
          const data = JSON.parse(event.data);

          // Check if this is a structured message with eventType
          if (data.eventType) {
            switch (data.eventType) {
              case 'connected':
                logger.debug('Connected to detection stream:', data);
                break;

              case 'ui_spectrogram':
                console.log(data); //todo:mdk
                break;

              case 'heartbeat':
                logger.debug('SSE heartbeat received, clients:', data.clients);
                break;

              default:
                logger.debug('Unknown event type:', data.eventType);
            }
          }
        } catch (error) {
          logger.error('Failed to parse SSE message:', error);
        }
      };

      // Handle specific event types
      // Handle specific event types
      spectrogramEventSource.addEventListener('connected', (event: Event) => {
        try {
          // eslint-disable-next-line no-undef
          const messageEvent = event as MessageEvent;
          const data = JSON.parse(messageEvent.data);
          logger.debug('Connected event received:', data);
        } catch (error) {
          logger.error('Failed to parse connected event:', error);
        }
      });

      spectrogramEventSource.addEventListener('ui_spectrogram', (event: Event) => {
        try {
          // eslint-disable-next-line no-undef
          const messageEvent = event as MessageEvent;
          const data = JSON.parse(messageEvent.data);
          handleSpectrogramData(Uint8Array.fromBase64(data.spectrogram));
        } catch (error) {
          logger.error('Failed to parse detection event:', error);
        }
      });

      spectrogramEventSource.addEventListener('heartbeat', (event: Event) => {
        try {
          // eslint-disable-next-line no-undef
          const messageEvent = event as MessageEvent;
          const data = JSON.parse(messageEvent.data);
          logger.debug('Heartbeat event received, clients:', data.clients);
        } catch (error) {
          logger.error('Failed to parse heartbeat event:', error);
        }
      });

      spectrogramEventSource.onerror = (error: Event) => {
        logger.error('SSE connection error:', error);
        // ReconnectingEventSource handles reconnection automatically
        // No need for manual reconnection logic
      };
    } catch (error) {
      logger.error('Failed to create ReconnectingEventSource:', error);
      // Try again in 5 seconds if initial setup fails
      setTimeout(() => connectToSpectrogramStream(), 5000);
    }
  }
    
  function handleSpectrogramData(bytes: Uint8Array) {
    draw(bytes.slice(0, 257));
    draw(bytes.slice(257, 514));
  }
  
  function draw(freqArray: Uint8Array) {
    requestAnimationFrame(function() {
        dodraw(freqArray);
    });
  }
  
  function dodraw(freqArray: Uint8Array) {
    const canvas = <HTMLCanvasElement> document.getElementById('spectrogram');
    if (canvas === null) {
      console.log("canvas is null");
      return;
    }
    
    const ctx = canvas.getContext('2d');
    if (ctx === null) {
      console.log("ctx is null");
      return;
    }
    
    let n = 3;
    
    // Shift existing content left by 1 pixel
    ctx.drawImage(canvas, -1*n, 0);
    
    // Draw new slice on the right edge
    const col = canvas.width - n;
    for (let i = 0; i < freqArray.length; i++) {
      const value = 255 - freqArray[i];
      ctx.fillStyle = `rgb(${value}, ${value}, ${value})`;
      ctx.fillRect(col, i, n, 1);
    }
  }

  onMount(() => {
    // Setup SSE connection for real-time updates
    connectToDetectionStream();
    connectToSpectrogramStream();

    return () => {
      // Clean up SSE connection
      if (eventSource) {
        eventSource.close();
        eventSource = null;
      }
      
      if (spectrogramEventSource) {
        spectrogramEventSource.close();
        spectrogramEventSource = null;
      }

      // Clean up animation timers
      animationCleanupTimers.forEach(timer => clearTimeout(timer));
      animationCleanupTimers.clear();

      // Cancel pending RAF
      if (animationFrame !== null) {
        window.cancelAnimationFrame(animationFrame);
        animationFrame = null;
      }

      // Clear pending cleanups
      pendingCleanups.clear();
    };
  });

  // Incremental daily summary update when new detection arrives via SSE
  function handleNewPrediction(data: ModelPredictions) {
    newDetectionIds.clear();
    
    let recs = filterAndSortResults(data.predictions);
    for (let i in recs)
    {
      let rec = recs[i];
      if (rec.commonName == "bird sp.")
      {
        continue;
      }
      newDetectionIds.add(rec.commonName);
      handleNewDetection(rec);
    }
  }
  
  
  //
  // ThresholdPrefs
  //
  
  function getBirdSingingThreshold()
  {
    return 0.96
  }
  
  function getInitialThreshold()
  {
    return 0.7; // dev value == 0.5
  }
  
  function getUnlockedBirdThreshold()
  {
    return 0.2;
  }
  
  function getMinDetectionsToUnlock()
  {
    return 2;
  }
  
  
  //
  // ClassificationResultsProcessorImpl
  //
  
  let unlockedSpecies = new Set<string>();
 
  function filterAndSortResults(recs: SoundRecognition[])
  {
    let filtered = filterByThreshold(recs);
    if (!containsBirdSinging(filtered))
    {
      return new Array<SoundRecognition>();
    }
    
    updateHistory(filtered);
    updateUnlockedSpecies(filtered);
    
    let results = new Array<SoundRecognition>();
    
    for (let i in filtered)
    {
      let rec = filtered[i];
      //todo:mdk if (isUnlocked(rec))
      {
        results.push(rec);
      }
    }
    
    //todo:mdk sort by confidence
    return results;
  }
  
  function updateUnlockedSpecies(recs: SoundRecognition[])
  {
    //todo:mdk implement
  }
 
  function unlock(commonName: string, consecutiveDetections: string[])
  {
    //todo:mdk implement
  }
  
  function updateHistory(recs: SoundRecognition[])
  {
    //todo:mdk implement
  }
  
  function containsBirdSinging(recs: SoundRecognition[])
  {
    for (let i in recs)
    {
      let rec = recs[i];
      if (rec.commonName == "bird sp.")
      {
        return true;
      }
    }
    
    return false;
  }
  
  function filterByThreshold(recs: SoundRecognition[])
  {
    let results = new Array<SoundRecognition>();
    
    for (let i in recs)
    {
      let rec = recs[i];
      if (rec.confidence >= getMinConfidence(rec))
      {
        results.push(rec);
      }
    }
    
    return results;
  }
  
  function getMinConfidence(rec: SoundRecognition)
  {
      if (rec.commonName == "bird sp.") {
        return getBirdSingingThreshold();
      }
      
      if (isUnlocked(rec)) {
        return getUnlockedBirdThreshold();
      }
      
      return getInitialThreshold();
  }

  function isUnlocked(rec: SoundRecognition)
  {
    return unlockedSpecies.has(rec.commonName);
  }


  // Incremental daily summary update when new detection arrives via SSE
  function handleNewDetection(detection: SoundRecognition) {

    const existingIndex = speciesSummary.findIndex(s => s.common_name === detection.commonName);

    if (existingIndex >= 0) {
      // Update existing species
      const existing = safeArrayAccess(speciesSummary, existingIndex);
      if (!existing) 
        return;
      const updated = { ...existing };
      updated.count++;
      updated.countIncreased = true;
      updated.maxConfidence = Math.max(updated.maxConfidence, detection.confidence)
      updated.confidence = detection.confidence;

      // Update in place
      speciesSummary = [
        ...speciesSummary.slice(0, existingIndex),
        updated,
        ...speciesSummary.slice(existingIndex + 1),
      ];
      logger.debug(
        `Updated species: ${detection.commonName} (count: ${updated.count})`
      );

      // Clear animation flags after animation completes
      scheduleAnimationCleanup(
        () => {
          const currentIndex = speciesSummary.findIndex(
            s => s.common_name === detection.commonName
          );
          if (currentIndex >= 0) {
            const currentItem = safeArrayAccess(speciesSummary, currentIndex);
            if (!currentItem) 
              return;
            const cleared = { ...currentItem };
            cleared.countIncreased = false;

            speciesSummary = [
              ...speciesSummary.slice(0, currentIndex),
              cleared,
              ...speciesSummary.slice(currentIndex + 1),
            ];
          }
        },
        1000,
        `count-${detection.scientificName}`
      );
    } else {
      // Add new species
      const newSpecies: MerlinSpeciesSummary = {
        common_name: detection.commonName,
        scientific_name: detection.scientificName,
        confidence: detection.confidence,
        maxConfidence: detection.confidence,
        count: 1,
        countIncreased: true,
      };

      // Add to array
      speciesSummary = [newSpecies, ...speciesSummary];

      logger.debug(`Added new species: ${detection.commonName} (count: 1)`);

      // Clear animation flag after animation completes
      scheduleAnimationCleanup(
        () => {
          const currentIndex = speciesSummary.findIndex(
            s => s.common_name === detection.commonName
          );
          if (currentIndex >= 0) {
            const currentItem = safeArrayAccess(speciesSummary, currentIndex);
            if (!currentItem) 
              return;
            const cleared = { ...currentItem };
            cleared.countIncreased = false;

            speciesSummary = [
              ...speciesSummary.slice(0, currentIndex),
              cleared,
              ...speciesSummary.slice(currentIndex + 1),
            ];
          }
        },
        1000,
        `count-${detection.scientificName}`
      );
    }
  }
</script>

<section class="col-span-12">
  <div class="pt-8 card bg-base-100 shadow-sm rounded-2xl border border-border-100 overflow-visible inline-block">
    <div class="overflow-x-auto overflow-y-visible inline-block">
      <canvas id="spectrogram" width="800" height="257" class="mb-4"></canvas>
      <MerlinResultsGrid
        data={speciesSummary}
        {newDetectionIds}  
      />
    </div>
  </div>
</section>
