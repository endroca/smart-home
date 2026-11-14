<script lang="ts">
  let { on = false, label = "", onclick }: { on?: boolean; label?: string; onclick?: () => void } = $props();
</script>

<button
  type="button"
  role="switch"
  aria-checked={on ? "true" : "false"}
  aria-label={label}
  {onclick}
  class="flex h-7 w-11 shrink-0 items-center rounded-full p-0.5 transition-all {on
    ? 'bg-primary'
    : 'bg-surface-container-highest'}"
>
  <div class="h-6 w-6 rounded-full {on ? 'ml-auto bg-on-primary' : 'bg-outline'}"></div>
</button>
