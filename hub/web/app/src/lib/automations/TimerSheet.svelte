<script lang="ts">
  // Folha de temporizadores voláteis de um device: "desligar daqui a X", "ligar
  // daqui a X", "às HH:MM" e o par "ligar às X / desligar às Y". Cada criação
  // gera automações voláteis (once: true) que o hub dispara uma vez e apaga.
  import { hub } from "$lib/stores/data.svelte";
  import { deleteAutomation, saveAutomation } from "$lib/actions/automations";
  import {
    clockTimer,
    offTarget,
    onTarget,
    pairTimers,
    relativeTimer,
    remainingLabel,
    TIMER_PRESETS,
    timersFor,
  } from "$lib/automations/timer";
  import {
    customTimer,
    effectiveKind,
    timerActionText,
    timerControls,
    timerDefaultValue,
    timerValues,
  } from "$lib/automations/timerAction";
  import type { FanView } from "$lib/devices/fan/logic";
  import type { MideaView } from "$lib/devices/midea/logic";
  import type { TuyaView } from "$lib/devices/tuya/logic";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import Field from "$lib/ui/Field.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Input from "$lib/ui/Input.svelte";
  import Select from "$lib/ui/Select.svelte";
  import Sheet from "$lib/ui/Sheet.svelte";
  import type { Automation, HubDevice } from "$lib/types";

  let { device, open = $bindable(false) }: { device: HubDevice; open?: boolean } = $props();

  let mode = $state<"off" | "on" | "pair">("off");
  let at = $state("22:00");
  let onAt = $state("10:00");
  let offAt = $state("14:00");
  let busy = $state(false);
  let error = $state("");

  // "Personalize a ação": o usuario escolhe o comando do device (num
  // interruptor de 3 botoes, qual chave) e o valor (ligar/desligar, velocidade…).
  let customOpen = $state(false);
  let customControl = $state("");
  let customValue = $state("");
  let customAt = $state("22:00");

  const off = $derived(offTarget(device));
  const on = $derived(onTarget(device));
  // O alvo do modo relativo/horário atual ("desligar" ou "ligar").
  const target = $derived(mode === "on" ? on : off);
  const verb = $derived(mode === "on" ? "Ligar" : "Desligar");
  const active = $derived(timersFor(device, hub.automations));

  // O device recebido ja e a view da familia (FanView/TuyaView/MideaView) nas
  // telas de card; o fallback por hub.specs cobre o caso de specs ainda vazias.
  const fan = $derived(device.kind === "fan" ? (device as FanView) : undefined);
  const midea = $derived(device.kind === "midea" ? (device as MideaView) : undefined);
  const viewSpecs = $derived(device.kind === "tuya" ? ((device as TuyaView).specs ?? []) : []);
  const specs = $derived(viewSpecs.length ? viewSpecs : (hub.specs[device.id] ?? []));
  const controls = $derived(timerControls(device, specs, fan, midea));
  const controlOptions = $derived(controls.map((c) => ({ value: c.value, label: c.label })));
  const selectedCustom = $derived(controls.find((c) => c.value === customControl));
  const customValues = $derived(timerValues(selectedCustom));
  const customKind = $derived(selectedCustom ? effectiveKind(selectedCustom) : "bool");
  const customText = $derived(timerActionText(selectedCustom, customValue));

  // Especs chegam de forma assincrona: com o bloco aberto, escolhe o primeiro
  // comando assim que o catalogo fica disponivel (ou se o device mudar).
  $effect(() => {
    if (!customOpen) return;
    if (customControl && controls.some((c) => c.value === customControl)) return;
    const first = controls[0];
    customControl = first?.value ?? "";
    customValue = timerDefaultValue(first);
  });

  async function createAll(bodies: Automation[]) {
    error = "";
    if (!bodies.length) {
      error = "Este dispositivo não aceita esta ação.";
      return;
    }
    busy = true;
    try {
      for (const body of bodies) await saveAutomation(body);
      open = false;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }

  function inPreset(seconds: number) {
    if (target) void createAll([relativeTimer(target, seconds, verb)]);
  }

  function atTime() {
    if (target) void createAll([clockTimer(target, at, verb)]);
  }

  function schedulePair() {
    void createAll(pairTimers(device, device, onAt, offAt));
  }

  function toggleCustom() {
    customOpen = !customOpen;
  }

  function setCustomControl(value: string) {
    customControl = value;
    customValue = timerDefaultValue(controls.find((c) => c.value === value));
  }

  function customIn(seconds: number) {
    const body = customTimer(device, selectedCustom, customValue, { seconds });
    if (body) void createAll([body]);
  }

  function customAtTime() {
    const body = customTimer(device, selectedCustom, customValue, { at: customAt });
    if (body) void createAll([body]);
  }
</script>

<Sheet title="Temporizador" bind:open cls="max-w-lg">
  <div class="flex flex-col gap-4">
    <p class="font-body-sm text-body-sm text-on-surface-variant">
      Ação única em <span class="font-semibold text-on-surface">{device.name || device.id}</span>. Depois de
      executar, o temporizador some sozinho.
    </p>

    {#if !off}
      <p class="rounded-xl border border-outline-variant bg-surface-container-low px-3 py-2 font-body-sm text-body-sm text-on-surface-variant">
        Este dispositivo não expõe uma ação de ligar/desligar. Carregue as funções dele em Ajustes.
      </p>
    {:else}
      <div class="grid grid-cols-3 gap-1.5">
        <button
          type="button"
          class="press rounded-xl px-1 py-2 text-center font-label-sm text-label-sm leading-tight {mode === 'off' ? 'bg-primary text-on-primary' : 'border border-outline-variant text-on-surface-variant'}"
          onclick={() => (mode = "off")}
        >
          Desligar
        </button>
        <button
          type="button"
          class="press rounded-xl px-1 py-2 text-center font-label-sm text-label-sm leading-tight {mode === 'on' ? 'bg-primary text-on-primary' : 'border border-outline-variant text-on-surface-variant'}"
          onclick={() => (mode = "on")}
        >
          Ligar
        </button>
        <button
          type="button"
          class="press rounded-xl px-1 py-2 text-center font-label-sm text-label-sm leading-tight {mode === 'pair' ? 'bg-primary text-on-primary' : 'border border-outline-variant text-on-surface-variant'}"
          onclick={() => (mode = "pair")}
        >
          Ligar e desligar
        </button>
      </div>

      {#if mode === "pair"}
        <div class="grid grid-cols-2 gap-2">
          <Field label="Ligar às">
            <Input type="time" bind:value={onAt} />
          </Field>
          <Field label="Desligar às">
            <Input type="time" bind:value={offAt} />
          </Field>
        </div>
        <p class="font-label-sm text-label-sm text-on-surface-variant">
          Cria duas automações voláteis: ligar {device.name || device.id} às {onAt} e desligar às {offAt}.
        </p>
        <AsyncButton label="Programar ligar e desligar" variant="primary" busy={busy} onclick={schedulePair} />
      {:else}
        <div class="grid grid-cols-2 gap-2">
          {#each TIMER_PRESETS as preset (preset.seconds)}
            <button
              type="button"
              disabled={busy}
              class="press rounded-xl border border-outline-variant bg-surface-container-low px-3 py-2.5 font-body-md text-body-md text-on-surface hover:border-primary disabled:opacity-50"
              onclick={() => inPreset(preset.seconds)}
            >
              {verb} em {preset.label}
            </button>
          {/each}
        </div>

        <Field label={`Ou ${verb.toLowerCase()} às`} hint="Usa o horário local do hub; se já passou, vale amanhã.">
          <div class="flex gap-2">
            <Input type="time" bind:value={at} cls="flex-1" />
            <AsyncButton label={verb} variant="primary" busy={busy} onclick={atTime} />
          </div>
        </Field>
      {/if}

      <div class="rounded-xl border border-outline-variant">
        <button
          type="button"
          class="press flex w-full items-center justify-between gap-2 rounded-xl bg-surface-container-low px-3 py-2 text-left"
          aria-expanded={customOpen}
          onclick={toggleCustom}
        >
          <span class="flex min-w-0 flex-col">
            <span class="font-label-md text-label-md font-semibold text-on-surface">Ou personalize a ação</span>
            <span class="truncate font-label-sm text-label-sm text-on-surface-variant">
              {controls.length
                ? "Escolha o comando e quando executar"
                : "Nenhuma ação disponível neste dispositivo"}
            </span>
          </span>
          <Icon
            name={customOpen ? "chevron_down" : "chevron_right"}
            cls="h-4 w-4 shrink-0 text-on-surface-variant"
          />
        </button>

        {#if customOpen}
          <div class="flex flex-col gap-3 border-t border-outline-variant p-3">
            {#if !controls.length}
              <p class="font-label-sm text-label-sm text-on-surface-variant">
                Este dispositivo não expõe comandos para agendar. Carregue as funções dele em Ajustes.
              </p>
            {:else}
              <div class="grid grid-cols-2 gap-2">
                <Field label="Ação">
                  <Select
                    value={customControl}
                    options={controlOptions}
                    onchange={setCustomControl}
                    cls="w-full"
                  />
                </Field>
                <Field label="Valor">
                  {#if customKind === "bool" || customValues.length}
                    <Select
                      value={customValue}
                      options={customValues}
                      onchange={(v) => (customValue = v)}
                      cls="w-full"
                    />
                  {:else}
                    <Input type={customKind === "number" ? "number" : "text"} bind:value={customValue} />
                  {/if}
                </Field>
              </div>

              <p class="font-label-sm text-label-sm text-on-surface-variant">
                Vai executar <span class="font-semibold text-on-surface">{customText}</span> ·
                {device.name || device.id}
              </p>

              <div class="grid grid-cols-2 gap-2">
                {#each TIMER_PRESETS as preset (preset.seconds)}
                  <button
                    type="button"
                    disabled={busy}
                    class="press rounded-xl border border-outline-variant bg-surface-container-low px-3 py-2.5 font-body-md text-body-md text-on-surface hover:border-primary disabled:opacity-50"
                    onclick={() => customIn(preset.seconds)}
                  >
                    em {preset.label}
                  </button>
                {/each}
              </div>

              <Field label="Ou às" hint="Usa o horário local do hub; se já passou, vale amanhã.">
                <div class="flex gap-2">
                  <Input type="time" bind:value={customAt} cls="flex-1" />
                  <AsyncButton label="Agendar" variant="primary" busy={busy} onclick={customAtTime} />
                </div>
              </Field>
            {/if}
          </div>
        {/if}
      </div>
    {/if}

    {#if active.length}
      <div class="flex flex-col gap-2">
        <h3 class="font-label-md text-label-md font-semibold text-on-surface">Temporizadores ativos</h3>
        {#each active as a (a.id)}
          {@const remaining = remainingLabel(a.source?.fire_at)}
          <div class="flex items-center justify-between gap-2 rounded-xl border border-outline-variant bg-surface-container-low px-3 py-2">
            <div class="flex min-w-0 items-center gap-2">
              <Icon name="timer" cls="h-5 w-5 shrink-0 text-tertiary" />
              <div class="min-w-0">
                <div class="truncate font-body-sm text-body-sm text-on-surface">{a.name || "Temporizador"}</div>
                <div class="font-label-sm text-label-sm text-on-surface-variant">{remaining || "aguardando"}</div>
              </div>
            </div>
            <GhostButton label="Cancelar" danger onclick={() => deleteAutomation(a.id)} />
          </div>
        {/each}
      </div>
    {/if}

    {#if error}
      <p class="rounded-xl border border-error/40 bg-error/10 px-3 py-2 font-body-sm text-body-sm text-on-error-container">{error}</p>
    {/if}
  </div>

  {#snippet footer()}
    <button
      type="button"
      class="press w-full rounded-xl border border-outline-variant py-2 text-xs text-on-surface-variant"
      onclick={() => (open = false)}
    >
      Fechar
    </button>
  {/snippet}
</Sheet>
