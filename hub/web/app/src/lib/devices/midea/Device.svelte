<!-- Card do ar-condicionado Midea. Portado de renderMidea/mideaSegments/
     mideaTelemetry (web/devices/midea.js): console de temperatura, modos,
     ventilacao, funcoes e as abas avancadas (aletas/brisa/ar externo). -->
<script lang="ts">
  import { hub } from "$lib/stores/data.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import Card from "$lib/ui/Card.svelte";
  import CardHeader from "$lib/ui/CardHeader.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Stat from "$lib/ui/Stat.svelte";
  import { kindBadge, roomChipName } from "$lib/devices/display";
  import { mideaSet } from "./actions";
  import TimerButton from "$lib/automations/TimerButton.svelte";
  import {
    MIDEA_ANGLES,
    MIDEA_BREEZE,
    MIDEA_FRESH_AIR,
    MIDEA_MODES,
    MIDEA_SPEEDS,
    MIDEA_TOGGLES,
    mideaHasSpec,
    mideaTargetTemp,
    mideaTelemetryStats,
    mideaTempLimits,
    type MideaEntry,
    type MideaView,
  } from "./logic";

  let { device, onOpenConfig }: { device: MideaView; onOpenConfig?: (id: string) => void } = $props();

  // Prefere o estado ao vivo do store (SSE/poll), caindo para a view recebida.
  const live = $derived(hub.midea.devices.find((d) => d.id === device.id));
  const s = $derived((live?.state ?? device.state ?? {}) as Record<string, unknown>);
  const specs = $derived(live?.actions ?? device.specs ?? []);
  const online = $derived(live?.online ?? device.online);
  const name = $derived(device.name || live?.name || "Ar-condicionado");
  const roomName = $derived(roomChipName(device.room));

  const temp = $derived(mideaTargetTemp(s, specs));
  const limits = $derived(mideaTempLimits(specs));
  const on = $derived(!!s.power);
  const telemetry = $derived(mideaTelemetryStats(s));

  const toggles = $derived(MIDEA_TOGGLES.filter(([key]) => mideaHasSpec(specs, key)));
  const hasAngle = $derived(mideaHasSpec(specs, "airflow_angle"));
  const hasBreeze = $derived(mideaHasSpec(specs, "breeze"));
  const hasFreshAir = $derived(mideaHasSpec(specs, "fresh_air"));
  const angle = $derived(MIDEA_ANGLES.some(([v]) => v === Number(s.airflow_angle)) ? Number(s.airflow_angle) : 0);

  const step = (delta: number) => mideaSet(device.id, { target_temp: temp + delta });
</script>

