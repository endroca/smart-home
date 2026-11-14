<script lang="ts">
  import Card from "$lib/ui/Card.svelte";
  import CardHeader from "$lib/ui/CardHeader.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Pressable from "$lib/ui/Pressable.svelte";
  import { kindBadge, roomChipName } from "$lib/devices/display";
  import { SPEED_LABELS, fanControls, fanSpeedClass, type FanView } from "./logic";
  import { fanCorrect, setFan } from "./actions";
  import TimerButton from "$lib/automations/TimerButton.svelte";

  let { device, onOpenConfig }: { device: FanView; onOpenConfig?: (id: string) => void } = $props();

  const c = $derived(fanControls(device));

  // "Desligar" e UM comando nativo que desliga tudo (motor e luz); mandar light
  // junto geraria um segundo frame RF. O firmware espelha a luz no estado.
  const OFF: Record<string, unknown> = { power: false };

  // A direcao e estado configurado, nao indicador de funcionamento: toque envia
  // SO a direcao (um frame), hold so corrige o estado conhecido.
  function set(partial: Record<string, unknown>) {
    return () => setFan(device.thing, device.device, partial);
  }
  function correct(partial: Record<string, unknown>) {
    return () => fanCorrect(device.thing, device.device, partial);
  }
</script>

{#snippet badgesSnippet()}
  <Badge text={kindBadge(device.kind)} cls="border-outline-variant text-on-surface-variant" />
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

{#if device.pendingManifest}
  <Card>
    <div class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full bg-tertiary/10 blur-3xl"></div>
    <CardHeader
      name={device.name}
      icon="fan"
      accent="tertiary"
      online={false}
      roomName={roomChipName(device.room)}
      badges={badgesSnippet}
      trailing={trailingSnippet}
    />
    <div class="rounded-xl border border-outline-variant bg-surface-container-low p-space-md font-body-sm text-body-sm text-on-surface-variant">
      Cadastrado, mas o hub ainda não recebeu o manifesto do gateway. Os controles aparecem assim que a descoberta na LAN
      terminar.
    </div>
  </Card>
{:else}
  <Card>
    <div class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full bg-primary/10 blur-3xl"></div>
    <CardHeader
      name={device.name}
      icon="fan"
      accent="primary"
      online={device.online}
      roomName={roomChipName(device.room)}
      spin={c.running}
      spinCls={fanSpeedClass(c)}
      badges={badgesSnippet}
      trailing={trailingSnippet}
    />

    <div class="ctrl-console">
      <div class="ctrl-grid">
        <Pressable
          cls="ctrl-tile{c.hasLight ? '' : ' ctrl-span-2'}"
          dataTone="neutral"
          dataArmed
          ariaLabel="Desligar"
          onTap={set(OFF)}
          onHold={correct(OFF)}
        >
          <Icon name="power" cls="h-5 w-5" />
          <span class="ctrl-tile-label">Desligar</span>
        </Pressable>

        {#if c.hasLight}
          <Pressable
            cls="ctrl-tile"
            dataTone="tertiary"
            dataActive={!!device.reported.light}
            ariaLabel="Luz"
            onTap={set({ light: !device.reported.light })}
            onHold={correct({ light: !device.reported.light })}
          >
            <Icon name="lightbulb" cls="h-5 w-5" />
            <span class="ctrl-tile-label">Luz</span>
          </Pressable>
        {/if}

        {#if c.hasDirection}
          <Pressable
            cls="ctrl-tile"
            dataTone="primary"
            dataActive={!c.reverse}
            ariaLabel="Ventilar"
            onTap={set({ direction: "forward" })}
            onHold={correct({ direction: "forward" })}
          >
            <Icon name="arrow_down" cls="h-5 w-5" />
            <span class="ctrl-tile-label">Ventilar</span>
          </Pressable>
          <Pressable
            cls="ctrl-tile"
            dataTone="secondary"
            dataActive={c.reverse}
            ariaLabel="Exaustar"
            onTap={set({ direction: "reverse" })}
            onHold={correct({ direction: "reverse" })}
          >
            <Icon name="arrow_up" cls="h-5 w-5" />
            <span class="ctrl-tile-label">Exaustar</span>
          </Pressable>
        {/if}
      </div>
    </div>

    {#if c.hasSpeed}
      <div>
        <div class="mb-2 flex items-center justify-between gap-2">
          <span class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Nível de ventilação</span>
          <span class="shrink-0 font-label-sm text-label-sm font-bold text-primary">{c.speed}/6 • {SPEED_LABELS[c.speed]}</span>
        </div>
        <div class="speed-track">
          {#each [0, 1, 2, 3, 4, 5, 6] as n (n)}
            <Pressable
              cls="speed-seg"
              ariaLabel={`Velocidade ${n}: ${SPEED_LABELS[n]}`}
              ariaPressed={c.speed === n}
              dataActive={c.speed === n}
              onTap={set({ speed: n })}
              onHold={correct({ speed: n })}
            >
              <span class="speed-seg-num">{n}</span>
              <span class="speed-seg-label">{SPEED_LABELS[n]}</span>
            </Pressable>
          {/each}
        </div>
      </div>
    {/if}
  </Card>
{/if}
