<script lang="ts">
  import { api } from "$lib/api";
  import { hub } from "$lib/stores/data.svelte";
  import { nav } from "$lib/stores/nav.svelte";
  import { allDevices } from "$lib/devices/lists";
  import { resolveTemplate } from "$lib/devices/registry";
  import { buildFans } from "$lib/devices/fan/logic";
  import { addFan, removeFan } from "$lib/devices/fan/actions";
  import {
    deleteThing,
    gatewayVersion,
    installFirmware,
    probeThing,
    rebootThing,
    thingConfigGet,
    thingConfigSet,
    thingDiag,
  } from "$lib/actions/gateway";
  import { fmtBytes, relTime } from "$lib/format";
  import { toast } from "$lib/stores/toast.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import ConfigSection from "$lib/ui/ConfigSection.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import Select from "$lib/ui/Select.svelte";
  import Sheet from "$lib/ui/Sheet.svelte";
  import JsonSheet from "$lib/ui/JsonSheet.svelte";
  import Busy from "$lib/ui/Busy.svelte";

  const detail = $derived(nav.detail);
  const device = $derived(detail?.type === "device" ? allDevices().find((d) => d.id === detail.id) : undefined);
  const thing = $derived(detail?.type === "gateway" ? hub.things.find((t) => t.id === detail.id) : undefined);

  const deviceModule = $derived(device ? resolveTemplate(device) : undefined);
  const DeviceConfig = $derived(deviceModule?.config);

  // Gateway: fans descobertos (sub-devices) e firmware.
  const gatewayFans = $derived(thing ? buildFans([thing], hub.devices) : []);
  const fwFiles = $derived(hub.firmware.files ?? []);
  const fwOptions = $derived(
    fwFiles.length
      ? fwFiles.map((f) => ({ value: f.name, label: `${f.name} — ${f.version}` }))
      : [{ value: "", label: "sem firmware" }],
  );
  let fwChoice = $state("");
  let installing = $state(false);
  let uploading = $state(false);
  let fileEl = $state<HTMLInputElement | undefined>(undefined);
  const progress = $derived(thing ? hub.otaProgress[thing.id] : undefined);

  async function onFile() {
    const file = fileEl?.files?.[0];
    if (!file) return;
    uploading = true;
    try {
      const r = await api.firmwareUpload(file);
      hub.firmware.files = r.files ?? [];
      toast.show("Firmware enviado ao hub");
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      uploading = false;
      if (fileEl) fileEl.value = "";
    }
  }

  async function deleteFirmware(name: string) {
    if (!confirm(`Remover ${name} do hub?`)) return;
    try {
      const r = await api.firmwareDelete(name);
      hub.firmware.files = r.files ?? [];
      toast.show("Firmware removido");
    } catch (e) {
      toast.show((e as Error).message, true);
    }
  }

  let diagOpen = $state(false);
  let diagBody = $state("—");
  let configOpen = $state(false);
  let configValue = $state<unknown>({});

  async function openDiag(id: string) {
    diagBody = "Carregando…";
    diagOpen = true;
    try {
      diagBody = await thingDiag(id);
    } catch (e) {
      diagBody = "Falha: " + (e as Error).message;
    }
  }

  async function openConfig(id: string) {
    try {
      configValue = await thingConfigGet(id);
      configOpen = true;
    } catch (e) {
      toast.show((e as Error).message, true);
    }
  }

  async function doInstall(id: string) {
    installing = true;
    try {
      await installFirmware(id, fwChoice || fwFiles[0]?.name || "");
    } finally {
      installing = false;
    }
  }
</script>

