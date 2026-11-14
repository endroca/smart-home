// Efeitos do ar-condicionado Midea: falam com a api, avisam via toast e
// recarregam o snapshot do hub. Portado de web/devices/midea.js (mideaSet,
// mideaRefreshState, mideaRefreshSpecs, mideaForget, mideaScan, mideaRegister,
// renameMidea). Nenhum destes toca no DOM: a reatividade do store cuida disso.

import { api } from "$lib/api";
import { hub } from "$lib/stores/data.svelte";
import { toast } from "$lib/stores/toast.svelte";

function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

// mideaSet envia um patch de estado. Atualiza o estado local na hora (feedback
// imediato) e recarrega o snapshot em seguida.
export async function mideaSet(id: string, state: Record<string, unknown>): Promise<void> {
  try {
    const r = (await api.mideaControl(id, state)) as { state?: Record<string, unknown> };
    hub.updateMideaState(id, { ...state, ...(r?.state ?? {}) });
    toast.show("Midea: " + (state.power === false ? "desligado" : "ok"));
    await hub.load();
  } catch (e) {
    toast.show(errMsg(e), true);
  }
}

export async function mideaRefreshState(id: string): Promise<void> {
  try {
    const r = (await api.mideaState(id)) as { state?: Record<string, unknown> };
    hub.updateMideaState(id, r?.state ?? {});
    toast.show("Estado atualizado");
    await hub.load();
  } catch (e) {
    toast.show(errMsg(e), true);
  }
}

export async function mideaRefreshSpecs(id: string): Promise<void> {
  try {
    const r = (await api.mideaSpecs(id)) as { actions?: unknown };
    const d = hub.midea.devices.find((x) => x.id === id);
    if (d && Array.isArray(r?.actions)) d.actions = r.actions as typeof d.actions;
    toast.show("Funções atualizadas");
    await hub.load();
  } catch (e) {
    toast.show(errMsg(e), true);
  }
}

export async function mideaForget(id: string): Promise<void> {
  if (typeof confirm === "function" && !confirm("Remover " + id + " do hub? A chave local será apagada.")) return;
  try {
    await api.mideaForget(id);
    hub.midea.devices = hub.midea.devices.filter((d) => d.id !== id);
    toast.show("Removido");
    await hub.load(true);
  } catch (e) {
    toast.show(errMsg(e), true);
  }
}

export async function mideaRegister(id: string, name?: string, ip?: string): Promise<void> {
  try {
    await api.mideaRegister(id, name, ip);
    toast.show("Midea registrado");
    await hub.load(true);
  } catch (e) {
    toast.show(errMsg(e), true);
  }
}

export async function mideaScan(ip?: string): Promise<void> {
  toast.show("Escaneando a rede…");
  try {
    const target = (ip ?? hub.midea.scanTarget ?? "").trim();
    const r = (await api.mideaScan(target || undefined)) as { devices?: typeof hub.midea.scan };
    hub.midea.scan = r?.devices ?? [];
    if (!hub.midea.scan.length) {
      toast.show(
        target
          ? `Nenhum Midea respondeu em ${target}`
          : "Nenhum Midea respondeu ao broadcast. Informe o IP para tentar diretamente.",
        true,
      );
    }
  } catch (e) {
    toast.show(errMsg(e), true);
  }
}

// renameMidea pede o novo nome e grava no registro do device (mesmo endpoint
// que o modal de rename antigo usava).
export async function renameMidea(id: string): Promise<void> {
  const d = hub.midea.devices.find((x) => x.id === id);
  const next =
    typeof prompt === "function" ? prompt("Novo nome", d?.name || "") : null;
  if (next === null || next === undefined) return;
  const name = next.trim();
  if (!name) return;
  try {
    await api.post(`/api/devices/${api.enc(id)}`, { name, kind: "midea" });
    toast.show("Nome atualizado");
    await hub.load(true);
  } catch (e) {
    toast.show(errMsg(e), true);
  }
}
