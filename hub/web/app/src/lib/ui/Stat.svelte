<script lang="ts">
  import Icon from "./Icon.svelte";
  import { accentOf } from "./accent";

  let {
    label,
    value,
    unit = "",
    icon = "dot",
    accent = "primary",
    bar = null,
    foot = "",
  }: {
    label: string;
    value: string;
    unit?: string;
    icon?: string;
    accent?: string;
    bar?: number | null;
    foot?: string;
  } = $props();

  const a = $derived(accentOf(accent));
</script>

<div
  class="relative flex flex-col justify-between overflow-hidden rounded-2xl border border-outline-variant/40 bg-surface-container-lowest/70 p-space-md"
>
  <div class="flex items-center justify-between gap-1">
    <span class="font-label-md text-label-md text-on-surface-variant">{label}</span>
    <Icon name={icon} cls="h-4 w-4 {a.text}" />
  </div>
  <div class="mt-2.5 flex items-baseline gap-1">
    <span class="whitespace-nowrap text-[21px] leading-7 font-bold tracking-tight sm:font-headline-md sm:text-headline-md {a.text}">
      {value}
    </span>
    <span class="font-label-md text-label-md text-on-surface-variant">{unit}</span>
  </div>
  {#if bar != null}
    <div class="mt-2.5 h-1.5 w-full overflow-hidden rounded-full bg-surface-container-high">
      <div class="h-full rounded-full {a.bg}" style="width:{Math.round(bar)}%"></div>
    </div>
  {/if}
  {#if foot}
    <div class="mt-2 font-label-sm text-label-sm text-on-surface-variant">{foot}</div>
  {/if}
</div>
