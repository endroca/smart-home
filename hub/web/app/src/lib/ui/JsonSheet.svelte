<script lang="ts">
  import { untrack } from "svelte";
  import Sheet from "./Sheet.svelte";
  import AsyncButton from "./AsyncButton.svelte";
  import { toast } from "$lib/stores/toast.svelte";

  let {
    title,
    hint = "",
    value,
    open = $bindable(false),
    onSave,
  }: {
    title: string;
    hint?: string;
    value: unknown;
    open?: boolean;
    onSave: (body: unknown) => Promise<void>;
  } = $props();

  let text = $state("");
  let busy = $state(false);
  let wasOpen = false;

  // O texto so e semeado na ABERTURA da folha. Re-semea-lo a cada mudanca de
  // `value` apagava o que o operador estava editando (o hub atualiza o estado
  // enquanto a folha esta aberta).
  $effect(() => {
    const isOpen = open;
    if (isOpen && !wasOpen) {
      untrack(() => {
        text = JSON.stringify(value, null, 2);
      });
    }
    wasOpen = isOpen;
  });

  async function save() {
    let body: unknown;
    try {
      body = JSON.parse(text);
    } catch {
      toast.show("JSON inválido", true);
      return;
    }
    busy = true;
    try {
      await onSave(body);
      open = false;
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      busy = false;
    }
  }
</script>

<Sheet {title} bind:open cls="max-w-lg">
  {#if hint}<p class="font-body-sm text-body-sm text-on-surface-variant">{hint}</p>{/if}
  <textarea
    bind:value={text}
    spellcheck="false"
    class="min-h-[200px] w-full flex-1 resize-none rounded-xl border-outline-variant bg-surface-container-lowest font-mono text-xs text-on-surface"
  ></textarea>

  {#snippet footer()}
    <div class="flex gap-2">
      <button
        type="button"
        class="press flex-1 rounded-xl border border-outline-variant py-2 text-xs text-on-surface-variant"
        onclick={() => (open = false)}
      >
        Cancelar
      </button>
      <AsyncButton label="Salvar" variant="primary" busy={busy} onclick={save} cls="flex-1" />
    </div>
  {/snippet}
</Sheet>
