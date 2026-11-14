<!-- Consumo (experimental) do ar-condicionado Midea: leitura direta do contador
     do aparelho. Portado de mideaEnergySection/mideaEnergyPanel/mideaEnergyBody
     (web/devices/midea.js). O estado do painel (range/loading/erro) vive aqui,
     por instancia. -->
<script lang="ts">
  import { api } from "$lib/api";
  import { relTime } from "$lib/format";
  import { toast } from "$lib/stores/toast.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import { MIDEA_ENERGY_RANGES, energyRangeLabel, type MideaView } from "./logic";

  let { device }: { device: MideaView } = $props();

  interface MideaEnergy {
    available?: boolean;
    total?: number;
    power_now?: number;
    updated_at?: string;
  }

  let range = $state("run");
  let data = $state<MideaEnergy | null>(null);
  let loading = $state(false);
  let error = $state("");

  async function load(r: string = range) {
    range = r;
    data = null;
    loading = true;
    error = "";
    try {
      data = (await api.mideaEnergy(device.id, r)) as MideaEnergy;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
      toast.show(error, true);
    } finally {
      loading = false;
    }
  }

  function kwh(value: unknown): string {
    const n = Number(value);
    if (!isFinite(n)) return "—";
    return n.toLocaleString("pt-BR", { minimumFractionDigits: 2, maximumFractionDigits: 2 }) + " kWh";
  }

  const when = $derived(data?.updated_at ? relTime(Date.parse(data.updated_at)) : "");
  const power = $derived(
    Number(data?.power_now) > 0 ? `${Math.round(Number(data?.power_now))} W agora` : "desligado / sem leitura",
  );
</script>

<section class="rounded-2xl border border-outline-variant/40 bg-surface-container-low/40 p-space-md">
  <div class="flex items-center gap-2">
    <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-tertiary/10">
      <Icon name="bolt" cls="h-4 w-4 text-tertiary" />
    </span>
    <span class="min-w-0 flex-1">
      <span class="block font-label-md text-label-md font-medium text-on-surface">Consumo (experimental)</span>
      <span class="block font-label-sm text-label-sm text-on-surface-variant">
        Leitura direta do contador do aparelho. Em alguns modelos os valores não batem com a realidade.
      </span>
    </span>
    <button
      type="button"
      onclick={() => load()}
      class="press shrink-0 rounded-lg border border-outline-variant px-2.5 py-1.5 font-label-sm text-label-sm text-on-surface-variant"
    >
      Ler
    </button>
  </div>

  {#if data || loading || error}
    <div class="mt-space-sm rounded-2xl border border-outline-variant/40 bg-surface-container-low/60 p-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex flex-wrap gap-1.5">
          {#each MIDEA_ENERGY_RANGES as [value, label] (value)}
            {@const active = range === value}
            <button
              type="button"
              onclick={() => load(value)}
              class="press shrink-0 rounded-full px-3 py-1 font-label-sm text-label-sm {active
                ? 'bg-tertiary font-bold text-on-tertiary'
                : 'border border-outline-variant/60 bg-surface-container-high/70 text-on-surface-variant'}"
            >
              {label}
            </button>
          {/each}
        </div>
        <span class="font-label-sm text-label-sm text-on-surface-variant">kWh</span>
      </div>

      {#if loading}
        <p class="py-6 text-center font-label-sm text-label-sm text-on-surface-variant">Lendo o aparelho…</p>
      {:else if error}
        <p class="py-4 font-label-sm text-label-sm text-on-surface-variant">{error}</p>
      {:else if !data}
        <p class="py-6 text-center font-label-sm text-label-sm text-on-surface-variant">
          Abra o painel para carregar o consumo.
        </p>
      {:else if !data.available}
        <p class="py-4 font-label-sm text-label-sm text-on-surface-variant">O aparelho não informou o consumo.</p>
      {:else}
        <div class="mt-3 flex items-end justify-between gap-2">
          <div class="min-w-0">
            <div class="font-headline-sm text-headline-sm font-bold leading-none text-tertiary">{kwh(data.total)}</div>
            <div class="mt-1 font-label-sm text-label-sm text-on-surface-variant">
              {energyRangeLabel(range)} • {power}
            </div>
          </div>
          <div class="shrink-0 text-right font-label-sm text-label-sm text-on-surface-variant">
            {#if when}<div>há {when}</div>{/if}
            <div class="opacity-75">contador do aparelho</div>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</section>
