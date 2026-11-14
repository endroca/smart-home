<script lang="ts">
  import Sheet from "$lib/ui/Sheet.svelte";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import { api } from "$lib/api";
  import { toast } from "$lib/stores/toast.svelte";
  import { thingConfigGet } from "$lib/actions/gateway";

  const COMMANDS = ["power", "light", "vel_0", "vel_1", "vel_2", "vel_3", "vel_4", "vel_5", "vel_6", "exhaust", "vent"];
  const COMMAND_LABELS: Record<string, string> = {
    power: "Power (desligar)",
    light: "Luz",
    vel_0: "Velocidade 0",
    vel_1: "Velocidade 1",
    vel_2: "Velocidade 2",
    vel_3: "Velocidade 3",
    vel_4: "Velocidade 4",
    vel_5: "Velocidade 5",
    vel_6: "Velocidade 6",
    exhaust: "Exaustar",
    vent: "Ventilar",
  };

  let { thing, device, open = $bindable(false) }: { thing: string; device: string; open?: boolean } = $props();

  let learned = $state<Set<string>>(new Set());
  let status = $state("");
  let loading = $state(false);
  let busy = $state("");
  let config = $state<unknown>(null);
  let timer: ReturnType<typeof setTimeout> | null = null;

  function learnedFrom(cfg: unknown): Set<string> {
    const set = new Set<string>();
    const fans = (cfg as { fans?: { device?: string; commands?: Record<string, unknown> }[] } | null)?.fans;
    if (Array.isArray(fans)) {
      const fan = fans.find((f) => f.device === device);
      if (fan?.commands) Object.keys(fan.commands).forEach((k) => set.add(k));
    }
    return set;
  }

  async function refresh() {
    loading = true;
    status = "";
    try {
      config = await thingConfigGet(thing);
      learned = learnedFrom(config);
    } catch {
      /* mantem o ultimo */
    } finally {
      loading = false;
    }
  }

  async function poll() {
    if (timer) clearTimeout(timer);
    let s: { active?: boolean; result?: string; command?: string; frame?: string } = {};
    try {
      const r = await api.get<{ status?: typeof s }>(`/api/things/${api.enc(thing)}/learn/status`);
      s = r.status ?? {};
    } catch {
      timer = setTimeout(poll, 900);
      return;
    }
    if (s.active) {
      timer = setTimeout(poll, 700);
      return;
    }
    busy = "";
    if (s.result === "ok") {
      status = `Aprendido: ${COMMAND_LABELS[s.command ?? ""] ?? s.command ?? ""}${s.frame ? ` • frame ${s.frame}` : ""}`;
      void refresh();
      return;
    }
    if (s.result === "cancelled") {
      status = "Cancelado.";
      return;
    }
    if (s.result === "error") {
      status = "Não captou nenhum frame. Tente novamente.";
      return;
    }
    timer = setTimeout(poll, 700);
  }

  function start(command: string) {
    busy = command;
    status = `Escutando… pressione o botão "${COMMAND_LABELS[command] ?? command}" no controle.`;
    void poll();
    // Fire and forget: no modo LAN a requisicao so responde no fim da sessao,
    // entao o polling e que conduz a UI.
    api.post(`/api/things/${api.enc(thing)}/learn`, { id: device, command }).catch((e: Error) => {
      toast.show(e.message, true);
      status = "Falha ao iniciar: " + e.message;
      busy = "";
    });
  }

  async function cancel() {
    try {
      await api.post(`/api/things/${api.enc(thing)}/learn/cancel`, {});
      status = "Cancelado.";
      busy = "";
      if (timer) clearTimeout(timer);
    } catch (e) {
      toast.show((e as Error).message, true);
    }
  }

  $effect(() => {
    if (open) void refresh();
    return () => {
      if (timer) clearTimeout(timer);
    };
  });
</script>

<Sheet title="Aprender controle" bind:open cls="max-w-md">
  <p class="font-body-sm text-body-sm text-on-surface-variant">
    Clique em <b>Aprender</b> no comando desejado e pressione o botão correspondente no controle físico. O gateway escuta
    por ~15 s.
  </p>

  {#if loading}
    <p class="py-6 text-center font-body-sm text-body-sm text-on-surface-variant">Carregando…</p>
  {:else}
    <div class="flex flex-col gap-2">
      {#each COMMANDS as cmd (cmd)}
        {@const ok = learned.has(cmd)}
        <div class="flex items-center justify-between gap-3 rounded-xl border border-outline-variant bg-surface-container-low px-3 py-2">
          <div class="min-w-0">
            <div class="truncate font-body-md text-body-md text-on-surface">{COMMAND_LABELS[cmd] ?? cmd}</div>
            <div class="font-label-sm text-label-sm {ok ? 'text-primary' : 'text-on-surface-variant'}">
              {ok ? "aprendido" : "não aprendido"}
            </div>
          </div>
          <AsyncButton
            label="Aprender"
            variant="primary"
            busy={busy === cmd}
            disabled={!!busy && busy !== cmd}
            onclick={() => start(cmd)}
          />
        </div>
      {/each}
    </div>
  {/if}

  <div class="min-h-[18px] font-body-sm text-body-sm text-on-surface-variant">{status}</div>

  {#snippet footer()}
    <div class="flex gap-2">
      <button
        type="button"
        class="press flex-1 rounded-xl border border-outline-variant py-2.5 text-xs text-on-surface-variant"
        onclick={() => cancel()}
      >
        Cancelar aprendizado
      </button>
      <button
        type="button"
        class="press flex-1 rounded-xl bg-surface-container-high py-2.5 text-xs text-on-surface-variant"
        onclick={() => (open = false)}
      >
        Fechar
      </button>
    </div>
  {/snippet}
</Sheet>
