// Acoes de nivel de gateway (thing): cadastro, descoberta, reboot, exclusao,
// diagnostico, config e OTA. Toda mutacao da feedback e recarrega o snapshot.

import { api } from "$lib/api";
import { toast } from "$lib/stores/toast.svelte";
import { hub } from "$lib/stores/data.svelte";
import type { Thing } from "$lib/types";

export async function probeThing(id: string, quiet = false): Promise<void> {
  if (!quiet) toast.show("Procurando devices na LAN…");
  try {
    await api.post(`/api/things/${api.enc(id)}/probe`, {});
    if (!quiet) toast.show("Descoberta concluída");
  } catch (e) {
    if (!quiet) toast.show((e as Error).message, true);
    else toast.show("Salvo, mas a descoberta LAN falhou: " + (e as Error).message, true);
  }
  await hub.load(true);
}

export async function saveThing(id: string, body: { name?: string; token?: string; url?: string }): Promise<void> {
  try {
    await api.post(`/api/devices/${api.enc(id)}`, body);
    toast.show("Gateway salvo");
    await probeThing(id, true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export async function addThing(id: string, url: string, token: string): Promise<boolean> {
  if (!id) {
    toast.show("Informe o id", true);
    return false;
  }
  try {
    await api.post("/api/devices", { id, kind: "thing", url, token });
    toast.show("Gateway cadastrado; descobrindo…");
    await hub.load(true);
    if (token) await probeThing(id, true);
    return true;
  } catch (e) {
    toast.show((e as Error).message, true);
    return false;
  }
}

export async function deleteThing(id: string): Promise<void> {
  if (!confirm(`Excluir ${id}? O token/URL e os devices descobertos dele serão removidos.`)) return;
  try {
    await api.del(`/api/devices/${api.enc(id)}`);
    toast.show("Excluído");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export async function rebootThing(id: string): Promise<void> {
  if (!confirm(`Reiniciar ${id}?`)) return;
  try {
    await api.post(`/api/things/${api.enc(id)}/reboot`, {});
    toast.show("Reiniciando…");
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export async function thingDiag(thing: string): Promise<string> {
  const r = await api.get<{ diag?: unknown }>(`/api/things/${api.enc(thing)}/diag`);
  return JSON.stringify(r.diag ?? {}, null, 2);
}

// extractConfig desembrulha o `config` de dentro do result do device.
export function extractConfig(v: unknown): unknown {
  if (v == null) return null;
  if (typeof v === "object") {
    const o = v as Record<string, unknown>;
    if (o.config != null) return o.config;
    if (o.result != null) return extractConfig(o.result);
  }
  return v;
}

export async function thingConfigGet(thing: string): Promise<unknown> {
  const r = await api.get<{ result?: unknown }>(`/api/things/${api.enc(thing)}/config`);
  return extractConfig(r.result);
}

export async function thingConfigSet(thing: string, config: unknown): Promise<void> {
  try {
    await api.post(`/api/things/${api.enc(thing)}/config`, { config });
    toast.show("config.set enviado");
  } catch (e) {
    toast.show((e as Error).message, true);
    throw e;
  }
}

export async function installFirmware(thing: string, file: string): Promise<void> {
  if (!file) {
    toast.show("Selecione um .bin", true);
    return;
  }
  if (!confirm(`Instalar ${file} em ${thing}?`)) return;
  try {
    await api.post(`/api/things/${api.enc(thing)}/ota`, { file });
    toast.show("OTA iniciado");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// gatewayVersion resolve a versao de firmware reportada pelo gateway.
export function gatewayVersion(t: Thing): string {
  return t.manifest?.firmware?.version || t.status?.version || "—";
}
