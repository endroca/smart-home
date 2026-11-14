// Logica PURA do ventilador Ventisol: monta a lista a partir do banco + do
// manifesto e deriva o estado dos controles. Sem Svelte, sem api.

import type { Capability, DeviceRow, HubDevice, Thing } from "$lib/types";

export const SPEED_LABELS = ["Off", "Suave", "Brisa", "Ativo", "Forte", "Turbo", "Max"];

export interface FanManifestData {
  name: string;
  caps: Record<string, Capability>;
  actions: string[];
  reported: Record<string, unknown>;
  online: boolean;
}

export interface FanView extends HubDevice {
  kind: "fan";
  thing: string;
  device: string;
  firmwareName: string;
  caps: Record<string, Capability>;
  actions: string[];
  reported: Record<string, unknown>;
  source: string;
  updatedAt: number;
  pendingManifest: boolean;
}

export interface FanControls {
  on: boolean;
  speed: number;
  running: boolean;
  reverse: boolean;
  hasLight: boolean;
  hasDirection: boolean;
  hasSpeed: boolean;
}

// Um gateway e considerado online enquanto continua empurrando heartbeat.
const FRESH_MS = 180000;

export function manifestFanData(things: Thing[]): Record<string, FanManifestData> {
  const map: Record<string, FanManifestData> = {};
  for (const t of things) {
    const seenAt = t.updatedAt ? Date.parse(t.updatedAt) : 0;
    const fresh = seenAt > 0 && Date.now() - seenAt < FRESH_MS;
    const online = !!t.status?.online || fresh;
    for (const d of t.manifest?.devices ?? []) {
      const sh = t.states?.[d.id] ?? {};
      map[`${t.id}/${d.id}`] = {
        name: d.name || d.id,
        caps: d.capabilities ?? {},
        actions: d.actions ?? [],
        reported: sh.reported ?? {},
        online,
      };
    }
  }
  return map;
}

export function fanView(
  thing: string,
  device: string,
  row: Partial<DeviceRow>,
  m: Partial<FanManifestData>,
): FanView {
  const rep = m.reported ?? {};
  return {
    kind: "fan",
    id: `${thing}/${device}`,
    thing,
    device,
    name: row.name || m.name || device,
    firmwareName: row.firmware_name || m.name || "",
    caps: m.caps ?? {},
    actions: m.actions ?? [],
    reported: rep,
    source: typeof rep.source === "string" ? rep.source : "",
    updatedAt: typeof rep.updatedAt === "number" ? rep.updatedAt : 0,
    online: !!m.online,
    pendingManifest: !m.caps,
    room: row.room_id || "",
    template: row.template || "",
  };
}

// buildFans usa o banco (fonte de verdade dos sub-devices) e enriquece com o
// manifesto em memoria. O banco e o que faz o card aparecer na hora.
export function buildFans(things: Thing[], devices: DeviceRow[]): FanView[] {
  const data = manifestFanData(things);
  const out: FanView[] = [];
  const seen: Record<string, boolean> = {};
  for (const row of devices) {
    if (row.kind !== "fan") continue;
    seen[row.id] = true;
    out.push(fanView(row.owner ?? "", row.device_id ?? "", row, data[row.id] ?? {}));
  }
  for (const t of things) {
    for (const d of t.manifest?.devices ?? []) {
      const key = `${t.id}/${d.id}`;
      if (seen[key]) continue;
      seen[key] = true;
      out.push(fanView(t.id, d.id, {}, data[key] ?? {}));
    }
  }
  return out;
}

// fanControls deriva o estado dos tiles/velocidade a partir do reported.
export function fanControls(f: FanView): FanControls {
  const rep = f.reported;
  const caps = f.caps;
  const speed = Number(rep.speed) || 0;
  const on = !!rep.power;
  return {
    on,
    speed,
    running: on && speed > 0,
    reverse: rep.direction === "reverse",
    hasLight: "light" in caps,
    hasDirection: "direction" in caps,
    hasSpeed: "speed" in caps,
  };
}

// fanSpeedClass monta a classe de animacao do rotor.
export function fanSpeedClass(c: FanControls): string {
  return `fan-speed-${c.speed}${c.reverse ? " fan-reverse" : ""}`;
}
