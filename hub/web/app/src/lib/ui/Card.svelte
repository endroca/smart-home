<script lang="ts">
  import type { Snippet } from "svelte";

  let { children, cls = "" }: { children: Snippet; cls?: string } = $props();
</script>

<article
  class="glass-card relative flex flex-col gap-space-lg overflow-hidden rounded-3xl bg-surface-container p-space-lg shadow-xl {cls}"
>
  {@render children()}
</article>
