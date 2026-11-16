// Logica pura do ar-condicionado Midea (sem Svelte, sem api, sem stores).
// Portada de web/devices/midea.js: aqui ficam os mapeamentos de rotulos, a
// construcao da view a partir do que o hub reporta e os helpers de telemetria.
// O componente so apresenta; nada aqui toca o DOM.

import { fmtNum } from "$lib/format";
import type { DeviceRow, HubDevice, MideaDevice, MideaSpec } from "$lib/types";

/* ---------- view normalizada ---------- */

// MideaView e a forma que o card consome: o HubDevice padrao (id/kind/name/
// room/online/template) mais o estado ao vivo, as funcoes suportadas e os
// dados de conexao do aparelho.
export interface MideaView extends HubDevice {
  kind: "midea";
  state: Record<string, unknown>;
  specs: MideaSpec[];
  model: string;
  ip: string;
}

// buildMideaDevices enriquece a lista do servico Midea com o registro do hub
// (room/template) e normaliza state/specs/online. O segundo parametro e o
// snapshot de /api/devices; quando presente, ele vence o que o proprio aparelho
// reporta (mesma precedencia do front antigo: stored.room_id || d.room_id).
export function buildMideaDevices(devices: MideaDevice[], rows: DeviceRow[] = []): MideaView[] {
  const byId = new Map((rows ?? []).map((r) => [r.id, r]));
  return (devices ?? []).map((d) => {
    const stored = byId.get(d.id);
    return {
      id: d.id,
      kind: "midea",
      name: d.name || stored?.name || "",
      room: stored?.room_id || d.room_id || "",
      online: !!d.online,
      template: stored?.template || d.template || "",
      sort: stored?.sort ?? 0,
      state: d.state ?? {},
      specs: d.actions ?? [],
      model: d.model || "",
      ip: d.ip || "",
    };
  });
}

/* ---------- rotulos ---------- */

export type MideaEntry<T extends string | number = string> = readonly [value: T, label: string, icon?: string];

export const MIDEA_MODES: readonly MideaEntry[] = [
  ["auto", "Auto", "sync"],
  ["cool", "Frio", "snowflake"],
  ["dry", "Secar", "droplet"],
  ["heat", "Aquecer", "twilight"],
  ["fan", "Ventilar", "air"],
];

export const MIDEA_SPEEDS: readonly MideaEntry[] = [
  ["auto", "Auto", "A"],
  ["silent", "Baixo", "1"],
  ["low", "Médio", "2"],
  ["medium", "Alto", "3"],
  ["high", "Máx", "4"],
  ["full", "Turbo", "5"],
];

export const MIDEA_BREEZE: readonly MideaEntry[] = [
  ["off", "Fora"],
  ["away", "Afaste"],
  ["mild", "Suave"],
  ["breezeless", "Sem vento"],
];

export const MIDEA_FRESH_AIR: readonly MideaEntry[] = [
  ["off", "Fora"],
  ["low", "Baixo"],
  ["medium", "Médio"],
  ["high", "Alto"],
  ["boost", "Boost"],
];

export const MIDEA_ANGLES: readonly MideaEntry<number>[] = [
  [0, "Fechada"],
  [1, "Pos 1"],
  [25, "Pos 2"],
  [50, "Pos 3"],
  [75, "Pos 4"],
  [100, "Aberta"],
];

export const MIDEA_TOGGLES: readonly MideaEntry[] = [
  ["swing_v", "Fluxo vert.", "arrow_up"],
  ["swing_h", "Fluxo horiz.", "sync"],
  ["turbo", "Boost", "bolt"],
  ["eco", "Eco", "sparkle"],
  ["display", "LED", "lightbulb"],
  ["sleep", "Sono", "moon"],
  ["purifier", "Purificador", "air"],
  ["out_silent", "Ext. silenciosa", "sensors"],
];

// mideaLabels acha o rotulo de um valor numa das listas acima (ou "" se nao
// houver correspondencia).
export function mideaLabels(list: readonly MideaEntry<string | number>[], value: unknown): string {
  const hit = list.find(([v]) => v === value);
  return hit ? hit[1] : "";
}

/* ---------- estado / specs ---------- */

// mideaHasSpec diz se o aparelho reportou suporte a uma funcao.
export function mideaHasSpec(specs: readonly MideaSpec[] | undefined, key: string): boolean {
  return (specs ?? []).some((a) => a.key === key);
}

export interface MideaTempLimits {
  min: number;
  max: number;
}

// mideaTempLimits le os limites de target_temp dos specs, com o mesmo fallback
// do front antigo (16..30).
export function mideaTempLimits(specs: readonly MideaSpec[] | undefined): MideaTempLimits {
  const spec = (specs ?? []).find((a) => a.key === "target_temp");
  const opts = (spec?.options ?? {}) as { min?: unknown; max?: unknown };
  return { min: Number(opts.min) || 16, max: Number(opts.max) || 30 };
}

// mideaTargetTemp devolve a temperatura alvo ja limitada/arredondada (default 24).
export function mideaTargetTemp(state: Record<string, unknown> | undefined, specs: readonly MideaSpec[] | undefined): number {
  const { min, max } = mideaTempLimits(specs);
  const raw = Math.round(Number((state ?? {}).target_temp) || 24);
  return Math.max(min, Math.min(max, raw));
}

/* ---------- telemetria ---------- */

export interface MideaTelemetry {
  /** temperatura ambiente (°C) ou null */
  indoor: number | null;
  /** temperatura externa (°C) ou null */
  outdoor: number | null;
  /** aparelho ligado */
  power: boolean;
}

function numOrNull(v: unknown): number | null {
  if (v === null || v === undefined || v === "") return null;
  const n = Number(v);
  return isFinite(n) ? n : null;
}

// mideaTelemetry extrai as leituras que o card mostra (ambiente/externa) e o
// estado de potencia do aparelho.
export function mideaTelemetry(state: Record<string, unknown> | undefined = {}): MideaTelemetry {
  const s = state ?? {};
  return {
    indoor: numOrNull(s.indoor_temp),
    outdoor: numOrNull(s.outdoor_temp),
    power: !!s.power,
  };
}

// mideaTelemetryStats formata a telemetria como tiles (label/valor/unidade),
// pronta para o componente Stat.
export interface MideaStat {
  key: string;
  label: string;
  value: string;
  unit: string;
  icon: string;
  accent: string;
}

export function mideaTelemetryStats(state: Record<string, unknown> | undefined): MideaStat[] {
  const t = mideaTelemetry(state);
  const out: MideaStat[] = [];
  if (t.indoor != null) {
    out.push({ key: "indoor", label: "Ambiente", value: fmtNum(t.indoor, 1, 1), unit: "°C", icon: "thermometer", accent: "secondary" });
  }
  if (t.outdoor != null) {
    out.push({ key: "outdoor", label: "Externa", value: fmtNum(t.outdoor, 1, 1), unit: "°C", icon: "cloud", accent: "tertiary" });
  }
  return out;
}

/* ---------- consumo ---------- */

export const MIDEA_ENERGY_RANGES: readonly MideaEntry[] = [
  ["run", "Ciclo atual"],
  ["total", "Acumulado"],
];

export function energyRangeLabel(range: unknown): string {
  return range === "total" ? "Acumulado" : "Ciclo atual";
}
