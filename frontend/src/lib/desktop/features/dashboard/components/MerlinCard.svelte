<!--
DailySummaryCard.svelte - Daily bird species detection summary table

Purpose:
- Displays daily bird species summaries with hourly detection counts
- Provides interactive heatmap visualization of detection patterns
- Integrates sun times to highlight sunrise/sunset hours

Features:
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
    error?: string | null;
    showThumbnails?: boolean;
    speciesLimit?: number;
  }

  let {
    data = [],
    error = null,
    showThumbnails = true,
    speciesLimit = 0,
  }: Props = $props();

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
    {t('dashboard.dailySummary.loading.complete')}
</div>

<!-- Progressive loading implementation -->
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
                  class:row-highlight={item.isNew}
                >
                  <!-- Species info column -->
                  <div class="species-label-col shrink-0 flex items-center gap-2 pr-4">
                    <MerlinThumbnail
                      thumbnailUrl={
                        `/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`}
                      commonName={item.common_name}
                      scientificName={item.common_name}
                    />
                  <span
                    class="text-lg hover:text-primary cursor-pointer font-medium leading-tight flex items-center gap-1 overflow-hidden"
                    title={item.common_name}
                  >
                    <span class="truncate flex-1">{item.common_name} ({item.count})</span>
                  </span>
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

<style>

  @keyframes rowHighlight {
    0% { background-color: #f8f07b; }
    100% { background-color: transparent; }
  }

  .row-highlight {
    animation: rowHighlight 1.5s ease-out forwards;
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


  :global(.overflow-y-visible) {
    overflow-y: visible !important;
  }
  
</style>
