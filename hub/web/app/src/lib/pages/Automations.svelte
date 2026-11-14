<script lang="ts">
  import { hub } from "$lib/stores/data.svelte";
  import {
    automationTemplate,
    deleteAutomation,
    runAutomation,
    saveAutomation,
    toggleAutomation,
  } from "$lib/actions/automations";
  import Editor from "$lib/automations/Editor.svelte";
  import {
    actionsSummary,
    buildDeviceContracts,
    mideaEventOptions,
    scheduleLabel,
    sourceSummary,
    tuyaEventOptions,
    type EventOption,
    type SummaryLookups,
  } from "$lib/automations/logic";
  import { volatileLabel } from "$lib/automations/timer";
  import { buildFans } from "$lib/devices/fan/logic";
  import { buildMideaDevices } from "$lib/devices/midea/logic";
  import { buildTuyaDevices, dpLabel } from "$lib/devices/tuya/logic";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import type { Automation } from "$lib/types";

  let editing = $state<Automation | null>(null);
  let sheetOpen = $state(false);
  let busyId = $state("");

  const fans = $derived(buildFans(hub.things, hub.devices));
  const tuyaViews = $derived(buildTuyaDevices(hub.tuya.devices, hub.specs, hub.tuya.listening, hub.devices));
  const mideaViews = $derived(buildMideaDevices(hub.midea.devices, hub.devices));
  const contracts = $derived(buildDeviceContracts(fans, tuyaViews, mideaViews, hub.specs, hub.rooms));

  function newAutomation() {
    editing = automationTemplate();
    sheetOpen = true;
  }

  function edit(a: Automation) {
    editing = a;
    sheetOpen = true;
  }

  function deviceName(id: string): string {
    if (!id) return "";
    const row = hub.devices.find((d) => d.id === id);
    const tuya = hub.tuya.devices.find((d) => d.id === id);
    const midea = hub.midea.devices.find((d) => d.id === id);
    return row?.name || tuya?.name || midea?.name || id;
  }

  const lookups: SummaryLookups = {
    deviceName,
    tuyaDP: (device, dp) => {
      const specs = hub.specs[device] ?? [];
      const spec = specs.find((s) => s.dp_id === dp) ?? specs.find((s) => s.code === dp);
      return spec ? dpLabel(spec) : dp;
    },
    mideaKey: (device, key) =>
      hub.midea.devices.find((d) => d.id === device)?.actions?.find((a) => a.key === key)?.label || key,
  };

  // eventsFor reconstroi o catalogo daquele dispositivo para o resumo do card.
  function eventsFor(a: Automation): EventOption[] {
    const src = a.source ?? {};
    const contract = contracts.find((c) => c.id === src.device && c.sourceType === src.type);
    if (contract) return contract.events;
    if (src.type === "tuya_dp") {
      const device = hub.tuya.devices.find((d) => d.id === src.device);
      return tuyaEventOptions(device, hub.specs[src.device ?? ""] ?? []);
    }
    if (src.type === "midea_state") {
      return mideaEventOptions(hub.midea.devices.find((d) => d.id === src.device));
    }
    return [];
  }

  async function run(id: string) {
    busyId = id;
    try {
      await runAutomation(id);
    } finally {
      busyId = "";
    }
  }
</script>

<p class="font-body-sm text-body-sm text-on-surface-variant">
  Uma automação liga um evento a uma ou mais ações. Ex.: a fechadura abriu entre 18:00 e 03:00 → ligar a luz da
  sala, ou o interruptor da parede ligou → ligar a luz. Cenas manuais aparecem como botões rápidos no Início.
</p>

<div>
  <AsyncButton label="Nova automação" variant="primary" icon="plus" onclick={newAutomation} />
</div>

<div class="flex flex-col gap-2">
  {#each hub.automations as a (a.id)}
    {@const trigger = sourceSummary(a, eventsFor(a), lookups)}
    {@const isTime = a.source?.type === "time"}
    {@const isWeekly = isTime && !!a.source?.days?.length}
    {@const isManual = !a.source?.type || a.source.type === "manual"}
    <div class="space-y-2 rounded-xl border border-outline-variant bg-surface-container-low p-3">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm font-semibold text-on-surface">{a.name || a.id}</span>
            <Badge
              text={isManual ? "Cena rápida" : isTime ? (isWeekly ? trigger : volatileLabel(a)) : scheduleLabel(a.schedule)}
              cls={isTime
                ? "border-tertiary/40 text-tertiary"
                : a.source?.type && a.source.type !== "manual" && a.schedule?.start
                  ? "border-secondary/40 text-secondary"
                  : "border-outline-variant text-on-surface-variant"}
            />
            {#if a.delay?.enabled}
              <Badge text={`${a.delay.ms ?? 0} ms entre ações`} cls="border-tertiary/40 text-tertiary" />
            {/if}
          </div>
          <p class="mt-1 text-[11px] text-on-surface-variant">
            {isManual
              ? "Cena manual"
              : isTime
                ? isWeekly
                  ? "Agendamento semanal"
                  : "Temporizador volátil (executa uma vez)"
                : `Quando ${trigger}`}
          </p>
          <p class="text-[11px] text-on-surface-variant">Então {actionsSummary(a.actions, lookups)}</p>
        </div>
        <span class="shrink-0 text-[10px] {a.enabled ? 'text-primary' : 'text-on-surface-variant'}">
          {a.enabled ? "ativa" : "inativa"}
        </span>
      </div>
      <div class="flex flex-wrap gap-2">
        <AsyncButton label="Executar" variant="primary" busy={busyId === a.id} onclick={() => run(a.id)} />
        <GhostButton label={a.enabled ? "Desativar" : "Ativar"} onclick={() => toggleAutomation(a)} />
        <GhostButton label="Editar" onclick={() => edit(a)} />
        <GhostButton label="Excluir" danger onclick={() => deleteAutomation(a.id)} />
      </div>
    </div>
  {:else}
    <p class="text-xs text-on-surface-variant">Nenhuma automação.</p>
  {/each}
</div>

{#if editing}
  <Editor automation={editing} bind:open={sheetOpen} onsave={(body) => saveAutomation(body, editing?.id || undefined)} />
{/if}
