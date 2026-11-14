<script lang="ts">
  import { ICONS } from "./icons";

  let { name, cls = "h-5 w-5" }: { name: string; cls?: string } = $props();
  const entry = $derived(ICONS[name] ?? ICONS.dot);
</script>

<svg
  class="shrink-0 {cls}"
  viewBox="0 0 24 24"
  aria-hidden="true"
  {...entry.f
    ? { fill: "currentColor" }
    : { fill: "none", stroke: "currentColor", "stroke-width": 2, "stroke-linecap": "round", "stroke-linejoin": "round" }}
>
  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
  {@html entry.f ?? entry.s}
</svg>
