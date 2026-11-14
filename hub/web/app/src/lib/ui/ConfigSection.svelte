<script lang="ts">
  import type { Snippet } from "svelte";

  let { title, children }: { title: string; children: Snippet } = $props();
</script>

<section class="space-y-2">
  <h4 class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">{title}</h4>
  {@render children()}
</section>
