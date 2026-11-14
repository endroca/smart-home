<script lang="ts">
  import type { Snippet } from "svelte";

  let { label, hint = "", children }: { label: string; hint?: string; children: Snippet } = $props();
</script>

<label class="flex flex-col gap-1">
  <span class="font-label-sm text-label-sm text-on-surface-variant">{label}</span>
  {@render children()}
  {#if hint}<span class="font-body-sm text-body-sm text-on-surface-variant">{hint}</span>{/if}
</label>
