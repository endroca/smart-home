// Logica PURA dos temporizadores volateis: um gatilho de horario (type "time")
// que dispara uma vez e some. Aqui vive a traducao de "desligar este device"
// para uma acao logica, a criacao da automacao volatil e a formatacao do tempo
// restante. Sem Svelte, sem api: testavel sem DOM.

import type { Automation, AutomationAction, HubDevice, TuyaSpec } from "$lib/types";
import { tuyaOffSpecs } from "$lib/devices/tuya/logic";

// Um alvo de temporizador: o device resolvido para uma acao logica "ligar" ou
// "desligar". O hub decide o transporte ao executar.
export interface TimerTarget {
  driver: "fan" | "tuya" | "midea";
  /** id de runtime do device (fan = "gateway/fan0"; tuya/midea = id). */
  id: string;
  label: string;
  action: AutomationAction;
}

export interface TimerPreset {
  label: string;
  seconds: number;
}

/** Presets de "ligar/desligar daqui a…". */
export const TIMER_PRESETS: TimerPreset[] = [
  { label: "30 min", seconds: 1800 },
  { label: "1 h", seconds: 3600 },
  { label: "2 h", seconds: 7200 },
  { label: "4 h", seconds: 14400 },
];

// O DP que representa a chave principal (para o "desligar").
const MASTER_PREFERENCE = ["switch_1", "switch", "power", "switch_led", "relay"];

function masterOffSpec(specs: TuyaSpec[]): TuyaSpec | undefined {
  const offs = tuyaOffSpecs({ specs });
  for (const code of MASTER_PREFERENCE) {
    const hit = offs.find((s) => s.code === code);
    if (hit) return hit;
  }
  return offs[0];
}

/** offTarget traduz um device para a acao "desligar", ou null quando não há. */
export function offTarget(device: HubDevice | undefined): TimerTarget | null {
  if (!device) return null;
  const label = device.name || device.id;
  switch (device.kind) {
    case "fan": {
      const thing = String(device.thing ?? "");
      const sub = String(device.device ?? "");
      if (!thing || !sub) return null;
      // Desligar tudo: um único frame nativo (motor + luz).
      return { driver: "fan", id: device.id, label, action: { kind: "fan", thing, device_id: sub, state: { power: false } } };
    }
    case "tuya": {
      const spec = masterOffSpec((device.specs as TuyaSpec[] | undefined) ?? []);
      if (!spec) return null;
      return { driver: "tuya", id: device.id, label, action: { kind: "tuya", device: device.id, dp: spec.dp_id, value: false } };
    }
    case "midea":
      return { driver: "midea", id: device.id, label, action: { kind: "midea", device: device.id, state: { power: false } } };
    default:
      return null;
  }
}

/** onTarget traduz um device para a acao "ligar", ou null quando não há. */
export function onTarget(device: HubDevice | undefined): TimerTarget | null {
  const off = offTarget(device);
  if (!off) return null;
  if (off.driver === "fan") {
    // O controle RF não tem "ligar": ligar é escolher uma velocidade.
    return { ...off, action: { kind: "fan", thing: off.action.thing ?? "", device_id: off.action.device_id ?? "", state: { speed: 1 } } };
  }
  if (off.driver === "tuya") {
    return { ...off, action: { ...off.action, value: true } };
  }
  return { ...off, action: { kind: "midea", device: off.action.device ?? "", state: { power: true } } };
}

/** fmtDuration formata segundos como "45 s", "9 min", "1 h 30 min". */
export function fmtDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s} s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  const rem = m % 60;
  return rem ? `${h} h ${rem} min` : `${h} h`;
}

/** remainingLabel descreve quanto falta até fire_at (RFC3339). */
export function remainingLabel(fire_at?: string, now: number = Date.now()): string {
  if (!fire_at) return "";
  const ms = Date.parse(fire_at) - now;
  if (!Number.isFinite(ms)) return "";
  if (ms <= 1000) return "agora";
  return `em ${fmtDuration(ms / 1000)}`;
}

/** relativeTimer monta a volátil que liga/desliga o device daqui a N segundos. */
export function relativeTimer(
  target: TimerTarget,
  seconds: number,
  verb = "Desligar",
  name?: string,
): Automation {
  return {
    id: "",
    name: name ?? `${verb} ${target.label} daqui a ${fmtDuration(seconds)}`,
    enabled: true,
    once: true,
    source: { type: "time", in_seconds: Math.max(1, Math.round(seconds)) },
    actions: [target.action],
  };
}

/** clockTimer monta a volátil que liga/desliga o device às HH:MM. */
export function clockTimer(target: TimerTarget, at: string, verb = "Desligar", name?: string): Automation {
  return {
    id: "",
    name: name ?? `${verb} ${target.label} às ${at}`,
    enabled: true,
    once: true,
    source: { type: "time", at },
    actions: [target.action],
  };
}

/** pairTimers monta o par volátil "liga às X e desliga às Y" (duas automações). */
export function pairTimers(
  onDevice: HubDevice | undefined,
  offDevice: HubDevice | undefined,
  onAt: string,
  offAt: string,
): Automation[] {
  const out: Automation[] = [];
  const on = onTarget(onDevice);
  const off = offTarget(offDevice ?? onDevice);
  if (on) {
    out.push({
      id: "",
      name: `Ligar ${on.label} às ${onAt}`,
      enabled: true,
      once: true,
      source: { type: "time", at: onAt },
      actions: [on.action],
    });
  }
  if (off) {
    out.push({
      id: "",
      name: `Desligar ${off.label} às ${offAt}`,
      enabled: true,
      once: true,
      source: { type: "time", at: offAt },
      actions: [off.action],
    });
  }
  return out;
}

// actionTargetsDevice reporta se uma ação aponta para o device.
function actionTargetsDevice(a: AutomationAction, device: HubDevice): boolean {
  switch (a.kind) {
    case "fan":
      return device.kind === "fan" && a.thing === device.thing && a.device_id === device.device;
    case "tuya":
      return device.kind === "tuya" && a.device === device.id;
    case "midea":
      return device.kind === "midea" && a.device === device.id;
    default:
      return false;
  }
}

/** timersFor lista as voláteis de horário ativas que miram o device. */
export function timersFor(device: HubDevice | undefined, automations: Automation[]): Automation[] {
  if (!device) return [];
  return automations.filter(
    (a) =>
      a.once === true &&
      a.source?.type === "time" &&
      (a.actions ?? []).some((act) => actionTargetsDevice(act, device)),
  );
}

/** volatileLabel resume uma automação volátil para a lista ("Volátil · em 9 min"). */
export function volatileLabel(a: Automation, now: number = Date.now()): string {
  const src = a.source ?? {};
  if (src.fire_at) {
    const rem = remainingLabel(src.fire_at, now);
    return rem ? `Volátil · ${rem}` : "Volátil";
  }
  if (src.in_seconds) return `Volátil · em ${fmtDuration(src.in_seconds)}`;
  if (src.at) return `Volátil · às ${src.at}`;
  return "Volátil";
}
