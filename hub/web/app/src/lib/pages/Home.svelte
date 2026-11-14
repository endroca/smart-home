<script lang="ts">
  import { api } from "$lib/api";
  import { toast } from "$lib/stores/toast.svelte";
  import { hub } from "$lib/stores/data.svelte";
  import { nav, roomFilter } from "$lib/stores/nav.svelte";
  import { viewport } from "$lib/stores/viewport.svelte";
  import { allDevices, offPlan } from "$lib/devices/lists";
  import DeviceCard from "$lib/devices/DeviceCard.svelte";
  import { runAutomation } from "$lib/actions/automations";
  import Icon from "$lib/ui/Icon.svelte";
  import Skeleton from "$lib/ui/Skeleton.svelte";

  const devices = $derived(allDevices());
  const filtered = $derived(roomFilter.active ? devices.filter((d) => d.room === roomFilter.active) : devices);

  const counts = $derived.by(() => {
    const c: Record<string, number> = {};
    for (const d of devices) c[d.room || ""] = (c[d.room || ""] ?? 0) + 1;
    return c;
  });

  const scenes = $derived(
    hub.automations.filter((a) => a.enabled !== false && a.source && a.source.type === "manual"),
  );

  const columns = $derived.by(() => {
    const a: typeof filtered = [];
    const b: typeof filtered = [];
    filtered.forEach((d, i) => (i % 2 === 0 ? a : b).push(d));
    return [a, b] as const;
  });

  let allOff = $state({ running: false, done: 0, total: 0 });
  const allOffLabel = $derived(allOff.running ? `Desligando ${allOff.done}/${allOff.total}…` : "Desligar tudo");

  const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

  // turnOffAll desliga, um device por vez, tudo o que esta na aba ativa. Os
  // ventiladores sao espacados (~600 ms) porque o frame RF leva ~0,5 s e uma
  // rajada colide no transmissor.
  async function turnOffAll() {
    if (allOff.running) return;
    const plan = offPlan(filtered);
    if (!plan.length) {
      toast.show("Nada para desligar neste cômodo.", true);
      return;
    }
    const room = hub.rooms.find((r) => r.id === roomFilter.active);
    const n = plan.length;
    const question = room
      ? n === 1
        ? `Desligar o dispositivo de ${room.name}?`
        : `Desligar os ${n} dispositivos de ${room.name}?`
      : n === 1
        ? "Desligar o dispositivo?"
        : `Desligar todos os ${n} dispositivos?`;
    if (!confirm(question)) return;

    allOff = { running: true, done: 0, total: n };
    let ok = 0;
    let failed = 0;
    for (let i = 0; i < plan.length; i++) {
      const step = plan[i];
      try {
        if (step.kind === "fan") {
          await api.post(`/api/things/${api.enc(step.thing)}/devices/${api.enc(step.device)}/state`, {
            state: { power: false },
            force: true,
          });
          if (i + 1 < plan.length && plan[i + 1].kind === "fan") await sleep(600);
        } else if (step.kind === "midea") {
          await api.post("/api/midea/control", { id: step.id, state: { power: false } });
        } else {
          for (const dp of step.dps) await api.post("/api/tuya/control", { id: step.id, dp, value: false });
        }
        ok++;
      } catch {
        failed++;
      }
      allOff = { running: true, done: i + 1, total: n };
    }
    allOff = { running: false, done: 0, total: 0 };
    const parts: string[] = [];
    if (ok) parts.push(ok === 1 ? "1 desligado" : `${ok} desligados`);
    if (failed) parts.push(failed === 1 ? "1 falhou" : `${failed} falharam`);
    toast.show(parts.join(", ") || "Nada foi desligado", failed > 0);
    await hub.load(true);
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
    <div class="no-scrollbar flex min-w-0 flex-1 gap-space-xs overflow-x-auto py-1">
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
