<!--
DailySummaryCard.svelte - Daily bird species detection summary table

Purpose:
- Displays daily bird species summaries with hourly detection counts
- Provides interactive heatmap visualization of detection patterns
- Integrates sun times to highlight sunrise/sunset hours

Features:
- Progressive loading states (skeleton → spinner → loaded/error)
- Responsive hourly/bi-hourly/six-hourly column grouping based on viewport
- Color-coded heatmap cells showing detection intensity
- Daylight visualization row showing sunrise/sunset times
- Species badges with colored initials (GitHub-style heatmap design)
- Real-time animation for new species and count increases
- URL memoization with LRU cache for performance optimization
- Heatmap legend showing intensity scale (Less → More)
- Clickable cells linking to detailed detection views

Props:
- data: MerlinSpeciesSummary[] - Array of species detection summaries
- loading?: boolean - Loading state indicator (default: false)
- error?: string | null - Error message to display (default: null)
- showThumbnails?: boolean - Show thumbnails or colored badge placeholders (default: true)

Performance Optimizations:
- $state.raw() for static data structures (caches, render functions)
- $derived.by() for complex reactive calculations
- LRU cache for URL memoization (500 entries max)
- Optimized animation cleanup with requestAnimationFrame
- Efficient data sorting and max count calculations

Responsive Breakpoints:
- Desktop (≥1400px): All hourly columns visible
- Large (1200-1399px): All hourly columns visible
- Medium (1024-1199px): All hourly columns visible
- Tablet (768-1023px): Bi-hourly columns only
- Mobile (480-767px): Bi-hourly columns only
- Small (<480px): Six-hourly columns only
-->

