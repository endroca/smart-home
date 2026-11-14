<script lang="ts">
  import Icon from "./Icon.svelte";

  let { name }: { name: string } = $props();
</script>

<span
  class="inline-flex items-center gap-1 rounded-full border border-outline-variant/50 bg-surface-container-high/70 px-2 py-0.5 font-label-sm text-label-sm text-on-surface-variant"
>
  <Icon name="grid" cls="h-3 w-3" />
  {name}
</span>
