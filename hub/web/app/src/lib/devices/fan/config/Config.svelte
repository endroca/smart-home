<script lang="ts">
  import ConfigSection from "$lib/ui/ConfigSection.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Select from "$lib/ui/Select.svelte";
  import { hub } from "$lib/stores/data.svelte";
  import { relTime } from "$lib/format";
  import { probeThing } from "$lib/actions/gateway";
  import type { FanView } from "../logic";
  import { removeFan, renameFan, setFanRoom } from "../actions";
  import LearnSheet from "../LearnSheet.svelte";

  let { device }: { device: FanView } = $props();

  let learnOpen = $state(false);

  const roomOptions = $derived([
    { value: "", label: "sem cômodo" },
    ...hub.rooms.map((r) => ({ value: r.id, label: r.name })),
  ]);
  const source = $derived(
    device.source ? `${device.source} • ${relTime(device.updatedAt)}` : relTime(device.updatedAt) || "sem telemetria",
  );
  const canLearn = $derived(device.actions.includes("learn.start"));
  const canRename = $derived(device.actions.includes("fan.rename"));
  const fwName = $derived(device.firmwareName && device.firmwareName !== device.name ? device.firmwareName : "");
</script>

{#if device.pendingManifest}
  <ConfigSection title="Conexão">
    <div class="flex items-center justify-between gap-2 rounded-lg bg-surface-container-low/60 px-space-md py-space-sm">
      <span class="flex items-center gap-2 font-label-sm text-label-sm text-on-surface-variant">
        <Icon name="sensors" cls="h-4 w-4 text-tertiary" /> sem telemetria
      </span>
      <Select
        value={device.room}
        options={roomOptions}
        onchange={(v) => setFanRoom(device.thing, device.device, v)}
      />
    </div>
  </ConfigSection>

  <ConfigSection title="Ações">
    <div class="flex flex-wrap gap-1.5">
      <GhostButton label="Descobrir na LAN" icon="refresh" onclick={() => probeThing(device.thing)} />
      <GhostButton label="Remover" icon="trash" danger onclick={() => removeFan(device.thing, device.device)} />
    </div>
  </ConfigSection>
{:else}
  <ConfigSection title="Conexão">
    <div class="flex items-center justify-between gap-2 rounded-lg bg-surface-container-low/60 px-space-md py-space-sm">
      <span class="flex items-center gap-2 font-label-sm text-label-sm text-on-surface-variant">
        <Icon name="sensors" cls="h-4 w-4 text-secondary" /> {source}
      </span>
      <Select
        value={device.room}
        options={roomOptions}
        onchange={(v) => setFanRoom(device.thing, device.device, v)}
      />
    </div>
  </ConfigSection>

  <ConfigSection title="Ações">
    <div class="flex flex-wrap gap-1.5">
      {#if canLearn}
        <GhostButton label="Aprender" icon="remote" onclick={() => (learnOpen = true)} />
      {/if}
      {#if canRename}
        <GhostButton
          label="Renomear"
          icon="settings"
          onclick={() => renameFan(device.thing, device.device, device.name)}
        />
      {/if}
      <GhostButton label="Remover" icon="trash" danger onclick={() => removeFan(device.thing, device.device)} />
    </div>
  </ConfigSection>

  {#if fwName}
    <p class="font-label-sm text-label-sm text-on-surface-variant">No dispositivo: <b>{fwName}</b></p>
  {/if}

  <p class="font-label-sm text-label-sm text-on-surface-variant">Toque = envia RF • Segure = corrige o estado sem RF</p>

  <LearnSheet thing={device.thing} device={device.device} bind:open={learnOpen} />
{/if}
