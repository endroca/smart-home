// Acoes da familia Tuya (efeitos). Cada mutacao da feedback (toast) e mantem o
// estado reativo coerente: o registro recarrega via hub.load(), as specs via
// hub.loadSpecsForDevices() e o estado do DP e atualizado de forma otimista.

import { api } from "$lib/api";
import { toast } from "$lib/stores/toast.svelte";
import { hub, type TuyaLast } from "$lib/stores/data.svelte";
import { rename } from "$lib/stores/rename.svelte";
import type { TuyaScanDevice, TuyaSpec } from "$lib/types";
import { buildTuyaDevices } from "./logic";

// setTuyaRoom associa o device a um comodo do hub.
export async function setTuyaRoom(id: string, roomId: string): Promise<void> {
  try {
    await api.post(`/api/devices/${api.enc(id)}`, { room_id: roomId });
    toast.show("Cômodo atualizado");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// setTuyaCloudOnly alterna o modo de leitura do device: pela nuvem (sem conexão
// local, para devices que dormem) ou LAN automática. Vale para qualquer device
// Tuya; a fechadura já vem ligada por padrão.
export async function setTuyaCloudOnly(id: string, cloudOnly: boolean): Promise<void> {
  try {
    await api.post(`/api/devices/${api.enc(id)}`, { cloud_only: cloudOnly });
    toast.show(cloudOnly ? "Lendo pela nuvem" : "Lendo pela LAN");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// tuyaControl escreve um DP. Atualiza o estado local na hora (equivalente ao
// render() antigo) para o card responder sem esperar um novo snapshot.
export async function tuyaControl(id: string, dp: string, value: unknown): Promise<void> {
  try {
    const r = (await api.tuyaControl(id, dp, value)) as { result?: { mode?: string } };
    toast.show("Tuya: " + (r?.result?.mode || "ok"));
    hub.updateTuyaState(id, { [dp]: value });
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// tuyaRefreshState rele o estado atual do device no hub.
export async function tuyaRefreshState(id: string): Promise<void> {
  try {
    const r = (await api.tuyaState(id)) as { state?: Record<string, unknown> };
    hub.updateTuyaState(id, r.state ?? {});
    toast.show("Estado atualizado");
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// refreshSpecs recarrega os data points do device (nuvem quando configurada,
// senao o cache local).
export async function refreshSpecs(id: string): Promise<void> {
  try {
    const r = (await api.tuyaSpecs(id)) as { specs?: TuyaSpec[] };
    hub.specs[id] = r.specs ?? [];
    const n = hub.specs[id].length;
    if (hub.tuya.cloud) {
      toast.show("Specs carregadas: " + n);
    } else {
      toast.show(
        n
          ? "Specs locais: " + n + " DP(s)"
          : "Sem specs. Configure a nuvem Tuya ou clique em “Ouvir” para inferir os DPs.",
      );
    }
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// tuyaToggleListen abre/fecha o listener em tempo real. Ao abrir, mostra o
// ultimo evento e agenda a releitura das specs inferidas.
export async function tuyaToggleListen(id: string, action: "start" | "stop"): Promise<void> {
  try {
    const r = (await api.tuyaListen(id, action)) as { listening?: string[] };
    hub.tuya.listening = r.listening ?? [];
    if (action === "start") {
      await tuyaShowLast(id);
      setTimeout(() => void hub.loadSpecsForDevices(), 1500);
    }
    toast.show(action === "start" ? "Ouvindo" : "Parado");
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// tuyaShowLast busca o ultimo evento cru do device e sincroniza o estado.
export async function tuyaShowLast(id: string): Promise<void> {
  try {
    const e = (await api.get(`/api/tuya/events?id=${api.enc(id)}`)) as {
      last?: TuyaLast;
      listening?: string[];
      state?: Record<string, unknown>;
    };
    if (e.last) hub.tuya.last[id] = e.last;
    if (e.listening) hub.tuya.listening = e.listening;
    if (e.state) hub.updateTuyaState(id, e.state);
  } catch (err) {
    toast.show((err as Error).message, true);
  }
}

// tuyaForget remove o device do banco (apaga a chave local).
export async function tuyaForget(id: string): Promise<void> {
  if (!confirm("Remover " + id + " do banco? A chave local será apagada.")) return;
  try {
    await api.tuyaForget(id);
    delete hub.specs[id];
    delete hub.tuya.last[id];
    toast.show("Removido");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// tuyaRegister cadastra um device descoberto na LAN.
export async function tuyaRegister(id: string): Promise<void> {
  try {
    await api.tuyaRegister(id);
    toast.show("Tuya cadastrado");
    await hub.load(true);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// tuyaScan varre a LAN em busca de devices Tuya e guarda o resultado no store.
export async function tuyaScan(): Promise<void> {
  try {
    const r = (await api.tuyaScan()) as { devices?: TuyaScanDevice[] };
    hub.tuya.scan = r.devices ?? [];
    toast.show("Encontrados " + hub.tuya.scan.length);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// renameTuya abre o modal compartilhado de renomear. O ID e o device Tuya, entao
// o nome e so de exibicao e nunca toca a identidade do aparelho.
export function renameTuya(id: string): void {
  const d = buildTuyaDevices(hub.tuya.devices, hub.specs, hub.tuya.listening, hub.devices).find(
    (x) => x.id === id,
  );
  rename.start({ kind: "tuya", id }, d ? d.name : "");
}
