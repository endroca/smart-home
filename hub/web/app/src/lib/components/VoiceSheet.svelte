<script lang="ts">
  import Sheet from "$lib/ui/Sheet.svelte";
  import Input from "$lib/ui/Input.svelte";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import { voice } from "$lib/stores/voice.svelte";

  let text = $state("");

  $effect(() => {
    if (voice.open) text = "";
  });
</script>

<Sheet title="Comando de voz" bind:open={voice.open}>
  <p class="font-body-sm text-body-sm text-on-surface-variant">{voice.hint}</p>
  <Input bind:value={text} placeholder="ligar luz do escritório" onenter={() => voice.submit(text)} />

  {#snippet footer()}
    <div class="flex gap-2">
      <button
        type="button"
        class="press flex-1 rounded-xl border border-outline-variant py-2.5 text-xs text-on-surface-variant"
        onclick={() => (voice.open = false)}
      >
        Cancelar
      </button>
      <AsyncButton label="Enviar" variant="primary" onclick={() => voice.submit(text)} cls="flex-1" />
    </div>
  {/snippet}
</Sheet>
