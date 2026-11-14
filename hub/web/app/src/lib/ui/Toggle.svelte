<script lang="ts">
  import Icon from "./Icon.svelte";

  let { on = false, label = "", onclick }: { on?: boolean; label?: string; onclick?: () => void } = $props();
</script>

<button
  type="button"
  role="switch"
  aria-checked={on ? "true" : "false"}
  aria-label={label}
  {onclick}
  class="flex h-9 w-16 shrink-0 items-center rounded-full p-1 transition-all {on
    ? 'justify-end bg-primary'
    : 'justify-start bg-surface-container-highest'}"
>
  {#if on}
    <div class="flex h-7 w-7 items-center justify-center rounded-full bg-on-primary">
      <Icon name="check" cls="h-4 w-4 text-primary" />
    </div>
  {:else}
    <div class="h-7 w-7 rounded-full bg-outline"></div>
  {/if}
</button>
