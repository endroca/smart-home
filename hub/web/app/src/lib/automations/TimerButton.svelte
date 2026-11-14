<script lang="ts">
  // Botão de temporizador de um device. Some sozinho quando o device não tem
  // ação de ligar/desligar. Marca quando há um temporizador ativo.
  import { hub } from "$lib/stores/data.svelte";
  import TimerSheet from "$lib/automations/TimerSheet.svelte";
  import { offTarget, timersFor } from "$lib/automations/timer";
  import Icon from "$lib/ui/Icon.svelte";
  import type { HubDevice } from "$lib/types";

  let { device }: { device: HubDevice } = $props();

  const available = $derived(!!offTarget(device));
  const active = $derived(timersFor(device, hub.automations).length > 0);
  let open = $state(false);
</script>

{#if available}
  <button
    type="button"
    aria-label="Temporizador"
    class="press relative flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border {active
      ? 'border-tertiary text-tertiary'
      : 'border-outline-variant text-on-surface-variant'} hover:text-primary"
    onclick={() => (open = true)}
  >
    <Icon name="timer" cls="h-5 w-5" />
    {#if active}<span class="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-tertiary"></span>{/if}
  </button>
  {#if open}<TimerSheet {device} bind:open />{/if}
{/if}
