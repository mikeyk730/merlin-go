<!--
SoundIdResultsGrid.svelte - Display detected birds in realtime

Purpose:
- Displays bird species

Props:
- data: SoundIdSummary[] - Array of species detection summaries
-->

<script lang="ts">
  import type { SoundIdSummary } from '$lib/types/detection.types';
  import BirdThumbnailPopup from './BirdThumbnailPopup.svelte';

  interface Props {
    data: SoundIdSummary[];
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
  class="sound-id-results-grid sm:mb-4 max-w-[700px]"
>
  <!-- Species rows -->
  <div class="flex flex-col" style:gap="var(--grid-gap)">
    {#each data as item (item.common_name)}
      {#key item.count}
        <div class="flex items-center species-row">
          <div class="w-full shrink-0 flex items-center gap-4 px-4 py-1">

            <!-- Species thumbnail -->
            <BirdThumbnailPopup
              thumbnailUrl={`/api/v2/media/species-image?name=${encodeURIComponent(item.scientific_name)}`}
              commonName={item.common_name}
              scientificName={item.scientific_name}
              largeThumbnails={true}
            />

            <!-- Species name -->
            <span class="text-md font-medium leading-tight flex items-center gap-1 overflow-hidden">
              <span class="truncate flex-1" class:highlight={item.count > 0}>{item.common_name}</span>
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="life-list-indicator p-1" class:in-life-list={item.inLifeList} fill="#4e8db5"><title>Life list</title><path d="M21 12C21 16.9706 16.9706 21 12 21C7.02944 21 3 16.9706 3 12C3 7.02944 7.02944 3 12 3C16.9706 3 21 7.02944 21 12Z" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" stroke="#4e8db5"></path><path d="M7.5 11.3497L10.5 15.3497L16.5 9.34998" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" stroke="white"></path></svg>
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

  .life-list-indicator {
    display: none;
  }

  .life-list-indicator.in-life-list {
    display: block;
    height: 1.37rem
  }

  .sound-id-results-grid {
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
