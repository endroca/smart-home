<script lang="ts">
  // Configuracao compartilhada da familia Tuya (usada por socket, switch e pelo
  // layout generico): controles avancados (opcionais), conexao, acoes e o
  // ultimo evento cru. Os layouts passam apenas a secao `advanced` que lhes
  // cabe; o resto e igual para todos.
  import type { TuyaSpec } from "$lib/types";
  import ConfigSection from "$lib/ui/ConfigSection.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import Select from "$lib/ui/Select.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import SmallSwitch from "$lib/ui/SmallSwitch.svelte";
  import { hub } from "$lib/stores/data.svelte";
  import { refreshSpecs, renameTuya, setTuyaCloudOnly, setTuyaRoom, tuyaForget, tuyaRefreshState, tuyaShowLast, tuyaToggleListen } from "./actions";
  import SpecControls from "./SpecControls.svelte";
  import type { TuyaView } from "./logic";

  let {
    device,
    advanced = [],
    advancedTitle = "Configurações do dispositivo",
  }: { device: TuyaView; advanced?: TuyaSpec[]; advancedTitle?: string } = $props();

  const listening = $derived(!!device.listening);
  const cloudOnly = $derived(!!device.cloud_only);
  const rooms = $derived(hub.rooms ?? []);
  const last = $derived(hub.tuya.last[device.id]);
  const recent = $derived(hub.tuya.recent[device.id] ?? []);
  const roomOptions = $derived([
    { value: "", label: "sem cômodo" },
    ...rooms.map((r) => ({ value: r.id, label: r.name })),
  ]);

  // Carrega o log recente ao abrir o painel, para nao depender de apertar
  // "Ouvir". Assim da para ver se uma pressao chegou (e onde houve reconexao).
  $effect(() => {
    void tuyaShowLast(device.id);
  });
</script>

{#if advanced.length}
  <ConfigSection title={advancedTitle}>
    <div class="space-y-2">
      <SpecControls {device} specs={advanced} />
    </div>
  </ConfigSection>
{/if}

<ConfigSection title="Conexão">
  <div
    class="mt-space-md flex flex-wrap items-center justify-between gap-2 rounded-lg bg-surface-container-low/60 px-space-md py-space-sm"
  >
    <div class="flex min-w-0 items-center gap-2 font-label-sm text-label-sm text-on-surface-variant">
      <Icon name="sensors" cls="h-4 w-4 text-secondary" />
      {device.ip || "—"}
    </div>
    <Select value={device.room} options={roomOptions} onchange={(v) => setTuyaRoom(device.id, v)} />
  </div>
  <div
    class="mt-2 flex items-center justify-between gap-2 rounded-lg bg-surface-container-low/60 px-space-md py-space-sm"
  >
    <div class="flex min-w-0 items-center gap-2">
      <Icon name="wifi" cls="h-4 w-4 shrink-0 text-secondary" />
      <div class="min-w-0">
        <div class="font-label-sm text-label-sm text-on-surface">Ler pela nuvem</div>
        <div class="font-label-sm text-label-sm text-on-surface-variant">
          Nuvem primeiro (push em tempo real). Se a nuvem não responder, tenta a rede local.
        </div>
      </div>
    </div>
    <SmallSwitch
      on={cloudOnly}
      label="Ler pela nuvem"
      onclick={() => setTuyaCloudOnly(device.id, !cloudOnly)}
    />
  </div>
</ConfigSection>

<ConfigSection title="Ações">
  <div class="mt-space-sm flex flex-wrap gap-1.5">
    {#if device.has_key && !cloudOnly}
      <GhostButton
        label={listening ? "Parar" : "Ouvir"}
        icon={listening ? "close" : "sensors"}
        onclick={() => tuyaToggleListen(device.id, listening ? "stop" : "start")}
      />
    {/if}
    <GhostButton label="Atualizar" icon="refresh" onclick={() => tuyaRefreshState(device.id)} />
    <GhostButton label="Renomear" icon="settings" onclick={() => renameTuya(device.id)} />
    <GhostButton label="Specs" icon="grid" onclick={() => refreshSpecs(device.id)} />
    <GhostButton label="Remover" icon="trash" danger onclick={() => tuyaForget(device.id)} />
  </div>
</ConfigSection>

{#if last}
  <ConfigSection title="Eventos recentes">
    <div
      class="mt-space-sm max-h-56 overflow-y-auto overflow-x-hidden rounded-lg bg-surface-container-lowest p-2 font-mono text-[10px] leading-relaxed text-on-surface-variant"
    >
      {#each recent as ev}
        <div class="flex gap-2">
          <span class="shrink-0 opacity-70">{ev.at ? new Date(ev.at).toLocaleTimeString() : ""}</span>
          <span class="shrink-0 {ev.kind === "status" ? "text-primary" : ""}">{ev.kind}</span>
          <span class="break-all">{JSON.stringify(ev.dps)}</span>
        </div>
      {/each}
    </div>
    <p class="mt-1 text-[10px] text-on-surface-variant">
      "status" marca (re)conexão do link LAN; "report" vem da nuvem.
    </p>
  </ConfigSection>
{/if}
