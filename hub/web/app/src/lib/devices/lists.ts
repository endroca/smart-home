// Agrega os devices de todas as origens (fans, Tuya, Midea) num unico array
// para o Inicio e o inventario de Ajustes.

import { hub } from "$lib/stores/data.svelte";
import type { HubDevice, TuyaSpec } from "$lib/types";
import { buildFans } from "./fan/logic";
import { buildMideaDevices } from "./midea/logic";
import { buildTuyaDevices, tuyaOffSpecs } from "./tuya/logic";

export function fanDevices(): HubDevice[] {
  return buildFans(hub.things, hub.devices);
}

export function tuyaViews(): HubDevice[] {
  return buildTuyaDevices(hub.tuya.devices, hub.specs, hub.tuya.listening, hub.devices);
}

export function mideaViews(): HubDevice[] {
  return buildMideaDevices(hub.midea.devices, hub.devices);
}

export function allDevices(): HubDevice[] {
  return [...fanDevices(), ...tuyaViews(), ...mideaViews()];
}

// Um passo de "desligar tudo": o que enviar para desligar um device.
export type OffStep =
  | { kind: "fan"; thing: string; device: string }
  | { kind: "tuya"; id: string; dps: string[] }
  | { kind: "midea"; id: string };

export function offPlan(devices: HubDevice[]): OffStep[] {
  const steps: OffStep[] = [];
  for (const d of devices) {
    if (d.kind === "fan") {
      if ((d as { pendingManifest?: boolean }).pendingManifest) continue;
      steps.push({ kind: "fan", thing: String(d.thing ?? ""), device: String(d.device ?? "") });
    } else if (d.kind === "midea") {
      const st = (d as { state?: Record<string, unknown> }).state ?? {};
      if (st.power) steps.push({ kind: "midea", id: d.id });
    } else if (d.kind === "tuya") {
      const dps = tuyaOffSpecs(d as { specs?: TuyaSpec[] }).map((s) => s.dp_id);
      if (dps.length) steps.push({ kind: "tuya", id: d.id, dps });
    }
  }
  return steps;
}
