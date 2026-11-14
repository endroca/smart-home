<script lang="ts">
  import { api } from "$lib/api";
  import { hub } from "$lib/stores/data.svelte";
  import { nav } from "$lib/stores/nav.svelte";
  import { theme, type ThemeMode } from "$lib/stores/theme.svelte";
  import { toast } from "$lib/stores/toast.svelte";
  import { addThing } from "$lib/actions/gateway";
  import { fmtUptime } from "$lib/format";
  import Icon from "$lib/ui/Icon.svelte";
  import Input from "$lib/ui/Input.svelte";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import Segmented from "$lib/ui/Segmented.svelte";
  import Badge from "$lib/ui/Badge.svelte";
  import Busy from "$lib/ui/Busy.svelte";
  import type { TuyaCloudDevice } from "$lib/types";

  type Section = "devices" | "appearance" | "hub";
  let section = $state<Section>("devices");

  const sections = [
    { value: "devices", label: "Dispositivos", icon: "grid" },
    { value: "appearance", label: "Aparência", icon: "moon" },
    { value: "hub", label: "Hub", icon: "cpu" },
  ];

  /* ---------- dispositivos ---------- */

  const gateways = $derived.by(() => {
    const map = new Map<string, { id: string; discovered: boolean; name?: string; version?: string }>();
    for (const t of hub.things) {
      map.set(t.id, { id: t.id, discovered: true, version: t.manifest?.firmware?.version || t.status?.version });
    }
    for (const d of hub.devices) {
      if (d.kind !== "thing") continue;
      const e = map.get(d.id) ?? { id: d.id, discovered: false };
      e.name = d.name;
      map.set(d.id, e);
    }
    return [...map.values()].sort((a, b) => a.id.localeCompare(b.id));
  });

  let newId = $state("");
  let newUrl = $state("");
  let newToken = $state("");
  let registering = $state(false);

  async function createGateway() {
    registering = true;
    try {
      if (await addThing(newId.trim(), newUrl.trim(), newToken.trim())) {
        newId = newUrl = newToken = "";
      }
    } finally {
      registering = false;
    }
  }

  /* ---------- Tuya ---------- */

  let scanningTuya = $state(false);
  let registeringTuya = $state("");
  let listingTuya = $state(false);
  let cloudTuya = $state<TuyaCloudDevice[]>([]);

  async function listTuyaCloud() {
    listingTuya = true;
    try {
      const r = await api.tuyaCloud();
      cloudTuya = r.devices ?? [];
      toast.show(`${cloudTuya.length} device(s) no app Tuya`);
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      listingTuya = false;
    }
  }

  async function scanTuya() {
    scanningTuya = true;
    toast.show("Escaneando a rede…");
    try {
      const r = await api.tuyaScan();
      hub.tuya.scan = r.devices ?? [];
      toast.show(`${hub.tuya.scan.length} device(s) encontrado(s)`);
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      scanningTuya = false;
    }
  }

  async function registerTuya(id: string) {
    registeringTuya = id;
    try {
      await api.tuyaRegister(id);
      toast.show("Registrado");
      cloudTuya = cloudTuya.map((d) => (d.id === id ? { ...d, registered: true } : d));
      await hub.load(true);
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      registeringTuya = "";
    }
  }

  /* ---------- Midea ---------- */

  let scanningMidea = $state(false);
  let registeringMidea = $state("");

  async function scanMidea() {
    scanningMidea = true;
    toast.show("Escaneando a rede…");
    try {
      const r = await api.mideaScan();
      hub.midea.scan = r.devices ?? [];
      toast.show(`${hub.midea.scan.length} aparelho(s) encontrado(s)`);
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      scanningMidea = false;
    }
  }

  async function registerMidea(id: string) {
    registeringMidea = id;
    try {
      await api.mideaRegister(id);
      toast.show("Registrado");
      await hub.load(true);
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      registeringMidea = "";
    }
  }

  const h = $derived(hub.health ?? {});
</script>

<Segmented options={sections} bind:value={section} />

