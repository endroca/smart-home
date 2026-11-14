<script lang="ts">
  // Histórico de aberturas da fechadura (nuvem Tuya, /door-lock/open-logs).
  // Bloco recolhível que carrega sob demanda; o estado vive no componente, então
  // sobrevive aos re-renders do SSE sem fechar nem recarregar.
  import Icon from "$lib/ui/Icon.svelte";
  import { api } from "$lib/api";
  import { relTime } from "$lib/format";
  import type { TuyaOpenRecord } from "$lib/types";
  import { unlockCodeLabel, type LockView } from "./logic";

  let { device }: { device: LockView } = $props();

  interface HistoryData {
    available?: boolean;
    reason?: string;
    message?: string;
    total?: number;
    records?: TuyaOpenRecord[];
  }

  let open = $state(false);
  let data = $state<HistoryData | null>(null);
  let loading = $state(false);
  let error = $state("");

  async function load(): Promise<void> {
    loading = true;
    error = "";
    try {
      data = (await api.tuyaRecords(device.id)) as unknown as HistoryData;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  function toggle(): void {
    open = !open;
    if (open && !data && !loading) void load();
  }

  function recordLabel(r: TuyaOpenRecord): string {
    return r.unlock_name || unlockCodeLabel(r.code);
  }

  function fullWhen(at?: number): string {
    return at ? new Date(at).toLocaleString("pt-BR") : "";
  }

  const records = $derived(data?.records ?? []);

  function unavailableMessage(d: HistoryData): string {
    if (d.reason === "cloud_not_configured") {
      return "Configure as credenciais da nuvem Tuya nos Ajustes para ver o histórico.";
    }
    if (d.reason === "not_subscribed") {
      return "A API de histórico da Tuya (Smart Lock) não está liberada para este projeto.";
    }
    if (d.message) return "Falha ao consultar a Tuya: " + d.message;
    return "Não foi possível carregar o histórico agora.";
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
      <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary/10">
        <Icon name="clock" cls="h-4 w-4 text-primary" />
      </span>
      <span class="min-w-0">
        <span class="block font-label-md text-label-md font-medium text-on-surface">Histórico de aberturas</span>
        <span class="block truncate font-label-sm text-label-sm text-on-surface-variant">
          {open ? "Toque para recolher" : "Quem abriu, quando e como • dados da nuvem"}
        </span>
      </span>
    </span>
    <Icon name={open ? "arrow_up" : "arrow_down"} cls="h-5 w-5 text-on-surface-variant" />
  </button>

  {#if open}
    <div class="mt-space-sm rounded-2xl border border-outline-variant/40 bg-surface-container-low/60 p-3">
      {#if loading}
        <p class="py-6 text-center font-label-sm text-label-sm text-on-surface-variant">Consultando a nuvem Tuya…</p>
      {:else if error}
        <p class="py-4 font-label-sm text-label-sm leading-snug text-error">{error}</p>
      {:else if !data}
        <p class="py-6 text-center font-label-sm text-label-sm text-on-surface-variant">
          Abra o painel para carregar o histórico.
        </p>
      {:else if !data.available}
        <div class="rounded-xl bg-primary/5 p-3 font-label-sm text-label-sm leading-snug text-on-surface-variant">
          {unavailableMessage(data)}
        </div>
      {:else if !records.length}
        <div class="rounded-xl bg-surface-container-high/70 p-3 font-label-sm text-label-sm leading-snug text-on-surface-variant">
          Nenhuma abertura registrada nos últimos 30 dias.
        </div>
      {:else}
        <ul class="flex flex-col divide-y divide-outline-variant/30">
          {#each records as r, i (i)}
            <li class="flex items-center gap-3 py-2 first:pt-0 last:pb-0">
              <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-surface-container-high">
                <Icon name="lock" cls="h-4 w-4 text-on-surface-variant" />
              </span>
              <div class="min-w-0 flex-1">
                <div class="truncate font-body-md text-body-md font-medium text-on-surface">{recordLabel(r)}</div>
                <div class="truncate font-label-sm text-label-sm text-on-surface-variant">
                  {unlockCodeLabel(r.code)}{r.user_name ? " • " + r.user_name : ""}
                </div>
              </div>
              <time class="shrink-0 font-label-sm text-label-sm text-on-surface-variant" title={fullWhen(r.at)}>
                {relTime(r.at)}
              </time>
            </li>
          {/each}
        </ul>
        {#if data.total}
          <div class="mt-2 font-label-sm text-label-sm text-on-surface-variant">
            {data.total} abertura{data.total === 1 ? "" : "s"} nos últimos 30 dias
          </div>
        {/if}
      {/if}
    </div>
  {/if}
</section>
