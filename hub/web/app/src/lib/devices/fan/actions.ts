// Acoes do ventilador (efeitos). Tap envia RF; hold corrige o estado sem RF.

import { api } from "$lib/api";
import { toast } from "$lib/stores/toast.svelte";
import { hub } from "$lib/stores/data.svelte";
import { rename } from "$lib/stores/rename.svelte";
import { buildFans } from "./logic";

export async function setFan(thing: string, device: string, partial: Record<string, unknown>): Promise<void> {
  try {
    // force:true reenvia o RF mesmo quando o hub acha que o estado ja esta certo.
    await api.post(`/api/things/${api.enc(thing)}/devices/${api.enc(device)}/state`, { state: partial, force: true });
    toast.show("Comando enviado");
  } catch (e) {
    toast.show((e as Error).message, true);
  } finally {
    await hub.load();
  }
}

// fanCorrect atualiza o estado conhecido do hub sem transmitir RF (long-press).
export async function fanCorrect(thing: string, device: string, state: Record<string, unknown>): Promise<void> {
  try {
    await api.post(`/api/things/${api.enc(thing)}/devices/${api.enc(device)}/correct`, { state });
    toast.show("Estado corrigido (sem RF)");
  } catch (e) {
    toast.show((e as Error).message, true);
  } finally {
    await hub.load();
  }
}

export async function setFanRoom(thing: string, device: string, roomId: string): Promise<void> {
  try {
    await api.post(`/api/devices/${api.enc(`${thing}/${device}`)}`, { room_id: roomId });
    toast.show("Cômodo atualizado");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export function renameFan(thing: string, device: string, name: string): void {
  rename.start({ kind: "fan", thing, device }, name);
}

export async function addFan(thing: string): Promise<void> {
  try {
    await api.post(`/api/things/${api.enc(thing)}/commands`, { action: "fan.add" });
    toast.show("Ventilador adicionado");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export async function removeFan(thing: string, device: string): Promise<void> {
  if (!confirm(`Remover ${device}? Os slots seguintes serão reindexados.`)) return;
  try {
    await api.post(`/api/things/${api.enc(thing)}/commands`, { action: "fan.remove", id: device });
    toast.show("Removido");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// fanName resolve o nome atual de um fan (para abrir o rename com o valor certo).
export function fanName(thing: string, device: string): string {
  const f = buildFans(hub.things, hub.devices).find((x) => x.thing === thing && x.device === device);
  return f ? f.name : "";
}
