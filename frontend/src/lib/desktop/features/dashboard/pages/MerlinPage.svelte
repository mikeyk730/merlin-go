<!--
DashboardPage.svelte - Main dashboard page with bird detection summaries

Purpose:
- Central dashboard displaying daily species summaries and recent detections
- Manages date persistence with hybrid URL/localStorage approach
- Provides real-time updates via Server-Sent Events (SSE)
- Handles date navigation with smart sticky date selection

Features:
- Sticky date selection (30-minute retention in localStorage)
- URL-based date sharing for bookmarking/sharing specific dates
- Real-time detection updates via SSE with animations
- Adjacent date preloading for smooth navigation
- Browser back/forward button support
- "Today" button resets date persistence to current date
- Dashboard navigation from sidebar resets to current date

Date Persistence Strategy:
- Priority: URL parameter > Recent localStorage (within 30 min) > Current date
- URL parameter allows direct navigation and sharing
- localStorage provides sticky behavior for return visits
- Automatic cleanup after 30-minute retention period
- Reset mechanisms via "Today" button and dashboard navigation

Props: None (Page component)

State Management:
- dailySummary: Array of species detection summaries for the selected date
- Real-time updates tracked via newDetectionIds and hourlyUpdates

Performance Optimizations:
- Adjacent date preloading for instant navigation
- Debounced SSE updates to prevent excessive re-renders
- Efficient animation cleanup with requestAnimationFrame
- Map-based lookups for O(1) species updates
-->
<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import ReconnectingEventSource from 'reconnecting-eventsource';
  import MerlinCard from '$lib/desktop/features/dashboard/components/MerlinCard.svelte';
  import { t } from '$lib/i18n';
  import type { MerlinSpeciesSummary, Detection } from '$lib/types/detection.types';
  import {
    parseHour,
  } from '$lib/utils/date';
  import {
    getInitialDate,
  } from '$lib/utils/datePersistence';
  import { getLogger } from '$lib/utils/logger';
  import { safeArrayAccess, isPlainObject } from '$lib/utils/security';
  import { api } from '$lib/utils/api';
  import { navigation } from '$lib/stores/navigation.svelte';

  const logger = getLogger('app');

  // Constants
  const ANIMATION_CLEANUP_DELAY = 2200; // Slightly longer than 2s animation duration
  const MIN_FETCH_LIMIT = 10; // Minimum number of detections to fetch for SSE processing
  // Species limit buffer constants for SSE updates
  // BUFFER_TRIGGER: When array exceeds limit + this, trigger cleanup
  // BUFFER_TARGET: After cleanup, keep limit + this many species to avoid frequent re-sorting
  const SPECIES_LIMIT_BUFFER_TRIGGER = 10;
  const SPECIES_LIMIT_BUFFER_TARGET = 5;

  type MdkTodo = {
    commonName: string;
    scientificName: string;
    confidence: number;
  };

  // SSE Detection Data Type
  type SSEDetectionData = {
    predictions: MdkTodo[];
    datetime: string; // YYYY-MM-DD
  };

  function isSSEDetectionData(v: unknown): v is SSEDetectionData {
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
    //  typeof o.SpeciesCode === 'string' &&
    //  o.SpeciesCode.length > 0
    //);
  }

  // State management
  let dailySummary = $state<MerlinSpeciesSummary[]>([]);
  let isLoadingSummary = $state(false);
  let isLoadingDetections = $state(true);
  let summaryError = $state<string | null>(null);
  let detectionsError = $state<string | null>(null);
  let showThumbnails = $state(true); // Default to true for backward compatibility
  let summaryLimit = $state(30); // Default from backend (conf/defaults.go) - species count limit for daily summary

  // SSE throttling timer
  let sseFetchTimer: ReturnType<typeof setTimeout> | null = null;

  // Animation state for new detections
  let newDetectionIds = $state(new Set<number>());
  let detectionArrivalTimes = $state(new Map<number, number>());

  // Update freeze tracking to prevent SSE updates during user interactions (menus, audio playback, etc.)
  let freezeCount = $state(0);
  let pendingDetectionQueue = $state<Detection[]>([]);

  // Debouncing for rapid daily summary updates
  let updateQueue = $state(new Map<string, Detection>());
  let updateTimer: ReturnType<typeof setTimeout> | null = null;

  async function fetchDashboardConfig() {
    try {
      interface DashboardConfig {
        thumbnails?: { summary?: boolean };
        summaryLimit?: number;
      }
      const config = await api.get<DashboardConfig>('/api/v2/settings/dashboard');
      // API returns lowercase field names matching Go JSON tags
      showThumbnails = config.thumbnails?.summary ?? true;
      summaryLimit = config.summaryLimit ?? 30;
      logger.debug('Dashboard config loaded:', {
        thumbnails: config.thumbnails,
        showThumbnails,
        summaryLimit,
      });
    } catch (error) {
      logger.error('Error fetching dashboard config:', error);
      // Keep default values on error
    }
  }

  // Manual refresh function that works with both SSE and polling
  function handleManualRefresh() {
    // Clear animation state on manual refresh
    newDetectionIds.clear();
    detectionArrivalTimes.clear();
  }

  // Animation cleanup timers and RAF manager - use $state.raw() for performance
  let animationCleanupTimers = $state.raw(new Set<ReturnType<typeof setTimeout>>());
  let animationFrame: number | null = null;
  let pendingCleanups = $state.raw(new Map<string, { fn: () => void; timestamp: number }>());

  // Clear animation states from daily summary
  function clearDailySummaryAnimations() {
    dailySummary = dailySummary.map(species => ({
      ...species,
      isNew: false,
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

  // Process new detection from SSE - queue if updates are frozen, otherwise process immediately
  function handleNewDetection(detection: Detection) {
    // If any interactions are active (menus, audio playback), queue the detection for later processing
    if (freezeCount > 0) {
      // Avoid duplicate detections in queue - add null-safety check
      const isDuplicate = pendingDetectionQueue.some(
        pending => pending?.id != null && detection?.id != null && pending.id === detection.id
      );
      if (!isDuplicate) {
        pendingDetectionQueue.push(detection);
      }
      return;
    }

    // Process immediately if no interactions are active
    processDetectionUpdate(detection);
  }

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
    if (!isSSEDetectionData(detectionData)) {
      const keys =
        typeof detectionData === 'object' && detectionData !== null
          ? Object.keys(detectionData as Record<string, unknown>)
          : [];
      logger.warn('SSE detection payload missing required fields', { keys });
      return;
    }
    try {
      // Convert SSEDetectionData to Detection format
      for (let i in detectionData.predictions){
        if (detectionData.predictions[i].commonName === 'bird sp.')
        {
          continue;
        }
        const detection: Detection = {
          id: 1,
          commonName: detectionData.predictions[i].commonName,
          scientificName: detectionData.predictions[i].scientificName,
          confidence: detectionData.predictions[i].confidence,
          date: detectionData.datetime,
          time: detectionData.datetime,
          speciesCode: detectionData.predictions[i].commonName,
          verified: 'unverified',
          locked: false,
          source: '',
          beginTime: '',
          endTime: '',
        };

        handleNewDetection(detection);
      }
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
    //console.log(bytes.length);
    
    draw(bytes.slice(0, 257));
    draw(bytes.slice(257, 514));
    //draw(bytes.slice(514, 771));
    //draw(bytes.slice(771, 1028));
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
    //console.log(freqArray.length)
    //console.log(freqArray)
    for (let i = 0; i < freqArray.length; i++) {
      const value = 255 - freqArray[i];
      ctx.fillStyle = `rgb(${value}, ${value}, ${value})`;
      ctx.fillRect(col, i, n, 1);
    }
  }

  onMount(() => {
    fetchDashboardConfig();

    // Setup SSE connection for real-time updates
    connectToDetectionStream();
    connectToSpectrogramStream();

    return () => {
      // Clean up SSE connection
      if (eventSource) {
        eventSource.close();
        eventSource = null;
      }

      // Clean up debounce timer
      if (updateTimer) {
        clearTimeout(updateTimer);
      }

      // Clean up SSE fetch throttling timer
      if (sseFetchTimer) {
        clearTimeout(sseFetchTimer);
        sseFetchTimer = null;
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


  function noop() {
  }

  // Queue daily summary updates with debouncing for rapid updates
  function queueDailySummaryUpdate(detection: Detection) {
    // Performance: Skip if too many pending updates to prevent UI freeze
    if (updateQueue.size > 20) {
      logger.warn('Too many pending daily summary updates, skipping to prevent performance issues');
      return;
    }

    // Add to queue (overwrites previous detection for same species)
    updateQueue.set(detection.speciesCode, detection);

    // Clear existing timer and set new one
    if (updateTimer) {
      clearTimeout(updateTimer);
    }

    updateTimer = setTimeout(() => {
      // Process all queued updates in order of species code for consistency
      const sortedUpdates = Array.from(updateQueue.entries()).sort(([a], [b]) =>
        a.localeCompare(b)
      );

      sortedUpdates.forEach(([_, queuedDetection]) => {
        updateDailySummary(queuedDetection);
      });

      updateQueue.clear();
      updateTimer = null;
    }, 150); // Batch updates within 150ms window
  }

  // Incremental daily summary update when new detection arrives via SSE
  function updateDailySummary(detection: Detection) {

    const existingIndex = dailySummary.findIndex(s => s.common_name === detection.commonName);

    if (existingIndex >= 0) {
      // Update existing species - MerlinCard's sortedData handles reordering
      const existing = safeArrayAccess(dailySummary, existingIndex);
      if (!existing) return;
      const updated = { ...existing };
      updated.previousCount = updated.count;
      updated.count++;
      updated.countIncreased = true;

      // Update in place - sorting is handled by MerlinCard's sortedData derived value
      dailySummary = [
        ...dailySummary.slice(0, existingIndex),
        updated,
        ...dailySummary.slice(existingIndex + 1),
      ];
      logger.debug(
        `Updated species: ${detection.commonName} (count: ${updated.count})`
      );

      // Clear animation flags after animation completes
      scheduleAnimationCleanup(
        () => {
          const currentIndex = dailySummary.findIndex(
            s => s.common_name === detection.commonName
          );
          if (currentIndex >= 0) {
            const currentItem = safeArrayAccess(dailySummary, currentIndex);
            if (!currentItem) return;
            const cleared = { ...currentItem };
            cleared.countIncreased = false;

            dailySummary = [
              ...dailySummary.slice(0, currentIndex),
              cleared,
              ...dailySummary.slice(currentIndex + 1),
            ];
          }
        },
        1000,
        `count-${detection.speciesCode}`
      );
    } else {
      // Add new species - sorting is handled by MerlinCard's sortedData derived value
      const newSpecies: MerlinSpeciesSummary = {
        common_name: detection.commonName,
        scientific_name: detection.scientificName,
        count: 1,
        isNew: true,
      };

      // Add to array - MerlinCard's sortedData will sort by count
      dailySummary = [...dailySummary, newSpecies];

      // Enforce species count limit to prevent grid from growing indefinitely
      // Note: This is a safety limit before sorting; MerlinCard applies final limit after sorting
      if (summaryLimit > 0 && dailySummary.length > summaryLimit + SPECIES_LIMIT_BUFFER_TRIGGER) {
        // Keep a buffer above the limit to allow for proper sorting in MerlinCard
        // Sort by count here to remove truly lowest-count species
        dailySummary = [...dailySummary]
          .sort((a, b) => b.count - a.count)
          .slice(0, summaryLimit + SPECIES_LIMIT_BUFFER_TARGET);
      }

      logger.debug(`Added new species: ${detection.commonName} (count: 1)`);

      // Clear animation flag after animation completes
      scheduleAnimationCleanup(
        () => {
          const currentIndex = dailySummary.findIndex(
            s => s.common_name === detection.commonName
          );
          if (currentIndex >= 0) {
            const currentItem = safeArrayAccess(dailySummary, currentIndex);
            if (!currentItem) return;
            const cleared = { ...currentItem };
            cleared.isNew = false;

            dailySummary = [
              ...dailySummary.slice(0, currentIndex),
              cleared,
              ...dailySummary.slice(currentIndex + 1),
            ];
          }
        },
        800,
        `new-${detection.speciesCode}`
      );
    }
  }


  // Update freeze state management
  function handleFreezeStart() {
    freezeCount++;
  }

  function handleFreezeEnd() {
    freezeCount--;
    // Clamp to prevent negative values due to unmount edge cases
    freezeCount = Math.max(0, freezeCount);

    // Process pending detections when all interactions are complete
    if (freezeCount === 0 && pendingDetectionQueue.length > 0) {
      // Process all pending detections
      pendingDetectionQueue.forEach(detection => {
        processDetectionUpdate(detection);
      });

      // Clear the queue
      pendingDetectionQueue = [];
    }
  }

  // Helper function to process a detection update (extracted from handleNewDetection)
  function processDetectionUpdate(detection: Detection) {

    // Queue daily summary update with debouncing
    queueDailySummaryUpdate(detection);
  }

  // Handle detection click - reserved for future card navigation implementation
  // eslint-disable-next-line no-unused-vars
  function _handleDetectionClick(detection: Detection) {
    // Navigate to detection detail view
    navigation.navigate(`/ui/detections/${detection.id}`);
  }
</script>

<div class="col-span-12">
  <canvas id="spectrogram" width="800" height="257"></canvas>

  <!-- Daily Summary Section -->
  <MerlinCard
    data={dailySummary}
    loading={isLoadingSummary}
    error={summaryError}
    {showThumbnails}
    speciesLimit={summaryLimit}
  />

</div>
