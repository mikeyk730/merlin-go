<!--
MerlinCard.svelte - Daily bird species detection summary table

Purpose:
- Displays daily bird species summaries with hourly detection counts
- Integrates sun times to highlight sunrise/sunset hours

Props:
- data: MerlinSpeciesSummary[] - Array of species detection summaries
-->

<script lang="ts">
  import type { MerlinSpeciesSummary } from '$lib/types/detection.types';
  import { safeArrayAccess, safeGet } from '$lib/utils/security';
  import MerlinThumbnail from './MerlinThumbnail.svelte';

  // Consolidated configuration for magic numbers
  const CONFIG = {
    SPECIES_COLUMN: {
      BASE_WIDTH: 4, // rem - thumbnail (2) + gap (0.5) + padding (1) + buffer (0.5)
      CHAR_WIDTH: 0.52, // rem per character for text-sm font
      MIN_WIDTH: 50, // rem - minimum column width
      MAX_WIDTH: 50, // rem - maximum column width (prevents excessive width)
    },
  } as const;

  interface Props {
    data: MerlinSpeciesSummary[];
    newDetectionIds?: Set<string>;
  }

  let {
    data = [],
    newDetectionIds = new Set(),
  }: Props = $props();


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

    <!-- Grid Content -->
        <div
          class="merlin-results-grid max-w-[800px]"
          style:--species-col-width={speciesColumnWidth}
        >
          <!-- Species rows -->
          <div class="flex flex-col" style:gap="var(--grid-gap)">
            {#each data as item (item.common_name)}
              {#key highlightedSpecies.get(item.common_name)}
                <div
                  class="flex items-center species-row mb-1"
                  class:row-highlight={item.countIncreased}
                >
                  <!-- Species info column -->
                  <div class="species-label-col shrink-0 flex items-center gap-4 pr-4">
                    <MerlinThumbnail
                      thumbnailUrl={
                        `/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`}
                      commonName={item.common_name}
                      scientificName={item.common_name}
                    />
                  <span
                    class="text-md font-medium leading-tight flex items-center gap-1 overflow-hidden"
                    title={item.common_name}
                  >
                    <span class="truncate flex-1">{item.common_name}</span>
                  </span>
                  <span
                    class="ml-auto text-md font-medium leading-tight flex items-center gap-1 overflow-hidden"
                    title={item.common_name}
                  >
                    <span class="truncate flex-1">{Math.floor(item.confidence*100)}%</span>
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

<style>

  @keyframes rowHighlight {
    0% { background-color: #fff5c2; }
    100% { background-color: transparent; }
  }

  .row-highlight {
    animation: rowHighlight 1.75s ease-out forwards;
  }
  
  /* ========================================================================
     CSS Custom Properties for Daily Summary Grid
     Scoped to component to avoid global conflicts
     ======================================================================== */
  .merlin-results-grid {
    --grid-cell-radius: 4px;
    --grid-gap: 4px; /* Gap between grid cells */

    /* Species column width fallbacks (actual width is set dynamically via JS)
       These are fallbacks only - the dynamic width is set via style:--species-col-width */
    --species-col-min-width: 9rem; /* Fallback, matches CONFIG.SPECIES_COLUMN.MIN_WIDTH */
    --species-col-max-width: 16rem; /* Fallback, matches CONFIG.SPECIES_COLUMN.MAX_WIDTH */
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
    border-radius: var(--grid-cell-radius);
  }

  .species-row:hover {
    background-color: var(--hover-overlay);
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
