<script lang="ts">
  import { api } from "$lib/api";
  import { toast } from "$lib/stores/toast.svelte";
  import { hub } from "$lib/stores/data.svelte";
  import { nav, roomFilter } from "$lib/stores/nav.svelte";
  import { viewport } from "$lib/stores/viewport.svelte";
  import { allDevices, sortDevices } from "$lib/devices/lists";
  import DeviceCard from "$lib/devices/DeviceCard.svelte";
  import { runAutomation } from "$lib/actions/automations";
  import Icon from "$lib/ui/Icon.svelte";
  import Skeleton from "$lib/ui/Skeleton.svelte";
  import type { HubDevice } from "$lib/types";

  const devices = $derived(allDevices());
  const filtered = $derived(
    roomFilter.active ? sortDevices(devices.filter((d) => d.room === roomFilter.active)) : devices,
  );

  const counts = $derived.by(() => {
    const c: Record<string, number> = {};
    for (const d of devices) c[d.room || ""] = (c[d.room || ""] ?? 0) + 1;
    return c;
  });

  // Modo Ordenar: setas ↑/↓ por card dentro do comodo ativo. A ordem e salva no
  // hub (por comodo) e vale para todos os clientes.
  let ordering = $state(false);
  let orderIds = $state<string[]>([]);
  let lastRoom = roomFilter.active;

  const byId = $derived(new Map(devices.map((d) => [d.id, d])));
  const ordered = $derived(
    orderIds.length
      ? orderIds.map((id) => byId.get(id)).filter((d): d is HubDevice => !!d)
      : filtered,
  );

  // Sai do modo Ordenar ao trocar de comodo/aba.
  $effect(() => {
    const room = roomFilter.active;
    if (room !== lastRoom) {
      lastRoom = room;
      ordering = false;
      orderIds = [];
    }
  });

  // Rola a barra de comodos ate o comodo ativo. Sem isso, um comodo salvo que
  // esteja fora da area visivel (ex.: na ponta direita) abre escondido — e o
  // usuario nao sabe em qual comodo esta.
  let roomScrollEl = $state<HTMLDivElement | null>(null);

  function revealActiveRoom() {
    const box = roomScrollEl;
    const activeId = roomFilter.active;
    if (!box || !activeId) return;
    for (const chip of box.querySelectorAll<HTMLElement>("[data-room-id]")) {
      if (chip.dataset.roomId !== activeId) continue;
      chip.scrollIntoView?.({ inline: "center", block: "nearest" });
      break;
    }
  }

  // Ao montar, ao carregar os comodos e ao trocar o comodo ativo.
  $effect(() => {
    roomFilter.active;
    hub.rooms.length;
    if (roomScrollEl) revealActiveRoom();
  });

  // A area visivel da barra estreita quando o botao "Ordenar" aparece (logo
  // apos o comodo ativo mudar). Sem re-rolar aqui, o comodo da ponta direita
  // fica cortado pelos botoes de acao.
  $effect(() => {
    const box = roomScrollEl;
    if (!box || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => revealActiveRoom());
    ro.observe(box);
    return () => ro.disconnect();
  });

  function toggleOrdering() {
    if (ordering) {
      ordering = false;
      orderIds = [];
      return;
    }
    orderIds = filtered.map((d) => d.id);
    ordering = true;
  }

  async function move(index: number, dir: -1 | 1) {
    const j = index + dir;
    if (j < 0 || j >= orderIds.length) return;
    const next = [...orderIds];
    [next[index], next[j]] = [next[j], next[index]];
    orderIds = next;
    try {
      await api.post("/api/devices/order", { ids: next });
      await hub.load(true);
    } catch (e) {
      toast.show((e as Error).message, true);
      await hub.load(true);
    }
  }

  const scenes = $derived(
    hub.automations.filter((a) => a.enabled !== false && a.source && a.source.type === "manual"),
  );

  const columns = $derived.by(() => {
    const a: typeof filtered = [];
    const b: typeof filtered = [];
    filtered.forEach((d, i) => (i % 2 === 0 ? a : b).push(d));
    return [a, b] as const;
  });

  let allOff = $state({ running: false });
  const allOffLabel = $derived(allOff.running ? "Desligando…" : "Desligar tudo");

  // turnOffAll desliga tudo o que esta na aba ativa. O hub decide o que e
  // "desligar" para cada device (funcoes marcadas "off") e espaca os comandos
  // de ventilador; aqui so disparamos e reportamos o resultado.
  async function turnOffAll() {
    if (allOff.running) return;
    const ids = filtered
      .filter((d) => !(d.kind === "fan" && (d as { pendingManifest?: boolean }).pendingManifest))
      .map((d) => d.id);
    if (!ids.length) {
      toast.show("Nada para desligar neste cômodo.", true);
      return;
    }
    const room = hub.rooms.find((r) => r.id === roomFilter.active);
    const n = ids.length;
    const question = room
      ? n === 1
        ? `Desligar o dispositivo de ${room.name}?`
        : `Desligar os ${n} dispositivos de ${room.name}?`
      : n === 1
        ? "Desligar o dispositivo?"
        : `Desligar todos os ${n} dispositivos?`;
    if (!confirm(question)) return;

    allOff = { running: true };
    try {
      const r = (await api.post("/api/off", { ids })) as {
        results?: { ok?: boolean; skipped?: boolean }[];
      };
      const results = r.results ?? [];
      const ok = results.filter((x) => x.ok).length;
      const failed = results.filter((x) => !x.ok && !x.skipped).length;
      const parts: string[] = [];
      if (ok) parts.push(ok === 1 ? "1 desligado" : `${ok} desligados`);
      if (failed) parts.push(failed === 1 ? "1 falhou" : `${failed} falharam`);
      toast.show(parts.join(", ") || "Nada foi desligado", failed > 0);
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      allOff = { running: false };
      await hub.load(true);
    }
  }
