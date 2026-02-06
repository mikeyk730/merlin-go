<!--
MerlinResultsGrid.svelte - Display detected birds in realtime

Purpose:
- Displays bird species

Props:
- data: MerlinSpeciesSummary[] - Array of species detection summaries
- birdSinging: MerlinSpeciesSummary - Whether the model thinks a bird is present
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
    birdSinging: MerlinSpeciesSummary;
  }

  let {
    data = [],
    birdSinging = {
        common_name: "",
        scientific_name: "",
        confidence: 0,
        maxConfidence: 0,
        count: 0,
      },
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
</script>

<div
  class="merlin-results-grid mb-4 max-w-[800px]"
  style:--species-col-width={speciesColumnWidth}
>
  <!-- Species rows -->
  <div class="flex flex-col" style:gap="var(--grid-gap)">
    {#each data as item (item.common_name)}
      {#key item.count}
        <div class="flex items-center species-row">
          <div class="species-label-col shrink-0 flex items-center gap-4 px-4 py-1">

            <!-- Species thumbnail -->
            <MerlinThumbnail
              thumbnailUrl={`/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`}
              commonName={item.common_name}
              scientificName={item.common_name}
            />

            <!-- Species name -->
            <span class="text-md font-medium leading-tight flex items-center gap-1 overflow-hidden">
              <span class="truncate flex-1" class:highlight={item.count > 0}>{item.common_name}</span>
            </span>

            <!-- Detection confidence -->
            <span class="ml-auto text-md font-medium leading-tight flex items-center gap-1 overflow-hidden">
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
    class="text-center py-8 max-w-[800px]"
    style:color="color-mix(in srgb, var(--color-base-content) 60%, transparent)"
  >
    Listening for birds...
  </div>
{/if}

<style>

  /* ========================================================================
     CSS Custom Properties for results grid
     Scoped to component to avoid global conflicts
     ======================================================================== */

  .merlin-results-grid {
    --grid-cell-radius: 4px;
    --grid-gap: 1px; /* Gap between grid cells */

    /* Species column width fallbacks (actual width is set dynamically via JS)
       These are fallbacks only - the dynamic width is set via style:--species-col-width */
    --species-col-min-width: 9rem; /* Fallback, matches CONFIG.SPECIES_COLUMN.MIN_WIDTH */
    --species-col-max-width: 16rem; /* Fallback, matches CONFIG.SPECIES_COLUMN.MAX_WIDTH */
  }

  /* Species label column - fixed width calculated from longest species name */
  .species-label-col {
    width: var(--species-col-width, var(--species-col-min-width));
  }

  /* Species row - consistent height */
  .species-row {
    border-radius: var(--grid-cell-radius);
  }

  @keyframes rowHighlight {
    0% { background-color: #fff5c2; }
    100% { background-color: transparent; }
  }

  /* Highlight background on update */
  .species-row:has(.highlight) {
    animation: rowHighlight 1.75s ease-out forwards;
  }

  .species-row:hover {
    background-color: var(--hover-overlay);
  }

</style>
