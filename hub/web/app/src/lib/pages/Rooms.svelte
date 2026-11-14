<script lang="ts">
  import { hub } from "$lib/stores/data.svelte";
  import { nav, roomFilter } from "$lib/stores/nav.svelte";
  import { allDevices } from "$lib/devices/lists";
  import { addRoom, deleteRoom, saveRoom } from "$lib/actions/rooms";
  import Input from "$lib/ui/Input.svelte";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import type { HubDevice } from "$lib/types";

  let newName = $state("");
  let busy = $state(false);
  let names = $state<Record<string, string>>({});

  const devices = $derived(allDevices());
  const groups = $derived([
    { id: "", name: "Sem cômodo", devices: devices.filter((d) => !d.room) },
    ...hub.rooms.map((r) => ({ id: r.id, name: r.name, devices: devices.filter((d) => d.room === r.id) })),
  ]);

  $effect(() => {
    for (const r of hub.rooms) {
      if (names[r.id] === undefined) names[r.id] = r.name;
    }
  });

  async function create() {
    busy = true;
    try {
      if (await addRoom(newName)) newName = "";
    } finally {
      busy = false;
    }
  }

  function focusRoom(id: string) {
    roomFilter.set(id);
    nav.go("home");
  }

  function deviceSub(d: HubDevice): string {
    if (d.kind === "fan") {
      const rep = (d as { reported?: Record<string, unknown> }).reported ?? {};
      return rep.power ? `Ligado • vel. ${Number(rep.speed) || 0}` : "Desligado";
    }
    const ip = (d as { ip?: string }).ip;
    return ip || ((d as { has_key?: boolean }).has_key ? "com chave" : "sem chave");
  }

  function deviceOn(d: HubDevice): boolean {
    if (d.kind === "fan") return !!(d as { reported?: Record<string, unknown> }).reported?.power;
    const st = (d as { state?: Record<string, unknown> }).state ?? {};
    return Object.keys(st).some((k) => st[k] === true);
  }
</script>

<div class="flex items-center gap-2">
  <Input bind:value={newName} placeholder="Novo cômodo (ex.: Sala)" cls="flex-1" onenter={create} />
  <AsyncButton label="Adicionar" variant="primary" busy={busy} onclick={create} />
</div>

<div class="flex flex-col gap-space-md">
  {#each groups as g (g.id)}
    {@const editable = g.id !== ""}
    <div class="glass-card rounded-3xl bg-surface-container p-space-lg shadow-xl">
      <div class="flex items-center justify-between gap-2">
        {#if editable}
          <Input bind:value={names[g.id]} cls="flex-1 text-sm" />
        {:else}
          <h3 class="font-headline-sm text-headline-sm font-semibold text-on-surface">{g.name}</h3>
        {/if}
        {#if editable}
          <div class="flex shrink-0 gap-1">
            <GhostButton label="Ver" onclick={() => focusRoom(g.id)} />
            <GhostButton label="Salvar" onclick={() => saveRoom(g.id, names[g.id] ?? "")} />
            <GhostButton label="Excluir" danger onclick={() => deleteRoom(g.id)} />
          </div>
        {/if}
      </div>

      <div class="mt-space-md flex flex-col gap-2">
        {#if !g.devices.length}
          <p class="font-body-sm text-body-sm text-on-surface-variant">Nenhum dispositivo aqui.</p>
        {:else}
          {#each g.devices as d (d.id)}
            {@const on = deviceOn(d)}
            <button
              type="button"
              onclick={() => focusRoom(d.room)}
              class="press flex items-center gap-3 rounded-xl bg-surface-container-high px-3 py-2 text-left"
            >
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-surface-container-lowest {on ? 'text-primary' : 'text-on-surface-variant'}">
                <Icon name={d.kind === "fan" ? "fan" : d.kind === "midea" ? "snowflake" : "plug"} cls="h-5 w-5" />
              </span>
              <span class="min-w-0 flex-1">
                <span class="block truncate font-body-md text-body-md text-on-surface">{d.name || d.id}</span>
                <span class="block truncate font-label-sm text-label-sm text-on-surface-variant">{deviceSub(d)}</span>
              </span>
              {#if on}<span class="h-2.5 w-2.5 shrink-0 rounded-full bg-primary glow-dot"></span>{/if}
            </button>
          {/each}
        {/if}
      </div>
    </div>
  {/each}
</div>