{#if !detail}
  <p class="py-10 text-center font-body-sm text-body-sm text-on-surface-variant">Nada selecionado.</p>
{:else if detail.type === "device"}
  {#if !device}
    <Busy label="Carregando dispositivo…" />
  {:else}
    <div class="flex items-center gap-2">
      <button
        type="button"
        aria-label="Voltar"
        class="press flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-outline-variant text-on-surface-variant hover:text-primary"
        onclick={() => nav.closeDetail()}
      >
        <Icon name="chevron_left" cls="h-5 w-5" />
      </button>
      <div class="min-w-0 flex-1">
        <h2 class="truncate font-headline-sm text-headline-sm font-bold text-on-surface">{device.name || device.id}</h2>
        <p class="truncate font-label-sm text-label-sm text-on-surface-variant">{device.kind} • {device.id}</p>
      </div>
      <span class="h-2.5 w-2.5 shrink-0 rounded-full {device.online ? 'bg-primary glow-dot' : 'bg-outline-variant'}"></span>
    </div>

    <div class="glass-card flex flex-col gap-4 rounded-3xl bg-surface-container p-space-lg shadow-xl">
      {#if DeviceConfig}
        <DeviceConfig {device} />
      {/if}
    </div>

    {#if device.kind === "fan"}
      <div class="flex justify-end">
        <GhostButton
          label="Abrir gateway"
          icon="cpu"
          onclick={() => nav.openGateway(String((device as { thing?: string }).thing ?? ""))}
        />
      </div>
    {/if}
  {/if}
{:else if detail.type === "gateway"}
  {#if !thing}
    <Busy label="Carregando gateway…" />
  {:else}
    <div class="flex items-center gap-2">
      <button
        type="button"
        aria-label="Voltar"
        class="press flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-outline-variant text-on-surface-variant hover:text-primary"
        onclick={() => nav.closeDetail()}
      >
        <Icon name="chevron_left" cls="h-5 w-5" />
      </button>
      <div class="min-w-0 flex-1">
        <h2 class="truncate font-mono font-headline-sm text-headline-sm font-bold text-on-surface">{thing.id}</h2>
        <p class="truncate font-label-sm text-label-sm text-on-surface-variant">
          gateway • fw {gatewayVersion(thing)}
        </p>
      </div>
      <span class="h-2.5 w-2.5 shrink-0 rounded-full {thing.status?.online ? 'bg-primary glow-dot' : 'bg-outline-variant'}"></span>
    </div>

    <div class="glass-card flex flex-col gap-4 rounded-3xl bg-surface-container p-space-lg shadow-xl">
      <ConfigSection title="Conexão">
        <div class="space-y-1 rounded-lg bg-surface-container-low/60 px-space-md py-space-sm font-label-sm text-label-sm text-on-surface-variant">
          <div class="flex justify-between gap-2"><span>Endereço</span><span class="truncate font-mono text-on-surface">{thing.status?.ip || "—"}</span></div>
          <div class="flex justify-between gap-2"><span>mDNS</span><span class="truncate font-mono text-on-surface">{thing.manifest?.http?.mdns || "—"}</span></div>
          <div class="flex justify-between gap-2"><span>Sinal</span><span class="font-mono text-on-surface">{thing.status?.rssi != null ? `${thing.status.rssi} dBm` : "—"}</span></div>
          <div class="flex justify-between gap-2"><span>Uptime</span><span class="font-mono text-on-surface">{thing.status?.uptime ? relTime(Math.floor(Date.now() / 1000) - thing.status.uptime) : "—"}</span></div>
        </div>
      </ConfigSection>

      <ConfigSection title={`Devices descobertos (${gatewayFans.length})`}>
        <div class="space-y-2">
          {#each gatewayFans as f (f.id)}
            <div class="flex items-center gap-2 rounded-xl border border-outline-variant bg-surface-container-low px-3 py-2">
              <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-surface-container-lowest text-primary">
                <Icon name="fan" cls="h-4 w-4" />
              </span>
              <div class="min-w-0 flex-1">
                <div class="truncate text-xs text-on-surface">{f.name || f.device}</div>
                <div class="text-[10px] text-on-surface-variant">{f.device}</div>
              </div>
              <GhostButton label="Abrir" onclick={() => nav.openDevice(f.id)} />
              <GhostButton label="Remover" danger onclick={() => removeFan(f.thing, f.device)} />
            </div>
          {:else}
            <p class="text-[11px] text-on-surface-variant">
              Nenhum device descoberto ainda. Clique em “Descobrir na LAN” — o hub lê /api/manifest.
            </p>
          {/each}
        </div>
      </ConfigSection>

      <ConfigSection title="Firmware">
        <input bind:this={fileEl} type="file" accept=".bin" class="hidden" onchange={onFile} />
        <div class="flex flex-wrap items-center gap-2">
          <Select bind:value={fwChoice} options={fwOptions} cls="min-w-[180px] flex-1" />
          <AsyncButton
            label="Instalar OTA"
            variant="primary"
            busy={installing}
            disabled={!fwFiles.length}
            onclick={() => doInstall(thing.id)}
          />
        </div>
        <div class="mt-2 flex flex-wrap gap-2">
          <AsyncButton
            label="Enviar .bin para o hub"
            icon="upload"
            busy={uploading}
            onclick={() => fileEl?.click()}
          />
        </div>

        {#if progress}
          <div class="mt-3">
            <div class="mb-1 flex items-center justify-between font-label-sm text-label-sm text-on-surface-variant">
              <span>Instalando{progress.version ? ` ${progress.version}` : ""}…</span>
              <span>{Math.round(progress.percent)}%</span>
            </div>
            <div class="h-2 w-full overflow-hidden rounded-full bg-surface-container-high">
              <div class="h-full rounded-full bg-primary transition-all" style="width:{Math.round(progress.percent)}%"></div>
            </div>
          </div>
        {/if}

        {#if fwFiles.length}
          <div class="mt-2 space-y-1">
            {#each fwFiles as f (f.name)}
              <div class="flex items-center justify-between gap-2 font-label-sm text-label-sm text-on-surface-variant">
                <span class="truncate font-mono">{f.name}</span>
                <span class="flex shrink-0 items-center gap-2">
                  {f.version}{f.size ? ` • ${fmtBytes(f.size)}` : ""}
                  <button
                    type="button"
                    aria-label={`Remover ${f.name}`}
                    class="press text-on-surface-variant hover:text-error"
                    onclick={() => deleteFirmware(f.name)}
                  >
                    <Icon name="trash" cls="h-4 w-4" />
                  </button>
                </span>
              </div>
            {/each}
          </div>
        {:else}
          <p class="mt-2 text-[11px] text-on-surface-variant">
            Nenhum .bin no hub. Envie um firmware pelo botão acima — ele fica no hub e o OTA o puxa para o gateway.
          </p>
        {/if}
      </ConfigSection>

      <ConfigSection title="Ações">
        <div class="flex flex-wrap gap-1.5">
          {#if thing.manifest?.actions?.includes("fan.add")}
            <GhostButton label="Adicionar ventilador" icon="plus" onclick={() => addFan(thing.id)} />
          {/if}
          {#if thing.manifest?.actions?.includes("config.get")}
            <GhostButton label="Config" icon="settings" onclick={() => openConfig(thing.id)} />
          {/if}
          <GhostButton label="Descobrir na LAN" icon="refresh" onclick={() => probeThing(thing.id)} />
          <GhostButton label="Reiniciar" icon="power" onclick={() => rebootThing(thing.id)} />
          <GhostButton label="Diagnóstico" icon="activity" onclick={() => openDiag(thing.id)} />
          <GhostButton label="Excluir" icon="trash" danger onclick={() => deleteThing(thing.id)} />
        </div>
      </ConfigSection>

      {#if hub.firmware.pending && Object.keys(hub.firmware.pending).length}
        <div class="flex items-center gap-2">
          <Badge text="OTA em andamento" cls="border-tertiary/40 text-tertiary" />
        </div>
      {/if}
    </div>

    <Sheet title={`Diagnóstico — ${thing.id}`} bind:open={diagOpen} cls="max-w-lg">
      <pre class="modal-scroll max-h-[50vh] overflow-auto rounded-xl bg-surface-container-lowest p-3 font-mono text-[11px] leading-relaxed text-on-surface-variant">{diagBody}</pre>
    </Sheet>

    <JsonSheet
      title={`Configuração — ${thing.id}`}
      hint="config.set substitui a configuração inteira."
      value={configValue}
      bind:open={configOpen}
      onSave={async (body) => thingConfigSet(thing.id, body)}
    />
  {/if}
{/if}