<script lang="ts">
  import SkeletonDailySummary from '$lib/desktop/components/ui/SkeletonDailySummary.svelte';
  import { t } from '$lib/i18n';
  import type { MerlinSpeciesSummary } from '$lib/types/detection.types';
  import { loggers } from '$lib/utils/logger';
  import { LRUCache } from '$lib/utils/LRUCache';
  import { safeArrayAccess, safeGet } from '$lib/utils/security';
  import {
    convertTemperature,
    getTemperatureSymbol,
    type TemperatureUnit,
  } from '$lib/utils/formatters';
  import { ChevronLeft, ChevronRight, Star, Sunrise, Sunset, XCircle } from '@lucide/svelte';
  import { untrack } from 'svelte';
  import AnimatedCounter from './AnimatedCounter.svelte';
  import MerlinThumbnail from './MerlinThumbnail.svelte';

  const logger = loggers.ui;

  // Progressive loading timing constants (optimized for Svelte 5)
  const LOADING_PHASES = $state.raw({
    skeleton: 0, // 0ms - show skeleton immediately to reserve space
    spinner: 650, // 650ms - show spinner if still loading
  });

  // Heatmap scaling configuration
  // MAX_HEAT_COUNT: detection count at which maximum intensity (9) is reached
  // INTENSITY_LEVELS: number of color intensity levels (1-9, plus 0 for empty)
  const HEATMAP_CONFIG = {
    MAX_HEAT_COUNT: 50,
    INTENSITY_LEVELS: 9,
  } as const;

  // Consolidated configuration for magic numbers
  const CONFIG = {
    CACHE: {
      SUN_TIMES_MAX_ENTRIES: 30, // Max days of sun times to cache
      URL_MAX_ENTRIES: 500, // Max URLs to cache for memoization
    },
    DAYLIGHT: {
      DAWN_DUSK_HOURS_OFFSET: 2, // Hours before sunrise / after sunset for pre-dawn/dusk
      MIDDAY_INTENSITY_THRESHOLD: 0.3, // Distance from midday for "mid-day" classification
      DAY_INTENSITY_THRESHOLD: 0.7, // Distance from midday for "day" classification
      DEEP_NIGHT_END: 4, // Hour when deep night ends (0-4)
      DEEP_NIGHT_START: 21, // Hour when deep night starts (21-23)
      NIGHT_MORNING: 5, // Morning twilight hour
      NIGHT_EVENING: 20, // Evening twilight hour
    },
    QUERY: {
      DEFAULT_NUM_RESULTS: 25, // Default number of results for detection queries
    },
    SKELETON: {
      SPECIES_COUNT: 8, // Number of skeleton rows to show during loading
    },
    SPECIES_COLUMN: {
      BASE_WIDTH: 4, // rem - thumbnail (2) + gap (0.5) + padding (1) + buffer (0.5)
      CHAR_WIDTH: 0.52, // rem per character for text-sm font
      MIN_WIDTH: 50, // rem - minimum column width
      MAX_WIDTH: 50, // rem - maximum column width (prevents excessive width)
    },
  } as const;

  // Column type definitions
  interface BaseColumn {
    key: string;
    header?: string;
    className?: string;
    align?: string;
  }

  interface SpeciesColumn extends BaseColumn {
    type: 'species';
    sortable: boolean;
  }

  interface HourlyColumn extends BaseColumn {
    type: 'hourly';
    hour: number;
    align: string;
  }

  type ColumnDefinition = SpeciesColumn | HourlyColumn;

  interface Props {
    data: MerlinSpeciesSummary[];
    loading?: boolean;
    error?: string | null;
    showThumbnails?: boolean;
    speciesLimit?: number;
  }

  let {
    data = [],
    loading = false,
    error = null,
    showThumbnails = true,
    speciesLimit = 0,
  }: Props = $props();

  // Progressive loading state management
  let loadingPhase = $state<'skeleton' | 'spinner' | 'loaded' | 'error'>('skeleton');
  let showDelayedIndicator = $state(false);

  // Optimize loading state management with proper dependency tracking
  $effect(() => {
    if (loading) {
      loadingPhase = 'skeleton'; // Show skeleton immediately to reserve space
      showDelayedIndicator = false;

      // Use untrack to prevent the timer from becoming a reactive dependency
      const spinnerTimer = setTimeout(() => {
        if (untrack(() => loading)) {
          loadingPhase = 'spinner';
          showDelayedIndicator = true;
        }
      }, LOADING_PHASES.spinner);

      return () => {
        clearTimeout(spinnerTimer);
      };
    } else {
      loadingPhase = error ? 'error' : 'loaded';
      showDelayedIndicator = false;
    }
  });


  /**
   * Calculate heatmap intensity using simple fixed-range scaling.
   * Maps detection counts evenly across intensity levels 1-9 based on HEATMAP_CONFIG.
   * - 0 detections → intensity 0 (empty cell)
   * - 1-6 detections → intensity 1
   * - 7-12 detections → intensity 2
   * - ...
   * - 45-50 detections → intensity 9
   * - 50+ detections → intensity 9
   *
   * @param count - The detection count for this cell
   * @returns Intensity value from 0-9
   */
  const getHeatmapIntensity = (count: number): number => {
    if (count <= 0) return 0;
    const { MAX_HEAT_COUNT, INTENSITY_LEVELS } = HEATMAP_CONFIG;
    const stepSize = MAX_HEAT_COUNT / INTENSITY_LEVELS;
    return Math.min(INTENSITY_LEVELS, Math.max(1, Math.ceil(count / stepSize)));
  };

  // Static column metadata - use $state.raw() for performance (no deep reactivity needed)
  const staticColumnDefs = $state.raw<ColumnDefinition[]>([
    {
      key: 'common_name',
      type: 'species' as const,
      sortable: true,
      className: 'font-medium whitespace-nowrap species-column',
    },
    // Progress bar column removed to save horizontal space - see mockup design
    ...Array.from({ length: 24 }, (_, hour) => ({
      key: `hour_${hour}`,
      type: 'hourly' as const,
      hour,
      header: hour.toString().padStart(2, '0'),
      align: 'center',
      className: 'hour-data hourly-count px-0',
    }))
  ]);

  // Reactive columns with only dynamic headers - use $derived.by for complex logic
  const columns = $derived.by((): ColumnDefinition[] => {
    // Early return for empty data to prevent unnecessary calculations
    if (staticColumnDefs.length === 0) return [];

    return staticColumnDefs.map(colDef => ({
      ...colDef,
      header:
        colDef.type === 'species' ? t('dashboard.dailySummary.columns.species') : colDef.header,
    }));
  });

  // Track and log unexpected column types once (performance optimization)
  const loggedUnexpectedColumns = new Set<string>();
  $effect(() => {
    if (import.meta.env.DEV) {
      const expectedTypes = new Set(['species', 'hourly']);

      columns.forEach(column => {
        if (!expectedTypes.has(column.type) && !loggedUnexpectedColumns.has(column.key)) {
          logger.warn('Unexpected column type detected', null, {
            columnKey: column.key,
            columnType: column.type,
            component: 'DailySummaryCard',
            action: 'columnValidation',
          });
          loggedUnexpectedColumns.add(column.key);
        }
      });
    }
  });

  // Pre-computed render functions - use $state.raw for performance (static functions)
  const renderFunctions = $state.raw({
    hourly: (item: MerlinSpeciesSummary, hour: number) =>
      item.count
  });

  // Calculate dynamic species column width based on longest name
  // This ensures all rows align properly regardless of name length
  // Uses CONFIG.SPECIES_COLUMN constants for easy adjustment
  const speciesColumnWidth = $derived.by(() => {
    const { BASE_WIDTH, CHAR_WIDTH, MIN_WIDTH, MAX_WIDTH } = CONFIG.SPECIES_COLUMN;

    if (data.length === 0) return `${MIN_WIDTH}rem`;

    // Find the longest species name
    const longestName = data.reduce(
      (longest, item) => (item.common_name.length > longest.length ? item.common_name : longest),
      ''
    );
    const maxLength = longestName.length;

    // Calculate width: base (thumbnail + gap + icons) + character width estimate
    const calculatedWidth = BASE_WIDTH + maxLength * CHAR_WIDTH;

    // Clamp between min and max
    const finalWidth = Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, calculatedWidth));

    return `${finalWidth}rem`;
  });

  // Track which species have been highlighted recently (for restart detection)
  const highlightedSpecies = $state.raw(new Map<string, number>());
  
  // Track previous counts to detect changes
  const previousCounts = $state.raw(new Map<string, number>());
  
  // Track timeouts per species so we can cancel them on re-highlight
  const highlightTimeouts = $state.raw(new Map<string, ReturnType<typeof setTimeout>>());

  // Add effect to track count changes
  $effect(() => {
    data.forEach(item => {
      const prevCount = previousCounts.get(item.common_name);
      
      if (prevCount !== item.count) {
        // Clear any existing timeout for this species
        const existingTimeout = highlightTimeouts.get(item.common_name);
        if (existingTimeout !== undefined) {
          clearTimeout(existingTimeout);
        }
        
        const now = Date.now();
        highlightedSpecies.set(item.common_name, now);
        
        // Set new timeout and store it
        const timeout = setTimeout(() => {
          highlightedSpecies.delete(item.common_name);
          highlightTimeouts.delete(item.common_name);
        }, 3000);
        
        highlightTimeouts.set(item.common_name, timeout);
      }
      
      // Update previous count for next comparison
      previousCounts.set(item.common_name, item.count);
    });
  });