{#if section === "devices"}
  <section class="space-y-2">
    <h4 class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Gateways RF</h4>
    <p class="font-body-sm text-body-sm text-on-surface-variant">
      O <code>id</code> é a identidade do gateway (o campo <code>thing</code> do manifesto). Toque para abrir a configuração
      do dispositivo, incluindo <b>firmware</b> e diagnóstico.
    </p>
    <div class="space-y-2">
      {#each gateways as g (g.id)}
        <button
          type="button"
          onclick={() => nav.openGateway(g.id)}
          class="press flex w-full items-center gap-3 rounded-xl border border-outline-variant bg-surface-container-low p-3 text-left"
        >
          <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-surface-container-lowest text-primary">
            <Icon name="cpu" cls="h-5 w-5" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="block truncate font-mono text-sm font-semibold text-on-surface">{g.id}</span>
            <span class="block truncate text-[10px] text-on-surface-variant">{g.name || "sem nome de exibição"} • fw {g.version || "—"}</span>
          </span>
          {#if g.discovered}
            <Badge text="descoberto" cls="border-primary/30 text-primary" />
          {:else}
            <Badge text="não descoberto" cls="border-tertiary/30 text-tertiary" />
          {/if}
          <Icon name="chevron_right" cls="h-4 w-4 shrink-0 text-on-surface-variant" />
        </button>
      {:else}
        <p class="text-xs text-on-surface-variant">Nenhum gateway descoberto ainda.</p>
      {/each}
    </div>

    <div class="space-y-2 rounded-xl border border-dashed border-outline-variant p-3">
      <p class="text-[11px] text-on-surface-variant">
        Cadastrar um gateway. O <b>id</b> e o <b>token</b> bastam: o hub descobre o endereço pelo <b>mDNS</b>. A <b>URL</b> é
        opcional (útil quando o mDNS não atravessa o roteador).
      </p>
      <div class="flex flex-wrap gap-2">
        <Input bind:value={newId} placeholder="id do gateway (ex.: smart-control-rf01)" cls="min-w-[200px] flex-1" />
        <Input bind:value={newUrl} placeholder="URL (ex.: http://192.168.1.50)" cls="min-w-[180px] flex-1" />
        <Input bind:value={newToken} placeholder="token LAN" cls="min-w-[140px] flex-1" />
        <AsyncButton label="Cadastrar" variant="primary" busy={registering} onclick={createGateway} />
      </div>
    </div>
  </section>

  {#if hub.tuya.available !== false}
    <section class="space-y-2">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h4 class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Tuya</h4>
        <div class="flex flex-wrap items-center gap-2">
          <AsyncButton label="Listar da nuvem" busy={listingTuya} onclick={listTuyaCloud} />
          <AsyncButton label="Escanear rede" busy={scanningTuya} onclick={scanTuya} />
        </div>
      </div>
      <div class="space-y-2">
        {#each hub.tuya.devices as d (d.id)}
          <button
            type="button"
            onclick={() => nav.openDevice(d.id)}
            class="press flex w-full items-center gap-3 rounded-xl border border-outline-variant bg-surface-container-low p-3 text-left"
          >
            <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-surface-container-lowest text-secondary">
              <Icon name="plug" cls="h-5 w-5" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm text-on-surface">{d.name || d.id}</span>
              <span class="block truncate text-[10px] text-on-surface-variant">{d.id} • {d.ip || "—"}</span>
            </span>
            <Icon name="chevron_right" cls="h-4 w-4 shrink-0 text-on-surface-variant" />
          </button>
        {:else}
          <p class="text-xs text-on-surface-variant">Nenhum device Tuya registrado.</p>
        {/each}
      </div>
      {#if hub.tuya.scan.length}
        <div class="space-y-2">
          <p class="text-[11px] uppercase tracking-wider text-on-surface-variant">Encontrados na rede</p>
          {#each hub.tuya.scan as d (d.id)}
            <div class="flex items-center justify-between rounded-xl border border-outline-variant bg-surface-container-low p-2">
              <div class="min-w-0">
                <div class="truncate text-xs text-on-surface">{d.name || d.id}</div>
                <div class="text-[10px] text-on-surface-variant">{d.ip || "—"} • {d.version || "—"}</div>
              </div>
              {#if d.registered}
                <span class="text-[10px] text-primary">registrado</span>
              {:else}
                <AsyncButton label="Registrar" variant="primary" busy={registeringTuya === d.id} onclick={() => registerTuya(d.id)} />
              {/if}
            </div>
          {/each}
        </div>
      {/if}
      {#if cloudTuya.length}
        <div class="space-y-2">
          <p class="text-[11px] uppercase tracking-wider text-on-surface-variant">No app Tuya (nuvem)</p>
          <p class="text-[11px] text-on-surface-variant">
            Use para devices que não aparecem na rede — fechaduras que dormem, por exemplo.
          </p>
          {#each cloudTuya as d (d.id)}
            <div class="flex items-center justify-between rounded-xl border border-outline-variant bg-surface-container-low p-2">
              <div class="min-w-0">
                <div class="truncate text-xs text-on-surface">{d.name || d.id}</div>
                <div class="text-[10px] text-on-surface-variant">
                  {d.category || "—"}{d.online ? " • online" : ""} • {d.has_key ? "tem chave" : "sem chave"}
                </div>
              </div>
              {#if d.registered}
                <span class="text-[10px] text-primary">registrado</span>
              {:else if !d.has_key}
                <span class="text-[10px] text-on-surface-variant">sem chave</span>
              {:else}
                <AsyncButton label="Registrar" variant="primary" busy={registeringTuya === d.id} onclick={() => registerTuya(d.id)} />
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </section>
  {/if}

  {#if hub.midea.available !== false}
    <section class="space-y-2">
      <div class="flex items-center justify-between gap-2">
        <h4 class="font-label-sm text-label-sm uppercase tracking-wider text-on-surface-variant">Midea</h4>
        <AsyncButton label="Escanear rede" busy={scanningMidea} onclick={scanMidea} />
      </div>
      <div class="space-y-2">
        {#each hub.midea.devices as d (d.id)}
          <button
            type="button"
            onclick={() => nav.openDevice(d.id)}
            class="press flex w-full items-center gap-3 rounded-xl border border-outline-variant bg-surface-container-low p-3 text-left"
          >
            <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-surface-container-lowest text-secondary">
              <Icon name="snowflake" cls="h-5 w-5" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm text-on-surface">{d.name || d.id}</span>
              <span class="block truncate text-[10px] text-on-surface-variant">{d.id} • {d.ip || "—"}</span>
            </span>
            <Icon name="chevron_right" cls="h-4 w-4 shrink-0 text-on-surface-variant" />
          </button>
        {:else}
          <p class="text-xs text-on-surface-variant">Nenhum ar-condicionado registrado.</p>
        {/each}
      </div>
      {#if hub.midea.scan.length}
        <div class="space-y-2">
          <p class="text-[11px] uppercase tracking-wider text-on-surface-variant">Encontrados na rede</p>
          {#each hub.midea.scan as d (d.id)}
            <div class="flex items-center justify-between rounded-xl border border-outline-variant bg-surface-container-low p-2">
              <div class="min-w-0">
                <div class="truncate text-xs text-on-surface">{d.name || d.id}</div>
                <div class="text-[10px] text-on-surface-variant">{d.ip || "—"}</div>
              </div>
              <AsyncButton label="Registrar" variant="primary" busy={registeringMidea === d.id} onclick={() => registerMidea(d.id)} />
            </div>
          {/each}
        </div>
      {/if}
    </section>
  {/if}
{:else if section === "appearance"}
  <p class="text-xs text-on-surface-variant">
    Escolha o tema da interface. <b>Sistema</b> acompanha a preferência claro/escuro do seu aparelho.
  </p>
  <div class="flex flex-col gap-2">
    {#each [["auto", "Sistema", "Segue o tema do aparelho/PWA automaticamente", "monitor"], ["light", "Claro", "Sempre com a paleta clara", "sun"], ["dark", "Escuro", "Sempre com a paleta escura", "moon"]] as [id, label, desc, ic] (id)}
      {@const active = theme.mode === id}
      <button
        type="button"
        aria-pressed={active ? "true" : "false"}
        onclick={() => theme.set(id as ThemeMode)}
        class="press flex items-center gap-3 rounded-xl border p-3 text-left transition-all {active
          ? 'border-primary/50 bg-primary/10 text-on-surface'
          : 'border-outline-variant bg-surface-container-low text-on-surface-variant'}"
      >
        <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full {active ? 'bg-primary/10' : 'bg-surface-container-high'}">
          <Icon name={ic} cls="h-5 w-5 {active ? 'text-primary' : 'text-on-surface-variant'}" />
        </span>
        <span class="min-w-0 flex-1">
          <span class="block text-sm font-semibold">{label}</span>
          <span class="block text-[11px] text-on-surface-variant">{desc}</span>
        </span>
        {#if active}<Icon name="check" cls="h-4 w-4 shrink-0 text-primary" />{/if}
      </button>
    {/each}
  </div>
{:else}
  <div class="rounded-xl border border-outline-variant bg-surface-container-low p-3">
    <div class="flex items-center gap-2">
      <span class="h-2 w-2 shrink-0 rounded-full {hub.loaded ? 'bg-primary glow-dot' : 'bg-outline-variant'}"></span>
      <span class="text-sm font-semibold text-on-surface">Hub na LAN</span>
    </div>
    <div class="mt-3 space-y-1 text-xs text-on-surface-variant">
      <div class="flex items-center justify-between gap-3"><span>Gateways</span><span class="min-w-0 truncate font-mono text-on-surface">{h.things ?? hub.things.length}</span></div>
      <div class="flex items-center justify-between gap-3"><span>Versão do hub</span><span class="min-w-0 truncate font-mono text-on-surface">{h.version || "—"}</span></div>
      <div class="flex items-center justify-between gap-3"><span>Build da UI</span><span class="min-w-0 truncate font-mono text-on-surface">{h.build || "—"}</span></div>
      {#if typeof h.uptime === "number"}
        <div class="flex items-center justify-between gap-3"><span>Tempo no ar</span><span class="min-w-0 truncate font-mono text-on-surface">{fmtUptime(h.uptime)}</span></div>
      {/if}
    </div>
  </div>
  <div class="flex justify-end">
    <GhostButton label="Atualizar estado" icon="refresh" onclick={() => hub.load(true)} />
  </div>
  {#if hub.loading && !hub.loaded}
    <Busy label="Carregando…" />
  {/if}
{/if}
