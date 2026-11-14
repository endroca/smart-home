<script lang="ts">
  // Consumo de energia da nuvem Tuya: bloco recolhivel com os periodos
  // (hoje/7/30 dias) e um grafico de barras em HTML/CSS puro (sem biblioteca,
  // para o hub seguir funcionando offline). O estado do painel fica no proprio
  // componente: sobrevive aos re-renders do SSE sem fechar nem perder a serie.
  import Icon from "$lib/ui/Icon.svelte";
  import { api } from "$lib/api";
  import { relTime } from "$lib/format";
  import type { TuyaView } from "./logic";

  let { device }: { device: TuyaView } = $props();

  const RANGES: [string, string][] = [
    ["today", "Hoje"],
    ["7d", "7 dias"],
    ["30d", "30 dias"],
  ];

  interface EnergyPoint {
    label?: string;
    value?: number | string;
  }
  interface EnergyData {
    available?: boolean;
    total?: number | string;
    updated_at?: string;
    reason?: string;
    message?: string;
    points?: EnergyPoint[];
  }

  let open = $state(false);
  let range = $state("today");
  let data = $state<EnergyData | null>(null);
  let loading = $state(false);
  let error = $state("");
  let seq = 0;

  async function load(): Promise<void> {
    seq += 1;
    const my = seq;
    const asked = range;
    loading = true;
    error = "";
    try {
      const res = (await api.tuyaEnergy(device.id, asked)) as unknown as EnergyData;
      if (seq !== my) return; // o usuario trocou de periodo durante a consulta
      data = res;
    } catch (e) {
      if (seq === my) error = (e as Error).message;
    } finally {
      if (seq === my) loading = false;
    }
  }

  function toggle(): void {
    open = !open;
    if (open && !data && !loading) void load();
  }

  function select(next: string): void {
    if (range === next && (data || loading)) return;
    range = next;
    data = null;
    error = "";
    void load();
  }

  // fmtKwh formata um consumo em kWh no padrao pt-BR.
  function fmtKwh(v: unknown, digits?: number): string {
    const n = Number(v);
    if (!isFinite(n)) return "—";
    const d = digits == null ? (n !== 0 && Math.abs(n) < 1 ? 3 : 2) : digits;
    return n.toLocaleString("pt-BR", { minimumFractionDigits: d, maximumFractionDigits: d }) + " kWh";
  }

  function energyRangeLabel(r: string): string {
    if (r === "7d") return "Últimos 7 dias";
    if (r === "30d") return "Últimos 30 dias";
    return "Hoje";
  }

  const points = $derived(data?.points ?? []);
  const maxVal = $derived(points.reduce((m, p) => Math.max(m, Number(p.value) || 0), 0));
  const labelEvery = $derived(Math.max(1, Math.ceil(points.length / 6)));

  // bars calcula a altura (%) e o alinhamento do tooltip de cada barra.
  const bars = $derived(
    points.map((p, i) => {
      const v = Number(p.value) || 0;
      const h = maxVal > 0 ? Math.max(1, Math.round((v / maxVal) * 100)) : 0;
      const tip = `${p.label ?? ""}: ${fmtKwh(v)}`;
      // As barras das pontas alinham o balao ao seu lado para ele nao sair do plot.
      const frac = points.length > 1 ? i / (points.length - 1) : 0.5;
      const align = frac <= 0.15 ? "left-0" : frac >= 0.85 ? "right-0" : "left-1/2 -translate-x-1/2";
      return { i, h, tip, align };
    }),
  );

  // labels posiciona os rotulos sob as barras (sem espremer como o flex antigo).
  const labels = $derived(
    points
      .map((p, i) => {
        if (i !== 0 && i !== points.length - 1 && i % labelEvery !== 0) return null;
        if (i === 0) return { i, text: p.label ?? "", cls: "absolute left-0 top-0", style: "" };
        if (i === points.length - 1) return { i, text: p.label ?? "", cls: "absolute right-0 top-0", style: "" };
        const left = (((i + 0.5) / points.length) * 100).toFixed(2);
        return { i, text: p.label ?? "", cls: "absolute top-0 -translate-x-1/2", style: `left:${left}%` };
      })
      .filter((x): x is NonNullable<typeof x> => x !== null),
  );

  function unavailableMessage(d: EnergyData): string {
    if (d.reason === "cloud_not_configured") {
      return "Configure as credenciais da nuvem Tuya nos Ajustes para ver o consumo.";
    }
    if (d.reason === "not_subscribed") {
      return "A API de consumo e histórico da Tuya não está liberada para este projeto.";
    }
    if (d.message) return "Falha ao consultar a Tuya: " + d.message;
    return "Não foi possível carregar o consumo agora.";
  }
</script>

