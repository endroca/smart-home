<script lang="ts">
  import Icon from "./Icon.svelte";

  export interface SegmentOption {
    value: string;
    label: string;
    icon?: string;
  }

  let {
    options,
    value = $bindable(""),
    onselect,
    cls = "",
    size = "md",
  }: {
    options: SegmentOption[];
    value?: string;
    onselect?: (value: string) => void;
    cls?: string;
    size?: "sm" | "md";
  } = $props();

  const pad = $derived(size === "sm" ? "px-2.5 py-1 text-[11px]" : "px-3 py-1.5 text-xs");
</script>

<div class="flex flex-wrap gap-1.5 {cls}">
  {#each options as opt (opt.value)}
    {@const active = opt.value === value}
    <button
      type="button"
      aria-pressed={active ? "true" : "false"}
      onclick={() => {
        value = opt.value;
        onselect?.(opt.value);
      }}
      class="press inline-flex items-center gap-1.5 rounded-full font-label-sm text-label-sm {pad} {active
        ? 'bg-primary text-on-primary'
        : 'border border-outline-variant/50 bg-surface-container-high/70 text-on-surface-variant hover:text-on-surface'}"
    >
      {#if opt.icon}<Icon name={opt.icon} cls="h-3.5 w-3.5" />{/if}
      {opt.label}
    </button>
  {/each}
</div>
