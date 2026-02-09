<!--
MerlinResultsGrid.svelte - Display detected birds in realtime

Purpose:
- Displays bird species

Props:
- data: MerlinSpeciesSummary[] - Array of species detection summaries
-->

<script lang="ts">
  import type { MerlinSpeciesSummary } from '$lib/types/detection.types';
  import MerlinThumbnail from './MerlinThumbnail.svelte';

  interface Props {
    data: MerlinSpeciesSummary[];
  }

  let {
    data = [],
  }: Props = $props();
</script>

{#if data.length === 0}
  <div
    class="text-center py-8 max-w-[700px]"
    style:color="color-mix(in srgb, var(--color-base-content) 60%, transparent)"
  >
    Listening for birds...
  </div>
{/if}

<div
  class="merlin-results-grid sm:mb-4 max-w-[700px]"
>
  <!-- Species rows -->
  <div class="flex flex-col" style:gap="var(--grid-gap)">
    {#each data as item (item.common_name)}
      {#key item.count}
        <div class="flex items-center species-row">
          <div class="w-full shrink-0 flex items-center gap-4 px-4 py-1">

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
            <span class="ml-auto max-sm:hidden text-md font-medium leading-tight flex items-center gap-1 overflow-hidden">
              <span class="truncate flex-1">{Math.floor(item.confidence*100)}%</span>
            </span>

          </div>
        </div>
      {/key}
    {/each}
  </div>
</div>

<style>

  /* ========================================================================
     CSS Custom Properties for results grid
     Scoped to component to avoid global conflicts
     ======================================================================== */

  .merlin-results-grid {
    --grid-cell-radius: 4px;
    --grid-gap: 1px; /* Gap between grid cells */
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