{#snippet segRow(list: readonly MideaEntry<string | number>[], active: unknown, cols: number, pick: (v: string | number) => void)}
  <div class="speed-track" style="grid-template-columns:repeat({cols},minmax(0,1fr))">
    {#each list as [value, label, glyph] (value)}
      {@const seg = active === value}
      <button
        type="button"
        aria-pressed={seg ? "true" : "false"}
        aria-label={label}
        class="speed-seg"
        data-active={seg ? "true" : "false"}
        onclick={() => pick(value)}
      >
        {#if glyph && glyph.length > 1}
          <Icon name={glyph} cls="h-4 w-4" />
        {:else}
          <span class="speed-seg-num">{glyph ?? ""}</span>
        {/if}
        <span class="speed-seg-label">{label}</span>
      </button>
    {/each}
  </div>
{/snippet}

{#snippet badges()}
  <Badge text={kindBadge(device.kind)} cls="border-secondary/30 text-secondary" />
{/snippet}

{#snippet trailing()}
  <TimerButton {device} />
  {#if onOpenConfig}
    <button
      type="button"
      onclick={() => onOpenConfig?.(device.id)}
      aria-label="Configurar dispositivo"
      class="press flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border border-outline-variant text-on-surface-variant hover:text-primary"
    >
      <Icon name="settings" cls="h-5 w-5" />
    </button>
  {/if}
{/snippet}

<Card>
  <div class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full bg-secondary/10 blur-3xl"></div>

  <CardHeader
    name={name}
    icon="snowflake"
    accent="secondary"
    {online}
    {roomName}
    {badges}
    {trailing}
  />

  <div
    class="display-inset flex items-stretch justify-between gap-2 rounded-2xl border border-outline-variant bg-gradient-to-b from-surface-container-high to-surface-container-lowest p-3 sm:p-4"
  >
    <button
      type="button"
      role="switch"
      aria-checked={on ? "true" : "false"}
      aria-label="Ligar ou desligar"
      onclick={() => mideaSet(device.id, { power: !on })}
      class="press flex h-20 w-16 shrink-0 items-center justify-center rounded-2xl border p-1 {on
        ? 'border-primary/50 bg-gradient-to-b from-primary/15 to-primary/5 text-primary'
        : 'border-outline-variant bg-gradient-to-b from-surface-container-high to-surface-container text-on-surface-variant'}"
    >
      <Icon name="power" cls="h-6 w-6" />
    </button>

    <div class="flex min-w-0 flex-1 flex-col items-center justify-center px-1">
      <span class="text-xs font-semibold uppercase tracking-widest text-on-surface-variant">Temperatura alvo</span>
      <div class="mt-0.5 flex items-baseline justify-center">
        <span class="font-mono text-[52px] font-black leading-[0.9] tracking-tighter text-on-surface tabular-nums">{temp}</span>
        <span class="ml-1 text-2xl font-bold text-secondary">°C</span>
      </div>
      <div class="mt-1 flex items-center gap-1.5 font-mono text-[10px] text-on-surface-variant">
        <span>MÍN {limits.min}°C</span>
        <span class="h-1 w-1 rounded-full bg-outline"></span>
        <span>MÁX {limits.max}°C</span>
      </div>
    </div>

    <div class="flex shrink-0 flex-col gap-2">
      <button
        type="button"
        aria-label="Aumentar 1 grau"
        disabled={!on || temp >= limits.max}
        onclick={() => step(1)}
        class="press flex h-[38px] w-14 items-center justify-center rounded-xl border border-outline-variant bg-gradient-to-b from-surface-container-high to-surface-container text-on-surface hover:text-primary disabled:opacity-30"
      >
        <Icon name="plus" cls="h-5 w-5" />
      </button>
      <button
        type="button"
        aria-label="Diminuir 1 grau"
        disabled={!on || temp <= limits.min}
        onclick={() => step(-1)}
        class="press flex h-[38px] w-14 items-center justify-center rounded-xl border border-outline-variant bg-gradient-to-b from-surface-container-high to-surface-container text-on-surface hover:text-primary disabled:opacity-30"
      >
        <Icon name="minus" cls="h-5 w-5" />
      </button>
    </div>
  </div>

  {#if telemetry.length}
    <div class="grid grid-cols-2 gap-space-sm">
      {#each telemetry as t (t.key)}
        <Stat label={t.label} value={t.value} unit={t.unit} icon={t.icon} accent={t.accent} />
      {/each}
    </div>
  {/if}

  <section class="space-y-2">
    <span class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Modo de operação</span>
    {@render segRow(MIDEA_MODES, s.mode, 5, (v) => mideaSet(device.id, { mode: v }))}
  </section>

  <section class="space-y-2">
    <span class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Ventilação</span>
    {@render segRow(MIDEA_SPEEDS, s.fan_speed, 6, (v) => mideaSet(device.id, { fan_speed: v }))}
  </section>

  {#if toggles.length}
    <div class="ctrl-grid">
      {#each toggles as [key, label, glyph] (key)}
        {@const active = !!s[key]}
        <button
          type="button"
          aria-pressed={active ? "true" : "false"}
          aria-label={label}
          class="ctrl-tile"
          data-tone="secondary"
          data-active={active ? "true" : "false"}
          onclick={() => mideaSet(device.id, { [String(key)]: !active })}
        >
          <Icon name={glyph ?? "dot"} cls="h-5 w-5" />
          <span class="ctrl-tile-label">{label}</span>
        </button>
      {/each}
    </div>
  {/if}

  {#if hasAngle}
    <section class="space-y-2">
      <span class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Posição das aletas</span>
      {@render segRow(MIDEA_ANGLES, angle, 6, (v) => mideaSet(device.id, { airflow_angle: v }))}
    </section>
  {/if}

  {#if hasBreeze}
    <section class="space-y-2">
      <span class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Brisa</span>
      {@render segRow(MIDEA_BREEZE, s.breeze, 4, (v) => mideaSet(device.id, { breeze: v }))}
    </section>
  {/if}

  {#if hasFreshAir}
    <section class="space-y-2">
      <span class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Ar externo</span>
      {@render segRow(MIDEA_FRESH_AIR, s.fresh_air, 5, (v) => mideaSet(device.id, { fresh_air: v }))}
    </section>
  {/if}
</Card>
