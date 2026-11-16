<script lang="ts">
  // Interruptor multi-tecla (gang) Tuya: uma placa tatil com uma tecla por saida
  // switch_N. Cada tecla liga/desliga o seu DP; os botoes Ligar/Desligar todas
  // agem so nas teclas — os DPs de configuracao (backlight, pulsacao, ...) ficam
  // na engrenagem. Usa os helpers de ../tuya.
  import Card from "$lib/ui/Card.svelte";
  import CardHeader from "$lib/ui/CardHeader.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import { hub } from "$lib/stores/data.svelte";
  import { kindBadge, roomChipName } from "$lib/devices/display";
  import { dpDesc, keyLabel, tuyaGangSpecs, tuyaVal, type TuyaView } from "../tuya/logic";
  import { tuyaControl, tuyaSetAll } from "../tuya/actions";
  import TimerButton from "$lib/automations/TimerButton.svelte";

  let { device, onOpenConfig }: { device: TuyaView; onOpenConfig?: (id: string) => void } = $props();

  const EMPTY_LABELS: Record<string, string> = {};

  const specs = $derived(device.specs ?? []);
  const keys = $derived(tuyaGangSpecs(specs));
  // Rotulos cadastrados (Funcoes e voz): a tecla mostra o mesmo nome que a voz.
  const labels = $derived(hub.functionLabels[device.id] ?? EMPTY_LABELS);
  const anyOn = $derived(keys.some((s) => tuyaVal(device, s) === true));
  const roomName = $derived(roomChipName(device.room));

  // setAll manda so as teclas que precisam mudar, num unico comando em lote.
  function setAll(on: boolean) {
    const updates = keys
      .filter((s) => tuyaVal(device, s) !== on)
      .map((s) => ({ dp: s.dp_id, value: on }));
    void tuyaSetAll(device.id, updates);
  }
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
  <div
    class="card-glow pointer-events-none absolute -right-12 -top-12 h-48 w-48 rounded-full {anyOn
      ? 'bg-tertiary/10'
      : 'bg-primary/10'} blur-3xl"
  ></div>

  <CardHeader
    name={device.name || device.id}
    icon="lightbulb"
    accent={anyOn ? "tertiary" : "primary"}
    online={device.online}
    {roomName}
    badges={badgesSnippet}
    trailing={trailingSnippet}
  />

  {#if !keys.length}
    <div
      class="rounded-xl border border-outline-variant bg-surface-container-low p-space-md font-body-sm text-body-sm text-on-surface-variant"
    >
      Sem teclas carregadas. Use a engrenagem para carregar os data points.
    </div>
  {:else}
    <section class="gang-plate" aria-label="Teclas do interruptor">
      <div class="gang-keys" style="--gang-cols:{keys.length}">
        {#each keys as s (s.dp_id)}
          {@const on = tuyaVal(device, s) === true}
          <button
            type="button"
            role="switch"
            aria-checked={on ? "true" : "false"}
            aria-label={keyLabel(s, labels)}
            class="gang-key press"
            data-on={on ? "true" : "false"}
            onclick={() => tuyaControl(device.id, s.dp_id, !on)}
          >
            <span class="gang-key-head">
              <span class="gang-key-name">{keyLabel(s, labels)}</span>
              {#if dpDesc(s)}<span class="gang-key-desc">{dpDesc(s)}</span>{/if}
            </span>
            <span class="gang-key-sensor"><Icon name="power" cls="h-6 w-6" /></span>
            <span class="gang-key-status">
              <span class="gang-led"></span>
              <span class="gang-key-state">{on ? "Ligada" : "Desligada"}</span>
            </span>
          </button>
        {/each}
      </div>
    </section>

    <div class="grid grid-cols-2 gap-space-sm">
      <button type="button" class="gang-bulk press" onclick={() => setAll(true)}>
        <Icon name="power" cls="h-4 w-4 text-primary" />
        <span>Ligar todas</span>
      </button>
      <button type="button" class="gang-bulk press" onclick={() => setAll(false)}>
        <Icon name="power" cls="h-4 w-4 text-on-surface-variant" />
        <span>Desligar todas</span>
      </button>
    </div>
  {/if}
</Card>
