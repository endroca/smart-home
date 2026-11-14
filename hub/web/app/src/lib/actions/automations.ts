// Acoes de automacao (executar, ativar/desativar, excluir, salvar).

import { api } from "$lib/api";
import { toast } from "$lib/stores/toast.svelte";
import { hub } from "$lib/stores/data.svelte";
import type { Automation } from "$lib/types";

export async function runAutomation(id: string): Promise<void> {
  try {
    const r = await api.post<{ actions?: number }>(`/api/automations/${api.enc(id)}/run`, {});
    toast.show(`Executada (${r.actions ?? 0} ação(ões))`);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export async function toggleAutomation(a: Automation): Promise<void> {
  try {
    await api.post(`/api/automations/${api.enc(a.id)}`, { ...a, enabled: !a.enabled });
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export async function deleteAutomation(id: string): Promise<void> {
  if (!confirm("Excluir esta automação?")) return;
  try {
    await api.del(`/api/automations/${api.enc(id)}`);
    toast.show("Automação excluída");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export async function saveAutomation(body: object, id?: string): Promise<void> {
  try {
    if (id) await api.post(`/api/automations/${api.enc(id)}`, { ...body, id });
    else await api.post("/api/automations", body);
    toast.show(id ? "Automação salva" : "Automação criada");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
    throw e;
  }
}

export function automationTemplate(): Automation {
  return {
    id: "",
    name: "Nova automação",
    enabled: true,
    source: { type: "tuya_dp" },
    schedule: undefined,
    actions: [],
  };
}
