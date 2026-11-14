<script lang="ts">
  import Icon from "./Icon.svelte";

  // Botao de acao com estado de carregamento embutido. Toda mutacao passa por
  // aqui (ou por um controle equivalente) para nunca ficar "mudo".
  let {
    label,
    icon = "",
    variant = "ghost",
    busy = false,
    disabled = false,
    title = undefined,
    onclick,
    cls = "",
  }: {
    label: string;
    icon?: string;
    variant?: "primary" | "ghost" | "danger";
    busy?: boolean;
    disabled?: boolean;
    title?: string;
    onclick?: (e: MouseEvent) => void;
    cls?: string;
  } = $props();

  const variantCls = $derived(
    variant === "primary"
      ? "border-transparent bg-primary text-on-primary"
      : variant === "danger"
        ? "border-error/40 text-on-error-container hover:text-error"
        : "border-outline-variant text-on-surface-variant hover:text-on-surface",
  );
</script>

<button
  type="button"
  disabled={disabled || busy}
  {title}
  {onclick}
  aria-busy={busy ? "true" : "false"}
  class="press inline-flex items-center justify-center gap-1.5 rounded-lg border px-3 py-1.5 font-label-sm text-label-sm {variantCls} {cls} disabled:opacity-50"
>
  {#if busy}
    <span class="inline-block h-3.5 w-3.5 animate-spin rounded-full border-2 border-current border-t-transparent"></span>
  {:else if icon}
    <Icon name={icon} cls="h-4 w-4" />
  {/if}
  {label}
</button>
