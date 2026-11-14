// Acoes de comodo (criar, renomear, excluir).

import { api } from "$lib/api";
import { toast } from "$lib/stores/toast.svelte";
import { hub } from "$lib/stores/data.svelte";

export async function addRoom(name: string): Promise<boolean> {
  const trimmed = name.trim();
  if (!trimmed) {
    toast.show("Informe o nome", true);
    return false;
  }
  try {
    await api.post("/api/rooms", { id: "room-" + Date.now().toString(36), name: trimmed });
    toast.show("Cômodo criado");
    await hub.load(true);
    return true;
  } catch (e) {
    toast.show((e as Error).message, true);
    return false;
  }
}

export async function saveRoom(id: string, name: string): Promise<void> {
  const trimmed = name.trim();
  if (!trimmed) {
    toast.show("Informe o nome", true);
    return;
  }
  try {
    await api.post(`/api/rooms/${api.enc(id)}`, { name: trimmed });
    toast.show("Cômodo salvo");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

export async function deleteRoom(id: string): Promise<void> {
  if (!confirm("Excluir este cômodo? Os dispositivos ficarão sem cômodo.")) return;
  try {
    await api.del(`/api/rooms/${api.enc(id)}`);
    toast.show("Cômodo excluído");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}
