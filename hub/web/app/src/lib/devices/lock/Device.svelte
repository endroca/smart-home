<script lang="ts">
  // Fechadura Tuya: bateria, alarmes e os últimos métodos de abertura. A
  // fechadura dorme em Wi-Fi, então o hub a lê pela nuvem; o que aparece é o
  // último estado reportado. O destrancar remoto fica para uma fase posterior.
  import Card from "$lib/ui/Card.svelte";
  import CardHeader from "$lib/ui/CardHeader.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Stat from "$lib/ui/Stat.svelte";
  import { kindBadge, roomChipName } from "$lib/devices/display";
  import { lockAlarm, lockBattery, lockFlags, lockMethods, type LockView } from "./logic";
  import LockHistory from "./LockHistory.svelte";

  let { device, onOpenConfig }: { device: LockView; onOpenConfig?: (id: string) => void } = $props();

  const specs = $derived(device.specs ?? []);
  const battery = $derived(lockBattery(device));
  const alarm = $derived(lockAlarm(device));
  const methods = $derived(lockMethods(device));
  const flags = $derived(lockFlags(device));
  const roomName = $derived(roomChipName(device.room));
  const batteryAccent = $derived(battery === null ? "neutral" : battery <= 20 ? "error" : battery <= 50 ? "tertiary" : "primary");
</script>

{#snippet badgesSnippet()}
  <Badge text={kindBadge(device.kind)} cls="border-secondary/30 text-secondary" />
  {#if device.cloud_only}<Badge text="nuvem" cls="border-outline-variant text-on-surface-variant" />{/if}
{/snippet}

{#snippet trailingSnippet()}
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
    icon="lock"
    accent="primary"
    online={device.online}
    {roomName}
    badges={badgesSnippet}
    trailing={trailingSnippet}
  />

  {#if !specs.length}
    <div class="rounded-xl border border-outline-variant bg-surface-container-low p-space-md font-body-sm text-body-sm text-on-surface-variant">
      Sem dados ainda. A fechadura dorme em Wi‑Fi; o hub relê o estado pela nuvem. Use a engrenagem para carregar as specs.
    </div>
  {:else}
    <div class="grid grid-cols-2 gap-space-sm">
      {#if battery !== null}
        <Stat label="Bateria" value={String(Math.round(battery))} unit="%" icon="battery" accent={batteryAccent} bar={battery} />
      {/if}
      <div class="flex flex-col justify-between gap-2 rounded-2xl border border-outline-variant/40 bg-surface-container-lowest/70 p-space-md {battery === null ? 'col-span-2' : ''}">
        <div class="flex items-center justify-between gap-1">
          <span class="font-label-md text-label-md text-on-surface-variant">Tranca reversa</span>
          <Icon name="lock" cls="h-4 w-4 text-on-surface-variant" />
        </div>
        <div class="mt-2.5 font-headline-md text-headline-md font-bold tracking-tight text-on-surface">
          {flags.reverse ? "Ativa" : "Normal"}
        </div>
        {#if flags.hijack || flags.doorbell}
          <div class="mt-1 flex flex-wrap gap-1.5">
            {#if flags.hijack}<span class="rounded-full border border-error/40 bg-error/10 px-2 py-0.5 font-label-sm text-label-sm text-error">Coação</span>{/if}
            {#if flags.doorbell}<span class="rounded-full border border-outline-variant px-2 py-0.5 font-label-sm text-label-sm text-on-surface-variant">Campainha</span>{/if}
          </div>
        {/if}
      </div>
    </div>

    {#if alarm}
      <div class="flex items-center gap-2.5 rounded-2xl border border-error/40 bg-error/10 p-space-md">
        <Icon name="alert" cls="h-5 w-5 shrink-0 text-error" />
        <div class="min-w-0">
          <div class="font-body-md text-body-md font-medium leading-tight text-error">Último alarme</div>
          <div class="mt-0.5 font-label-sm text-label-sm text-on-surface-variant">{alarm.label} <span class="opacity-60">({alarm.code})</span></div>
        </div>
      </div>
    {/if}

    {#if methods.length}
      <div class="grid grid-cols-2 gap-space-sm sm:grid-cols-3">
        {#each methods as m (m.spec.dp_id)}
          <div class="rounded-2xl bg-surface-container-high/70 p-space-md">
            <div class="truncate font-label-sm text-label-sm text-on-surface-variant">{m.label}</div>
            <div class="mt-0.5 truncate font-body-md text-body-md font-medium text-on-surface">
              {m.value === undefined || m.value === null || m.value === "" ? "—" : String(m.value)}
            </div>
          </div>
        {/each}
      </div>
      <div class="font-label-sm text-label-sm text-on-surface-variant">
        Último método usado por tipo (o número é o id do método). O histórico com data e nome fica no detalhe.
      </div>
    {:else}
      <div class="rounded-xl border border-outline-variant bg-surface-container-low p-space-md font-body-sm text-body-sm text-on-surface-variant">
        Nenhum método de abertura reportado ainda.
      </div>
    {/if}
  {/if}

  <LockHistory {device} />
</Card>