<section class="border-t border-outline-variant/40 pt-space-md">
  <button
    type="button"
    aria-expanded={open ? "true" : "false"}
    class="press flex w-full items-center justify-between gap-2 text-left"
    onclick={toggle}
  >
    <span class="flex min-w-0 items-center gap-2">
      <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-tertiary/10">
        <Icon name="bolt" cls="h-4 w-4 text-tertiary" />
      </span>
      <span class="min-w-0">
        <span class="block font-label-md text-label-md font-medium text-on-surface">Consumo de energia</span>
        <span class="block truncate font-label-sm text-label-sm text-on-surface-variant">
          {open ? "Toque para recolher" : "Hoje, 7 e 30 dias • dados da nuvem"}
        </span>
      </span>
    </span>
    <Icon name={open ? "arrow_up" : "arrow_down"} cls="h-5 w-5 text-on-surface-variant" />
  </button>

  {#if open}
    <div class="mt-space-sm rounded-2xl border border-outline-variant/40 bg-surface-container-low/60 p-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex flex-wrap gap-1.5">
          {#each RANGES as [id, label] (id)}
            {@const active = range === id}
            <button
              type="button"
              class="press shrink-0 rounded-full px-3 py-1 font-label-sm text-label-sm {active
                ? 'bg-tertiary text-on-tertiary font-bold'
                : 'border border-outline-variant/60 bg-surface-container-high/70 text-on-surface-variant'}"
              onclick={() => select(id)}
            >
              {label}
            </button>
          {/each}
        </div>
        <span class="font-label-sm text-label-sm text-on-surface-variant">kWh</span>
      </div>

      {#if loading}
        <p class="py-6 text-center font-label-sm text-label-sm text-on-surface-variant">Consultando a nuvem Tuya…</p>
      {:else if error}
        <p class="py-4 font-label-sm text-label-sm leading-snug text-error">{error}</p>
      {:else if !data}
        <p class="py-6 text-center font-label-sm text-label-sm text-on-surface-variant">
          Abra o painel para carregar o consumo.
        </p>
      {:else if !data.available}
        <div
          class="mt-3 rounded-xl bg-tertiary/5 p-3 font-label-sm text-label-sm leading-snug text-on-surface-variant"
        >
          {unavailableMessage(data)}
        </div>
      {:else}
        {@const total = Number(data.total) || 0}
        {@const when = data.updated_at ? relTime(new Date(data.updated_at).getTime()) : ""}
        <div class="mt-3 flex items-end justify-between gap-2">
          <div class="min-w-0">
            <div class="font-headline-sm text-headline-sm font-bold leading-none text-tertiary">{fmtKwh(total)}</div>
            <div class="mt-1 font-label-sm text-label-sm text-on-surface-variant">
              {energyRangeLabel(range)} • total
            </div>
          </div>
          <div class="shrink-0 text-right font-label-sm text-label-sm text-on-surface-variant">
            {#if when}<div>atualizado {when}</div>{/if}
            <div class="opacity-75">nuvem Tuya</div>
          </div>
        </div>

        {#if total <= 0}
          <div
            class="mt-3 rounded-xl bg-surface-container-high/70 p-3 font-label-sm text-label-sm leading-snug text-on-surface-variant"
          >
            Sem consumo registrado neste período. A tomada envia o consumo acumulado a cada ~30 min; períodos sem
            relato (ou anteriores ao cadastro do aparelho) aparecem zerados.
          </div>
        {:else}
          <div class="mt-3 flex h-32 items-stretch gap-[3px] rounded-xl bg-surface-container-lowest/70 px-2 pt-8">
            {#each bars as bar (bar.i)}
              <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
              <div
                class="group relative flex h-full min-w-0 flex-1 flex-col justify-end focus:outline-none"
                tabindex="0"
                aria-label={bar.tip}
              >
                <div
                  class="relative w-full rounded-t-sm bg-tertiary/80 transition-colors group-hover:bg-tertiary group-focus:bg-tertiary"
                  style="height:{bar.h}%"
                >
                  <span
                    class="pointer-events-none absolute bottom-full {bar.align} z-10 mb-1 whitespace-nowrap rounded-md bg-surface-container-highest px-2 py-1 font-label-sm text-[10px] leading-none text-on-surface opacity-0 shadow-lg ring-1 ring-outline-variant/60 transition-opacity group-hover:opacity-100 group-focus:opacity-100"
                  >
                    {bar.tip}
                  </span>
                </div>
              </div>
            {/each}
          </div>
          <div class="relative mt-1 h-3 px-2 text-[9px] leading-3 text-on-surface-variant">
            {#each labels as lb (lb.i)}
              {#if lb}
                <span class={lb.cls} style={lb.style ?? ""}>{lb.text}</span>
              {/if}
            {/each}
          </div>
        {/if}
      {/if}
    </div>
  {/if}
</section>
