<script lang="ts">
  // Tomada / Plug Tuya: chave mestre (dial circular) + telemetria de energia
  // (potência, tensão, corrente), timer e trava infantil. Usa os helpers de
  // ../tuya.
  import Card from "$lib/ui/Card.svelte";
  import CardHeader from "$lib/ui/CardHeader.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Stat from "$lib/ui/Stat.svelte";
  import SmallSwitch from "$lib/ui/SmallSwitch.svelte";
  import { fmtNum, fmtValue, pct } from "$lib/format";
  import { kindBadge, roomChipName } from "$lib/devices/display";
  import EnergyPanel from "../tuya/EnergyPanel.svelte";
  import { MASTER_CODES, specByCode, tuyaTelemetry, tuyaVal, type TuyaView } from "../tuya/logic";
  import { tuyaControl } from "../tuya/actions";
  import TimerButton from "$lib/automations/TimerButton.svelte";

  let { device, onOpenConfig }: { device: TuyaView; onOpenConfig?: (id: string) => void } = $props();

  const specs = $derived(device.specs ?? []);
  const master = $derived(specByCode(specs, MASTER_CODES));
  const masterOn = $derived(master ? tuyaVal(device, master) === true : false);
  const tel = $derived(tuyaTelemetry(specs));
  const countdown = $derived(specByCode(specs, ["countdown_1", "countdown"]));
  const childLock = $derived(specByCode(specs, ["child_lock"]));
  const childOn = $derived(childLock ? tuyaVal(device, childLock) === true : false);
  const roomName = $derived(roomChipName(device.room));
</script>

{#snippet badgesSnippet()}
  <Badge text={kindBadge(device.kind)} cls="border-secondary/30 text-secondary" />
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
  <div class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full bg-secondary/10 blur-3xl"></div>

  <CardHeader
    name={device.name || device.id}
    icon="plug"
    accent="primary"
    online={device.online}
    {roomName}
    badges={badgesSnippet}
    trailing={trailingSnippet}
  />

  {#if master}
    <div class="flex justify-center">
      <div class="power-dial-wrap" data-on={masterOn ? "true" : "false"}>
        <div class="power-dial-stage">
          <span class="power-dial-aura" aria-hidden="true"></span>
          <button
            type="button"
            role="switch"
            aria-checked={masterOn ? "true" : "false"}
            aria-label="Ligar ou desligar tomada"
            class="power-dial"
            data-on={masterOn ? "true" : "false"}
            onclick={() => tuyaControl(device.id, master.dp_id, !masterOn)}
          >
            <span class="power-dial-ring">
              <span class="power-dial-core"><Icon name="power" cls="power-dial-icon h-8 w-8" /></span>
            </span>
          </button>
        </div>
        <span class="power-dial-caption"><span class="power-dial-dot"></span>{masterOn ? "Ligada" : "Desligada"}</span>
      </div>
    </div>
  {/if}

  {#if tel.length}
    <div class="grid grid-cols-3 gap-space-sm">
      {#each tel as item (item.spec.dp_id)}
        {@const raw = tuyaVal(device, item.spec)}
        {@const scaled = Number(raw) * item.t.scale}
        <Stat
          label={item.t.label}
          value={fmtNum(raw, item.t.scale, item.t.digits)}
          unit={item.t.unit}
          icon={item.t.icon}
          accent={item.t.accent}
          bar={isFinite(scaled) ? pct(scaled, 0, item.t.max) : 0}
        />
      {/each}
    </div>
  {:else}
    <div
      class="rounded-xl border border-outline-variant bg-surface-container-low p-space-md font-label-sm text-label-sm text-on-surface-variant"
    >
      Sem telemetria de energia.
      {master ? "Use o botão acima para ligar/desligar." : "Carregue as specs para liberar os controles."}
    </div>
  {/if}

  {#if countdown || childLock}
    <div class="grid grid-cols-1 gap-space-sm sm:grid-cols-2">
      {#if countdown}
        <div class="flex items-center justify-between gap-2 rounded-2xl bg-surface-container-high/70 p-space-md">
          <div class="flex min-w-0 items-center gap-2.5">
            <Icon name="timer" cls="h-5 w-5 text-on-surface-variant" />
            <div class="min-w-0">
              <div class="font-body-md text-body-md font-medium leading-tight text-on-surface">Timer desligar</div>
              <div class="mt-0.5 font-label-sm text-label-sm text-on-surface-variant">
                {fmtValue(tuyaVal(device, countdown))}s
              </div>
            </div>
          </div>
        </div>
      {/if}

      {#if childLock}
        <div class="flex items-center justify-between gap-2 rounded-2xl bg-surface-container-high/70 p-space-md">
          <div class="flex min-w-0 items-center gap-2.5">
            <Icon name="child" cls="h-5 w-5 text-on-surface-variant" />
            <div class="min-w-0">
              <div class="font-body-md text-body-md font-medium leading-tight text-on-surface">Trava infantil</div>
              <div class="mt-0.5 font-label-sm text-label-sm text-on-surface-variant">
                {childOn ? "Ativa" : "Desativada"}
              </div>
            </div>
          </div>
          <SmallSwitch
            on={childOn}
            label="Trava infantil"
            onclick={() => tuyaControl(device.id, childLock.dp_id, !childOn)}
          />
        </div>
      {/if}
    </div>
  {/if}

  <EnergyPanel {device} />
</Card>
