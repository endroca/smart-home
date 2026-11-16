<script lang="ts">
  // Interruptor / Luz Tuya: os booleans viram chaves (hero quando há só uma)
  // mais os controles extras (brilho, modo, ...) na configuracao. Usa os
  // helpers de ../tuya.
  import Card from "$lib/ui/Card.svelte";
  import CardHeader from "$lib/ui/CardHeader.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import SmallSwitch from "$lib/ui/SmallSwitch.svelte";
  import Toggle from "$lib/ui/Toggle.svelte";
  import { accentOf } from "$lib/ui/accent";
  import { hub } from "$lib/stores/data.svelte";
  import { kindBadge, roomChipName } from "$lib/devices/display";
  import { dpDesc, keyLabel, tuyaBoolSpecs, tuyaVal, type TuyaView } from "../tuya/logic";
  import { tuyaControl } from "../tuya/actions";
  import TimerButton from "$lib/automations/TimerButton.svelte";

  let { device, onOpenConfig }: { device: TuyaView; onOpenConfig?: (id: string) => void } = $props();

  const EMPTY_LABELS: Record<string, string> = {};

  const specs = $derived(device.specs ?? []);
  const bools = $derived(tuyaBoolSpecs(specs).filter((s) => s.writable !== false));
  // Rotulos cadastrados (Funcoes e voz): a chave mostra o mesmo nome que a voz.
  const labels = $derived(hub.functionLabels[device.id] ?? EMPTY_LABELS);
  const hero = $derived(bools.length === 1 ? bools[0] : null);
  const heroOn = $derived(hero ? tuyaVal(device, hero) === true : false);
  const roomName = $derived(roomChipName(device.room));

  const icons: Record<string, string> = {
    switch_led: "lightbulb",
    switch_1: "power",
    switch: "power",
    power: "power",
  };
</script>

{#snippet badgesSnippet()}
  <Badge text={kindBadge(device.kind)} cls="border-secondary/30 text-secondary" />
{/snippet}

{#snippet gearSnippet()}
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

{#snippet heroTrailing()}
  <Toggle on={heroOn} label="Alternar" onclick={() => hero && tuyaControl(device.id, hero.dp_id, !heroOn)} />
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

{#if !specs.length}
  <Card>
    <div class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full bg-tertiary/10 blur-3xl"></div>
    <CardHeader
      name={device.name || device.id}
      icon="power"
      accent="tertiary"
      online={device.online}
      {roomName}
      badges={badgesSnippet}
      trailing={gearSnippet}
    />
    <div
      class="rounded-xl border border-outline-variant bg-surface-container-low p-space-md font-body-sm text-body-sm text-on-surface-variant"
    >
      Sem specs carregadas. Use a engrenagem para carregar os data points.
    </div>
  </Card>
{:else if hero}
  {@const accentIsLight = /led|light|lamp/.test(hero.code)}
  {@const accent = accentIsLight ? "tertiary" : "primary"}
  {@const a = accentOf(accent)}
  <Card>
    <div
      class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full {accentIsLight
        ? 'bg-tertiary/10'
        : 'bg-primary/10'} blur-3xl"
    ></div>
    <CardHeader
      name={device.name || device.id}
      icon={accentIsLight ? "lightbulb" : "power"}
      {accent}
      online={device.online}
      {roomName}
      badges={badgesSnippet}
      trailing={heroTrailing}
    />
    <div class="flex items-center justify-between rounded-2xl bg-surface-container-lowest/70 p-space-md">
      <div class="flex min-w-0 items-center gap-3">
        <span
          class="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl {heroOn
            ? a.soft
            : 'bg-surface-container-high'}"
        >
          <Icon
            name={accentIsLight ? "lightbulb" : "power"}
            cls="h-6 w-6 {heroOn ? a.text : 'text-on-surface-variant'}"
          />
        </span>
        <div class="min-w-0">
          <div class="truncate font-headline-sm text-headline-sm font-semibold {heroOn ? a.text : 'text-on-surface'}">
            {keyLabel(hero, labels)}
          </div>
          <div class="truncate font-label-sm text-label-sm text-on-surface-variant">
            {heroOn ? "Ligado" : "Desligado"}{dpDesc(hero) ? " • " + dpDesc(hero) : ""}
          </div>
        </div>
      </div>
      {#if heroOn}<span class="h-2.5 w-2.5 rounded-full {a.bg} shadow-lg"></span>{/if}
    </div>
  </Card>
{:else}
  {@const lightish = bools.find((s) => /led|light|lamp|lightbulb/.test(s.code))}
  <Card>
    <div
      class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full {lightish
        ? 'bg-tertiary/10'
        : 'bg-primary/10'} blur-3xl"
    ></div>
    <CardHeader
      name={device.name || device.id}
      icon={lightish ? "lightbulb" : "grid"}
      accent={lightish ? "tertiary" : "primary"}
      online={device.online}
      {roomName}
      badges={badgesSnippet}
      trailing={gearSnippet}
    />
    <div class="space-y-2">
      {#each bools as s (s.dp_id)}
        {@const on = tuyaVal(device, s) === true}
        <div class="flex items-center justify-between gap-2 rounded-2xl bg-surface-container-high/70 p-space-md">
          <div class="flex min-w-0 items-center gap-2.5">
            <Icon
              name={icons[s.code] || "power"}
              cls="h-5 w-5 shrink-0 {on ? 'text-primary' : 'text-on-surface-variant'}"
            />
            <div class="min-w-0">
              <div class="truncate font-body-md text-body-md font-medium leading-tight text-on-surface">
                {keyLabel(s, labels)}
              </div>
              <div class="truncate font-label-sm text-label-sm text-on-surface-variant">
                {dpDesc(s) || "DP " + s.dp_id}
              </div>
            </div>
          </div>
          <SmallSwitch {on} label={keyLabel(s, labels)} onclick={() => tuyaControl(device.id, s.dp_id, !on)} />
        </div>
      {/each}
    </div>
  </Card>
{/if}