</script>


<!-- Live region for screen reader announcements of loading state changes -->
<div class="sr-only" role="status" aria-live="polite" aria-atomic="true">
  {#if loadingPhase === 'skeleton'}
    {t('dashboard.dailySummary.loading.preparing')}
  {:else if loadingPhase === 'spinner'}
    {t('dashboard.dailySummary.loading.fetching')}
  {:else if loadingPhase === 'error'}
    {t('dashboard.dailySummary.loading.error')}
  {:else if loadingPhase === 'loaded'}
    {t('dashboard.dailySummary.loading.complete')}
  {/if}
</div>

<!-- Progressive loading implementation -->
{#if loadingPhase === 'skeleton'}
  <SkeletonDailySummary {showThumbnails} speciesCount={CONFIG.SKELETON.SPECIES_COUNT} />
{:else if loadingPhase === 'spinner'}
  <SkeletonDailySummary
    {showThumbnails}
    showSpinner={showDelayedIndicator}
    speciesCount={CONFIG.SKELETON.SPECIES_COUNT}
  />
{:else if loadingPhase === 'error'}
  <section
    class="daily-summary-card card col-span-12 bg-base-100 shadow-sm rounded-2xl border border-border-100 overflow-visible"
  >
    <div class="p-6">
      <div class="alert alert-error">
        <XCircle class="size-6" />
        <span>{error}</span>
      </div>
    </div>
  </section>
{:else if loadingPhase === 'loaded'}
  <section
    class="daily-summary-card card col-span-12 bg-base-100 shadow-sm rounded-2xl border border-border-100 overflow-visible"
  >
    <!-- Grid Content -->
    <div class="p-6 pt-8">
      <div class="overflow-x-auto overflow-y-visible">
        <div
          class="daily-summary-grid min-w-[900px]"
          style:--species-col-width={speciesColumnWidth}
        >
          <!-- Species rows -->
          <div class="flex flex-col" style:gap="var(--grid-gap)">
            {#each data as item (item.common_name)}
              {#key highlightedSpecies.get(item.common_name)}
                <div
                  class="flex items-center species-row"
                  class:row-highlight={highlightedSpecies.has(item.common_name)}
                >
                  <!-- Species info column -->
                  <div class="species-label-col shrink-0 flex items-center gap-2 pr-4">
                    <MerlinThumbnail
                      thumbnailUrl={
                        `/api/v2/media/species-image?name=${encodeURIComponent(item.common_name)}`}
                      commonName={item.common_name}
                      scientificName={item.common_name}
                    />
                  <a
                    class="text-lg hover:text-primary cursor-pointer font-medium leading-tight flex items-center gap-1 overflow-hidden"
                    title={item.common_name}
                  >
                    <span class="truncate flex-1">{item.common_name}</span>
                  </a>
                </div>

                <div class="hourly-grid flex-1 grid">
                    <div
                      class="heatmap-cell h-8 rounded-sm heatmap-color-{getHeatmapIntensity(item.count)} flex items-center justify-center text-xs font-medium"
                    >
                      {#if item.count > 0}
                        <a
                          class="w-full h-full flex items-center justify-center cursor-pointer hover:opacity-80"
                        >
                          <AnimatedCounter value={item.count} />
                        </a>
                      {/if}
                    </div>
                </div>
              </div>
              {/key}
            {/each}
          </div>
        </div>

        {#if data.length === 0}
          <div
            class="text-center py-8"
            style:color="color-mix(in srgb, var(--color-base-content) 60%, transparent)"
          >
            Listening for birds...
          </div>
        {/if}
      </div>
    </div>
  </section>
{/if}

<style>

  @keyframes rowHighlight {
    0% { background-color: #f8f07b; }
    100% { background-color: transparent; }
  }

  .row-highlight {
    animation: rowHighlight 3s ease-out forwards;
  }
  
  /* ========================================================================
     CSS Custom Properties for Daily Summary Grid
     Scoped to component to avoid global conflicts
     ======================================================================== */
  .daily-summary-card {
    /* todo:mdk Grid layout properties */
    --grid-cell-height: 3.0rem;
    --grid-cell-radius: 4px;
    --grid-gap: 4px; /* Gap between grid cells */

    /* Species column width fallbacks (actual width is set dynamically via JS)
       These are fallbacks only - the dynamic width is set via style:--species-col-width */
    --species-col-min-width: 9rem; /* Fallback, matches CONFIG.SPECIES_COLUMN.MIN_WIDTH */
    --species-col-max-width: 16rem; /* Fallback, matches CONFIG.SPECIES_COLUMN.MAX_WIDTH */

    /* Light theme heatmap colors */
    --heatmap-color-0: #f0f9fc;
    --heatmap-color-1: #e0f3f8;
    --heatmap-color-2: #ccebf6;
    --heatmap-color-3: #99d7ed;
    --heatmap-color-4: #66c2e4;
    --heatmap-color-5: #33ade1;
    --heatmap-color-6: #0099d8;
    --heatmap-color-7: #0077be;
    --heatmap-color-8: #005595;
    --heatmap-color-9: #036;

    /* Animation durations */
    --anim-count-pop: 600ms;
    --anim-heart-pulse: 1000ms;
    --anim-new-species: 800ms;
  }

  /* ========================================================================
     CSS Grid Layout Styles
     ======================================================================== */

  /* Species label column - fixed width calculated from longest species name */
  .species-label-col {
    width: var(--species-col-width, var(--species-col-min-width));
  }

  /* CSS Grid for hour columns - equal columns using minmax(0, 1fr) */
  .hourly-grid {
    display: grid;
    grid-template-columns: repeat(24, minmax(0, 1fr));
    gap: var(--grid-gap);
  }

  /* Heatmap cell base styles */
  .heatmap-cell {
    transition:
      opacity 0.15s ease,
      transform 0.15s ease;
  }

  .heatmap-cell a {
    color: inherit;
    text-decoration: none;
  }

  /* Species row - consistent height */
  .species-row {
    min-height: 5rem;
    border-radius: var(--grid-cell-radius);
    transition: background-color 0.15s ease;
  }

  .species-row:hover {
    background-color: var(--hover-overlay);
  }

  /* Empty cells background */
  :global(.heatmap-color-0) {
    background-color: var(--color-base-300);
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light'] .heatmap-color-0) {
    background-color: #e2e8f0;
  }

  :global([data-theme='dark'] .heatmap-color-0) {
    background-color: #1e293b;
  }

  /* ========================================================================
     Responsive Grid Display
     ======================================================================== */

  /* Tablet (768-1023px): show bi-hourly */
  @media (min-width: 768px) and (max-width: 1023px) {
    .hourly-grid {
      display: grid;
    }
  }

  /* Mobile (<768px): show bi-hourly */
  @media (max-width: 767px) {
    .hourly-grid {
      display: grid;
    }
  }

  /* Small mobile (<480px): show six-hourly */
  @media (max-width: 479px) {
    .hourly-grid {
      display: grid;
    }
  }

  /* ========================================================================
     Heatmap Colors
     ======================================================================== */

  /* Dark theme heatmap colors - more vibrant and saturated */
  /* Must use .daily-summary-card scope to override the light theme vars defined above */
  :global([data-theme='dark']) .daily-summary-card {
    --heatmap-color-0: #1e293b;
    --heatmap-color-1: #164e63;
    --heatmap-color-2: #0e7490;
    --heatmap-color-3: #0891b2;
    --heatmap-color-4: #06b6d4;
    --heatmap-color-5: #22d3ee;
    --heatmap-color-6: #38bdf8;
    --heatmap-color-7: #60a5fa;
    --heatmap-color-8: #93c5fd;
    --heatmap-color-9: #bfdbfe;
    --heatmap-text-1: #fff;
    --heatmap-text-2: #fff;
    --heatmap-text-3: #fff;
    --heatmap-text-4: #000;
    --heatmap-text-5: #000;
    --heatmap-text-6: #000;
    --heatmap-text-7: #000;
    --heatmap-text-8: #000;
    --heatmap-text-9: #000;
  }

  /* Heatmap cell styles - solid colors with rounded corners */
  :global(.heatmap-color-1),
  :global(.heatmap-color-2),
  :global(.heatmap-color-3),
  :global(.heatmap-color-4),
  :global(.heatmap-color-5),
  :global(.heatmap-color-6),
  :global(.heatmap-color-7),
  :global(.heatmap-color-8),
  :global(.heatmap-color-9) {
    border-radius: var(--grid-cell-radius);
  }

  :global(.heatmap-color-1) {
    background-color: var(--heatmap-color-1);
    color: var(--heatmap-text-1, #000);
  }

  :global(.heatmap-color-2) {
    background-color: var(--heatmap-color-2);
    color: var(--heatmap-text-2, #000);
  }

  :global(.heatmap-color-3) {
    background-color: var(--heatmap-color-3);
    color: var(--heatmap-text-3, #000);
  }

  :global(.heatmap-color-4) {
    background-color: var(--heatmap-color-4);
    color: var(--heatmap-text-4, #000);
  }

  :global(.heatmap-color-5) {
    background-color: var(--heatmap-color-5);
    color: var(--heatmap-text-5, #fff);
  }

  :global(.heatmap-color-6) {
    background-color: var(--heatmap-color-6);
    color: var(--heatmap-text-6, #fff);
  }

  :global(.heatmap-color-7) {
    background-color: var(--heatmap-color-7);
    color: var(--heatmap-text-7, #fff);
  }

  :global(.heatmap-color-8) {
    background-color: var(--heatmap-color-8);
    color: var(--heatmap-text-8, #fff);
  }

  :global(.heatmap-color-9) {
    background-color: var(--heatmap-color-9);
    color: var(--heatmap-text-9, #fff);
  }

  /* Dark theme text color overrides */
  :global([data-theme='dark'] .heatmap-color-1),
  :global([data-theme='dark'] .heatmap-color-2),
  :global([data-theme='dark'] .heatmap-color-3) {
    color: #fff;
  }

  :global([data-theme='dark'] .heatmap-color-4),
  :global([data-theme='dark'] .heatmap-color-5),
  :global([data-theme='dark'] .heatmap-color-6),
  :global([data-theme='dark'] .heatmap-color-7),
  :global([data-theme='dark'] .heatmap-color-8),
  :global([data-theme='dark'] .heatmap-color-9) {
    color: #000;
  }

  /* Dynamic Update Animations - not in custom.css */

  /* Count increment animation */
  @keyframes countPop {
    0% {
      transform: scale(1);
    }

    50% {
      transform: scale(1.3);
      background-color: oklch(var(--su) / 0.3);
      box-shadow: 0 0 10px oklch(var(--su) / 0.5);
    }

    100% {
      transform: scale(1);
      background-color: transparent;
    }
  }

  .count-increased {
    animation: countPop var(--anim-count-pop) cubic-bezier(0.4, 0, 0.2, 1);
  }

  /* New species row animation */
  @keyframes newSpeciesSlide {
    0% {
      transform: translateY(-30px);
      opacity: 0;
      background-color: oklch(var(--p) / 0.15);
    }

    100% {
      transform: translateY(0);
      opacity: 1;
      background-color: transparent;
    }
  }

  .new-species {
    animation: newSpeciesSlide var(--anim-new-species) cubic-bezier(0.25, 0.46, 0.45, 0.94);
  }

  /* Heatmap cell heart pulse animation */
  @keyframes heartPulse {
    0% {
      transform: scale(1);
      box-shadow: 0 0 0 0 oklch(var(--p) / 0.7);
    }

    15% {
      transform: scale(1.15);
      box-shadow: 0 0 0 4px oklch(var(--p) / 0.5);
    }

    25% {
      transform: scale(1.05);
      box-shadow: 0 0 0 6px oklch(var(--p) / 0.3);
    }

    35% {
      transform: scale(1.12);
      box-shadow: 0 0 0 8px oklch(var(--p) / 0.1);
    }

    45% {
      transform: scale(1);
      box-shadow: 0 0 0 10px oklch(var(--p) / 0);
    }

    100% {
      transform: scale(1);
      box-shadow: 0 0 0 0 oklch(var(--p) / 0);
    }
  }

  .hour-updated {
    animation: heartPulse var(--anim-heart-pulse) ease-out;
    position: relative;
    z-index: 10;
  }

  /* ========================================================================
     Species Column & Badge Styles
     ======================================================================== */

  :global(.species-column) {
    width: auto;
    min-width: 0;
    max-width: var(--species-col-max-width, 18rem);
    padding: 0 0.75rem 0 0.5rem !important;
  }


  /* ========================================================================
     Daylight Row Styles
     ======================================================================== */

  .daylight-cell {
    position: relative;
    transition: background-color 0.2s ease;
    overflow: visible;
  }

  :global(.overflow-y-visible) {
    overflow-y: visible !important;
  }

  /* ========================================================================
     Daylight Color Classes - Gradual shading from night to day
     ======================================================================== */

  /* Deep night (midnight - 4am, 9pm - midnight) - darkest indigo */
  .daylight-deep-night {
    background-color: rgb(30 27 75 / 0.5); /* indigo-950/50 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-deep-night {
    background-color: rgb(30 27 75 / 0.3); /* indigo-950/30 */
  }

  /* Night (5am, 8pm) - lighter indigo */
  .daylight-night {
    background-color: rgb(49 46 129 / 0.4); /* indigo-900/40 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-night {
    background-color: rgb(49 46 129 / 0.2); /* indigo-900/20 */
  }

  /* Evening (6-7pm) - transition indigo */
  .daylight-evening {
    background-color: rgb(67 56 202 / 0.3); /* indigo-700/30 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-evening {
    background-color: rgb(67 56 202 / 0.15); /* indigo-700/15 */
  }

  /* Pre-dawn (1-2 hours before sunrise) - transitional purple/indigo */
  .daylight-pre-dawn {
    background-color: rgb(99 102 241 / 0.3); /* indigo-500/30 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-pre-dawn {
    background-color: rgb(99 102 241 / 0.2); /* indigo-500/20 */
  }

  /* Sunrise - gradient from orange to amber */
  .daylight-sunrise {
    background: linear-gradient(to right, #fb923c, #fbbf24); /* orange-400 to amber-400 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-sunrise {
    background: linear-gradient(to right, #f97316, #fcd34d); /* orange-500 to amber-300 */
  }

  /* Early day (just after sunrise) - soft warm amber */
  .daylight-early-day {
    background-color: rgb(251 191 36 / 0.4); /* amber-400/40 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-early-day {
    background-color: rgb(252 211 77 / 0.6); /* amber-300/60 */
  }

  /* Day (mid-morning, mid-afternoon) - medium amber */
  .daylight-day {
    background-color: rgb(251 191 36 / 0.5); /* amber-400/50 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-day {
    background-color: rgb(252 211 77 / 0.7); /* amber-300/70 */
  }

  /* Mid-day (peak daylight) - brightest amber/yellow */
  .daylight-mid-day {
    background-color: rgb(253 224 71 / 0.6); /* yellow-300/60 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-mid-day {
    background-color: rgb(254 240 138 / 0.8); /* yellow-200/80 */
  }

  /* Late day (before sunset) - soft warm amber */
  .daylight-late-day {
    background-color: rgb(251 191 36 / 0.4); /* amber-400/40 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-late-day {
    background-color: rgb(252 211 77 / 0.6); /* amber-300/60 */
  }

  /* Sunset - gradient from rose to purple */
  .daylight-sunset {
    background: linear-gradient(to right, #fda4af, #c084fc); /* rose-300 to purple-400 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-sunset {
    background: linear-gradient(to right, #fb7185, #a855f7); /* rose-400 to purple-500 */
  }

  /* Dusk (1-2 hours after sunset) - transitional purple */
  .daylight-dusk {
    background-color: rgb(139 92 246 / 0.25); /* violet-500/25 */
    border-radius: var(--grid-cell-radius);
  }

  :global([data-theme='light']) .daylight-dusk {
    background-color: rgb(139 92 246 / 0.15); /* violet-500/15 */
  }
</style>
