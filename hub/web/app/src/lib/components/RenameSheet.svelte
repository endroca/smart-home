<script lang="ts">
  import Sheet from "$lib/ui/Sheet.svelte";
  import Input from "$lib/ui/Input.svelte";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import { rename } from "$lib/stores/rename.svelte";
  import { saveRename } from "$lib/actions/rename";
</script>

<Sheet title="Renomear dispositivo" bind:open={rename.open}>
  <p class="font-body-sm text-body-sm text-on-surface-variant">
    O nome é só exibição no hub. O nome no dispositivo, o ID e o cômodo não mudam.
  </p>
  <Input bind:value={rename.name} maxlength={32} placeholder="Nome do dispositivo" onenter={saveRename} />

  {#snippet footer()}
    <div class="flex gap-2">
      <button
        type="button"
        class="press flex-1 rounded-xl border border-outline-variant py-2.5 text-xs text-on-surface-variant"
        onclick={() => rename.close()}
      >
        Cancelar
      </button>
      <AsyncButton label="Salvar" variant="primary" busy={rename.busy} onclick={saveRename} cls="flex-1" />
    </div>
  {/snippet}
</Sheet>
