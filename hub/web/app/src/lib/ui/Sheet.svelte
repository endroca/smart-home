<script lang="ts">
  import type { Snippet } from "svelte";
  import Icon from "./Icon.svelte";

  let {
    title,
    open = $bindable(false),
    children,
    footer,
    cls = "max-w-md",
  }: {
    title: string;
    open?: boolean;
    children: Snippet;
    footer?: Snippet;
    cls?: string;
  } = $props();

  let el: HTMLDialogElement | undefined = $state();

  $effect(() => {
    if (!el) return;
    if (open && !el.open) el.showModal();
    else if (!open && el.open) el.close();
  });
</script>

<dialog
  bind:this={el}
  onclose={() => (open = false)}
  class="glass-panel m-auto modal-responsive {cls} rounded-2xl border border-outline-variant bg-surface-container p-5 text-left text-on-surface"
>
  <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
    <div class="flex items-center justify-between">
      <h2 class="font-headline-sm text-headline-sm font-semibold text-on-surface">{title}</h2>
      <button
        type="button"
        aria-label="Fechar"
        class="press flex h-8 w-8 items-center justify-center rounded-lg border border-outline-variant text-on-surface-variant"
        onclick={() => (open = false)}
      >
        <Icon name="close" cls="h-4 w-4" />
      </button>
    </div>
    <div class="modal-scroll mt-4 flex flex-1 flex-col gap-4 overflow-y-auto pr-1">
      {@render children()}
    </div>
    {#if footer}
      <div class="mt-4">{@render footer()}</div>
    {/if}
  </div>
</dialog>