</script>

{#if scenes.length}
  <div class="-mx-1 flex gap-space-sm overflow-x-auto px-1 py-2 no-scrollbar">
    {#each scenes as a (a.id)}
      <button
        type="button"
        onclick={() => runAutomation(a.id)}
        class="press flex shrink-0 items-center gap-space-sm rounded-full border border-outline-variant/60 bg-surface-container-high/80 px-space-md py-2 font-label-md text-label-md text-on-surface transition-all"
      >
        <Icon name="sparkle" cls="h-4 w-4 text-primary" />
        {a.name || a.id}
      </button>
    {/each}
  </div>
{/if}

<div class="room-bar">
  <div class="flex items-center gap-2">
    <div class="no-scrollbar flex min-w-0 flex-1 scroll-px-1.5 gap-space-xs overflow-x-auto py-1" bind:this={roomScrollEl}>
      <button
        type="button"
        onclick={() => roomFilter.set("")}
        class="press flex shrink-0 items-center gap-2 rounded-full px-4 py-2.5 font-label-md text-label-md {roomFilter.active === ''
          ? 'bg-primary text-on-primary'
          : 'border border-outline-variant/50 bg-surface-container-high/80 text-on-surface-variant hover:text-on-surface'}"
      >
        <span>Todos</span>
        <span class="rounded-full px-1.5 py-0.5 font-label-sm text-label-sm font-bold {roomFilter.active === '' ? 'bg-on-primary/20 text-on-primary' : 'bg-surface-container-highest text-on-surface'}">{devices.length}</span>
      </button>
      {#each hub.rooms as r (r.id)}
        {@const active = r.id === roomFilter.active}
        <button
          type="button"
          data-room-id={r.id}
          onclick={() => roomFilter.set(r.id)}
          class="press flex shrink-0 items-center gap-2 rounded-full px-4 py-2.5 font-label-md text-label-md {active
            ? 'bg-primary text-on-primary'
            : 'border border-outline-variant/50 bg-surface-container-high/80 text-on-surface-variant hover:text-on-surface'}"
        >
          <span>{r.name}</span>
          <span class="rounded-full px-1.5 py-0.5 font-label-sm text-label-sm font-bold {active ? 'bg-on-primary/20 text-on-primary' : 'bg-surface-container-highest text-on-surface'}">{counts[r.id] ?? 0}</span>
        </button>
      {/each}
    </div>
    {#if roomFilter.active && filtered.length > 1}
      <button
        type="button"
        aria-pressed={ordering ? "true" : "false"}
        title={ordering ? "Concluir a ordenação" : "Ordenar os dispositivos deste cômodo"}
        onclick={toggleOrdering}
        class="press flex h-10 shrink-0 items-center gap-1.5 rounded-xl px-3 font-label-md text-label-md {ordering
          ? 'bg-primary text-on-primary'
          : 'border border-outline-variant text-on-surface-variant hover:text-primary'}"
      >
        <Icon name={ordering ? "check" : "swap_vert"} cls="h-5 w-5" />
        <span class="hidden sm:inline">{ordering ? "Concluir" : "Ordenar"}</span>
      </button>
    {/if}
    <button
      type="button"
      aria-label={allOffLabel}
      title={allOffLabel}
      disabled={allOff.running}
      aria-busy={allOff.running ? "true" : "false"}
      onclick={turnOffAll}
      class="all-off-btn press {allOff.running ? 'opacity-60' : ''}"
    >
      {#if allOff.running}
        <span class="inline-block h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"></span>
      {:else}
        <Icon name="power" cls="h-5 w-5" />
      {/if}
    </button>
  </div>
</div>

{#if !hub.loaded && hub.loading}
  <div class="flex flex-col gap-space-lg">
    <div class="glass-card rounded-3xl p-space-lg">
      <Skeleton cls="h-6 w-40" />
      <Skeleton cls="mt-4 h-20 w-full" />
    </div>
    <div class="glass-card rounded-3xl p-space-lg">
      <Skeleton cls="h-6 w-32" />
      <Skeleton cls="mt-4 h-20 w-full" />
    </div>
  </div>
{:else if !filtered.length}
  <p class="py-10 text-center font-body-md text-body-md text-on-surface-variant">
    Nada neste cômodo ainda. Cadastre dispositivos em Ajustes.
  </p>
{:else if ordering}
  <div class="flex flex-col gap-space-lg">
    {#each ordered as d, i (d.id)}
      <div class="flex items-stretch gap-2">
        <div class="min-w-0 flex-1">
          <DeviceCard device={d} />
        </div>
        <div class="flex flex-col justify-center gap-1.5">
          <button
            type="button"
            aria-label="Mover para cima"
            title="Mover para cima"
            disabled={i === 0}
            onclick={() => move(i, -1)}
            class="press flex h-9 w-9 items-center justify-center rounded-xl border border-outline-variant text-on-surface-variant hover:text-primary disabled:opacity-30"
          >
            <Icon name="arrow_up" cls="h-5 w-5" />
          </button>
          <button
            type="button"
            aria-label="Mover para baixo"
            title="Mover para baixo"
            disabled={i === ordered.length - 1}
            onclick={() => move(i, 1)}
            class="press flex h-9 w-9 items-center justify-center rounded-xl border border-outline-variant text-on-surface-variant hover:text-primary disabled:opacity-30"
          >
            <Icon name="arrow_down" cls="h-5 w-5" />
          </button>
        </div>
      </div>
    {/each}
  </div>
{:else if viewport.desktop}
  <div class="grid grid-cols-2 items-start gap-space-lg">
    {#each columns as col}
      <div class="flex min-w-0 flex-col gap-space-lg">
        {#each col as d (d.id)}
          <DeviceCard device={d} onOpenConfig={(id) => nav.openDevice(id)} />
        {/each}
      </div>
    {/each}
  </div>
{:else}
  <div class="flex flex-col gap-space-lg">
    {#each filtered as d (d.id)}
      <DeviceCard device={d} onOpenConfig={(id) => nav.openDevice(id)} />
    {/each}
  </div>
{/if}
