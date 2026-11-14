// Salvar o rename compartilhado (o modal vive em App.svelte).

import { api } from "$lib/api";
import { toast } from "$lib/stores/toast.svelte";
import { rename } from "$lib/stores/rename.svelte";
import { hub } from "$lib/stores/data.svelte";

export async function saveRename(): Promise<void> {
  const ctx = rename.ctx;
  if (!ctx) return;
  const name = rename.name.trim();
  if (!name) {
    toast.show("Informe um nome", true);
    return;
  }
  rename.busy = true;
  try {
    if (ctx.kind === "tuya") {
      await api.post(`/api/devices/${api.enc(ctx.id ?? "")}`, { name, kind: "tuya" });
    } else if (ctx.kind === "midea") {
      await api.post(`/api/devices/${api.enc(ctx.id ?? "")}`, { name, kind: "midea" });
    } else {
      await api.post(`/api/things/${api.enc(ctx.thing ?? "")}/commands`, {
        action: "fan.rename",
        id: ctx.device,
        name,
      });
    }
    toast.show("Nome atualizado");
    rename.close();
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  } finally {
    rename.busy = false;
  }
}
