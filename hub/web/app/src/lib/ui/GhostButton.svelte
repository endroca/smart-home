<script lang="ts">
  import Icon from "./Icon.svelte";

  let {
    label = "",
    icon = "",
    danger = false,
    disabled = false,
    title = undefined,
    onclick,
  }: {
    label?: string;
    icon?: string;
    danger?: boolean;
    disabled?: boolean;
    title?: string;
    onclick?: (e: MouseEvent) => void;
  } = $props();

  const cls = $derived(
    danger
      ? "border-error/40 text-on-error-container hover:text-error"
      : "border-outline-variant text-on-surface-variant hover:text-on-surface",
  );
</script>

<button
  type="button"
  {disabled}
  {title}
  {onclick}
  class="press inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 font-label-sm text-label-sm {cls} disabled:opacity-40"
>
  {#if icon}<Icon name={icon} cls="h-4 w-4" />{/if}
  {#if label}{label}{/if}
</button>
