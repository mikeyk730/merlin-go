<!--
mdk:todo:
-add bird is singing indicator
-backend needs to use different thresholds for sound id events
-turn off spectrogram events when not on sound id page
lower priority:
-displayed species should decay over time (if haven't heard in 15, 30, 60 mins?)
-unlocked species should decay over time (more aggressive than above?)
-3x higher res spectrogram
-use merlin thumbnails
-code cleanup

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
  import type { MerlinSpeciesSummary, ModelPredictions, SoundRecognition, SoundIdConfig } from '$lib/types/detection.types';
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
  let birdSinging = $state({
        common_name: "",
        scientific_name: "",
        confidence: 0,
        maxConfidence: 0,
        count: 0,
      });

  let thresholdPrefs : SoundIdConfig = {
    birdsingingthreshold: 1.0,
    initialthreshold: 1.0,
    unlockedthreshold: 1.0,
    mindetectionstounlock: 1000,
  };

  async function fetchSoundIdConfig() {
    try {
      thresholdPrefs = await api.get<SoundIdConfig>('/api/v2/settings/soundid');
      logger.debug('Soundid config loaded:', {
        birdsingingthreshold: thresholdPrefs.birdsingingthreshold,
        initialthreshold: thresholdPrefs.initialthreshold,
        unlockedthreshold: thresholdPrefs.unlockedthreshold,
        mindetectionstounlock: thresholdPrefs.mindetectionstounlock,
      });
    } catch (error) {
      logger.error('Error fetching dashboard config:', error);
      // Keep default values on error
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

  async function startUp() {
    await fetchSoundIdConfig();

    // Setup SSE connection for real-time updates
    connectToDetectionStream();
    connectToSpectrogramStream();
  }

  onMount(() => {
    startUp();

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
    };
  });

  // Incremental daily summary update when new detection arrives via SSE
  function handleNewPrediction(data: ModelPredictions) {
    let recs = filterAndSortResults(data.predictions);
    for (const rec of recs)
    {
      if (rec.commonName == SINGING_BIRD_NAME)
      {
        birdSinging.common_name = rec.commonName;
        birdSinging.scientific_name = rec.scientificName;
        birdSinging.maxConfidence = Math.max(birdSinging.maxConfidence, rec.confidence);
        birdSinging.confidence = rec.confidence;

        if (recs.length == 1)
        {
          birdSinging.count++; //todo: need to clear this after 1 second of idle
        }
        else
        {
          birdSinging.count = 0;
        }

        continue;
      }
      handleNewDetection(rec);
    }
  }


  //
  // ThresholdPrefs
  //

  function getBirdSingingThreshold()
  {
    return thresholdPrefs.birdsingingthreshold;
  }

  function getInitialThreshold()
  {
    return thresholdPrefs.initialthreshold;
  }

  function getUnlockedBirdThreshold()
  {
    return thresholdPrefs.unlockedthreshold;
  }

  function getMinDetectionsToUnlock()
  {
    return thresholdPrefs.mindetectionstounlock;
  }


  //
  // ClassificationResultsProcessorImpl
  //

  let SINGING_BIRD_NAME = "bird sp."
  let unlockedSpecies = new Set<string>([SINGING_BIRD_NAME]);
  let previousDetections = new Array<SoundRecognition>();

  function filterAndSortResults(recs: SoundRecognition[])
  {
    let filteredResults = filterByThreshold(recs);
    if (!containsBirdSinging(filteredResults))
    {
      return new Array<SoundRecognition>();
    }

    let history = updateHistory(filteredResults);
    updateUnlockedSpecies(filteredResults, history);

    let results = new Array<SoundRecognition>();

    for (const rec of filteredResults)
    {
      if (isUnlocked(rec))
      {
        results.push(rec);
      }
    }

    results.sort(function(a, b) {
      return a.confidence - b.confidence;
    });

    return results;
  }

  function updateUnlockedSpecies(recs: SoundRecognition[], history: Map<string, number>)
  {
    for (const rec of recs)
    {
      if (!isUnlocked(rec))
      {
        unlock(rec.commonName, history)
      }
    }
  }

  function unlock(commonName: string, history: Map<string, number>)
  {
    const count = history.get(commonName) || 0;
    if (count >= getMinDetectionsToUnlock()) {
      unlockedSpecies.add(commonName);
    }
  }

  function updateHistory(recs: SoundRecognition[])
  {
    let detectionHistory = new Map<string, number>();

    for (const rec of previousDetections) {
      let name = rec.commonName;
      const currentCount = detectionHistory.get(name) || 0;
      detectionHistory.set(name, currentCount + 1);
    }
    for (const rec of recs) {
      let name = rec.commonName;
      const currentCount = detectionHistory.get(name) || 0;
      detectionHistory.set(name, currentCount + 1);
    }

    previousDetections = recs;

    return detectionHistory;
  }

  function containsBirdSinging(recs: SoundRecognition[])
  {
    for (const rec of recs)
    {
      if (rec.commonName == SINGING_BIRD_NAME)
      {
        return true;
      }
    }

    return false;
  }

  function filterByThreshold(recs: SoundRecognition[])
  {
    let results = new Array<SoundRecognition>();

    for (const rec of recs)
    {
      if (rec.confidence >= getMinConfidence(rec))
      {
        results.push(rec);
      }
    }

    return results;
  }

  function getMinConfidence(rec: SoundRecognition)
  {
      if (rec.commonName == SINGING_BIRD_NAME) {
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
    } else {
      // Add new species
      const newSpecies: MerlinSpeciesSummary = {
        common_name: detection.commonName,
        scientific_name: detection.scientificName,
        confidence: detection.confidence,
        maxConfidence: detection.confidence,
        count: 1,
      };

      // Add to array
      speciesSummary = [newSpecies, ...speciesSummary];

      logger.debug(`Added new species: ${detection.commonName} (count: 1)`);
    }
  }
</script>

<section class="col-span-12">
  <div class="pt-8 card bg-base-100 shadow-sm rounded-2xl border border-border-100 overflow-visible inline-block">
    <div class="overflow-x-auto overflow-y-visible inline-block">
      <canvas id="spectrogram" width="800" height="257" class="mb-4"></canvas>
      <div id="singingBirdIndicator" class="flex flex-col">
        {#key birdSinging.count}
          <span class="btext-xs ml-auto flex items-center">
            <span class="bird-indicator-text" class:bird-singing={birdSinging.count > 3}>Hearing a bird</span>
            <span class="bird-indicator" class:bird-singing={birdSinging.count > 0}>&#x25CF;</span>
          </span>
        {/key}
      </div>
      <MerlinResultsGrid
        data={speciesSummary}
      />
    </div>
  </div>
</section>

<style>
  .bird-indicator, .bird-indicator-text
  {
    visibility: hidden;
  }

  .bird-indicator.bird-singing
  {
    visibility: visible;
    animation: singingIndicatorAnimation 1.75s ease-out forwards;
  }
  
  .bird-indicator-text.bird-singing
  {
    visibility: visible;
    text-transform: uppercase;
    animation: singingTextAnimation 1.75s ease-out forwards;
  }  

  @keyframes singingIndicatorAnimation {
    0% {
      color: #6fa8e9;
    }
    50% {
      color: #2b73cc
    }
    99% {
      color: #6fa8e9;
    }
    to {
      color: transparent;
    }
  }

  @keyframes singingTextAnimation {
    0% {
      color: #2b73cc
    }
    99% {
      color: #2b73cc
    }
    to {
      color: transparent;
    }
  }  

</style>