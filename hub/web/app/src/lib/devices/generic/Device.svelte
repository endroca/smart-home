<script lang="ts">
  import Badge from "$lib/ui/Badge.svelte";
  import Card from "$lib/ui/Card.svelte";
  import CardHeader from "$lib/ui/CardHeader.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import { kindBadge, roomChipName } from "$lib/devices/display";
  import { genericChips, type GenericView } from "./logic";
  import TimerButton from "$lib/automations/TimerButton.svelte";

  let { device, onOpenConfig }: { device: GenericView; onOpenConfig?: (id: string) => void } = $props();

  const chips = $derived(genericChips(device));
</script>

{#snippet badgesSnippet()}
  {#if device.kind}<Badge text={kindBadge(device.kind)} cls="border-outline-variant text-on-surface-variant" />{/if}
{/snippet}

{#snippet trailingSnippet()}
  <TimerButton {device} />
  {#if onOpenConfig}
    <button
      type="button"
      aria-label="Configurar dispositivo"
      class="press flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border border-outline-variant text-on-surface-variant hover:text-primary"
      onclick={() => onOpenConfig?.(device.id)}
    >
      <Icon name="settings" cls="h-5 w-5" />
    </button>
  {/if}
{/snippet}

<Card>
  <div class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full bg-primary/10 blur-3xl"></div>
  <CardHeader
    name={device.name || device.id}
    icon="grid"
    accent="primary"
    online={device.online}
    roomName={roomChipName(device.room)}
    badges={badgesSnippet}
    trailing={trailingSnippet}
  />
  {#if chips.length}
    <div class="flex flex-wrap gap-1.5">
      {#each chips as c (c.label)}
        <span class="rounded-full border border-outline-variant bg-surface-container-low px-2 py-0.5 font-label-sm text-label-sm text-on-surface-variant">
          {c.label}: {c.value}
        </span>
      {/each}
    </div>
  {:else}
    <div class="rounded-xl border border-outline-variant bg-surface-container-low p-space-md font-body-sm text-body-sm text-on-surface-variant">
      Nenhum estado conhecido ainda. Use a engrenagem para carregar as specs ou ouvir o dispositivo.
    </div>
  {/if}
</Card>
