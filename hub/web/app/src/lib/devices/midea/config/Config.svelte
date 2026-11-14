<!-- Detalhe/configuracao do ar-condicionado Midea (abre pela engrenagem).
     Portado de configMidea (web/devices/midea.js): conexao + sala, consumo
     (EnergyPanel) e acoes. -->
<script lang="ts">
  import { api } from "$lib/api";
  import { hub } from "$lib/stores/data.svelte";
  import { toast } from "$lib/stores/toast.svelte";
  import ConfigSection from "$lib/ui/ConfigSection.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Select from "$lib/ui/Select.svelte";
  import EnergyPanel from "../EnergyPanel.svelte";
  import { mideaForget, mideaRefreshSpecs, mideaRefreshState, mideaSet, renameMidea } from "../actions";
  import { mideaHasSpec, type MideaView } from "../logic";

  let { device }: { device: MideaView } = $props();

  const live = $derived(hub.midea.devices.find((d) => d.id === device.id));
  const specs = $derived(live?.actions ?? device.specs ?? []);
  const ip = $derived(device.ip || live?.ip || "—");
  const model = $derived(device.model || live?.model || "Midea V3");
  const room = $derived(device.room || live?.room_id || "");

  const roomOptions = $derived([
    { value: "", label: "Sem sala" },
    ...hub.rooms.map((r) => ({ value: r.id, label: r.name })),
  ]);

  async function setRoom(roomId: string) {
    try {
      await api.post(`/api/devices/${api.enc(device.id)}`, { room_id: roomId, kind: "midea" });
      toast.show("Sala atualizada");
      await hub.load(true);
    } catch (e) {
      toast.show(e instanceof Error ? e.message : String(e), true);
    }
  }
</script>

<ConfigSection title="Conexão">
  <div class="mt-space-md flex flex-wrap items-center justify-between gap-2 rounded-lg bg-surface-container-low/60 px-space-md py-space-sm">
    <div class="flex min-w-0 items-center gap-2 font-label-sm text-label-sm text-on-surface-variant">
      <Icon name="wifi" cls="h-4 w-4 text-secondary" />
      <span class="truncate">{ip} • {model}</span>
    </div>
    <Select value={room} options={roomOptions} onchange={setRoom} cls="px-2 py-1" />
  </div>
</ConfigSection>

<ConfigSection title="Consumo">
  <EnergyPanel {device} />
</ConfigSection>

<ConfigSection title="Ações">
  <div class="mt-space-sm flex flex-wrap gap-1.5">
    <GhostButton label="Renomear" icon="edit" onclick={() => renameMidea(device.id)} />
    <GhostButton label="Atualizar estado" icon="refresh" onclick={() => mideaRefreshState(device.id)} />
    <GhostButton label="Atualizar funções" icon="grid" onclick={() => mideaRefreshSpecs(device.id)} />
    {#if mideaHasSpec(specs, "clean")}
      <GhostButton label="Autolimpeza" icon="droplet" onclick={() => mideaSet(device.id, { clean: true })} />
    {/if}
    <GhostButton label="Remover" icon="trash" danger onclick={() => mideaForget(device.id)} />
  </div>
</ConfigSection>
