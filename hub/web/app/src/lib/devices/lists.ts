// Agrega os devices de todas as origens (fans, Tuya, Midea) num unico array
// para o Inicio e o inventario de Ajustes.

import { hub } from "$lib/stores/data.svelte";
import type { HubDevice } from "$lib/types";
import { buildFans } from "./fan/logic";
import { buildMideaDevices } from "./midea/logic";
import { buildTuyaDevices } from "./tuya/logic";

export function fanDevices(): HubDevice[] {
  return buildFans(hub.things, hub.devices);
}

export function tuyaViews(): HubDevice[] {
  return buildTuyaDevices(hub.tuya.devices, hub.specs, hub.tuya.listening, hub.devices);
}

export function mideaViews(): HubDevice[] {
  return buildMideaDevices(hub.midea.devices, hub.devices);
}

// KIND_RANK e a ordem natural por tipo, usada como desempate quando dois
// devices ainda nao foram posicionados a mao (sort igual).
const KIND_RANK: Record<string, number> = { fan: 0, tuya: 1, midea: 2 };

// sortDevices ordena os devices de um comodo pela posicao configurada no banco.
// Quem ainda nao foi ordenado (sort igual) cai no tipo e no nome — assim a lista
// fica igual a de hoje enquanto ninguem usa o modo Ordenar.
export function sortDevices(devices: HubDevice[]): HubDevice[] {
  return [...devices].sort((a, b) => {
    const sa = a.sort ?? 0;
    const sb = b.sort ?? 0;
    if (sa !== sb) return sa - sb;
    const ka = KIND_RANK[a.kind] ?? 9;
    const kb = KIND_RANK[b.kind] ?? 9;
    if (ka !== kb) return ka - kb;
    return (a.name || a.id).localeCompare(b.name || b.id);
  });
}

export function allDevices(): HubDevice[] {
  return [...fanDevices(), ...tuyaViews(), ...mideaViews()];
}

