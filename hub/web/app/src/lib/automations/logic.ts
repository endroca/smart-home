// Logica PURA da tela de automacoes: catalogo de gatilhos/acoes, conversao
// entre o JSON salvo e o formulario amigavel, rotulos e validacao. O componente
// so apresenta; os testes cobrem este arquivo sem DOM.

import { fmtValue } from "$lib/format";
import { fmtDuration } from "./timer";
import type {
  Automation,
  AutomationAction,
  AutomationSchedule,
  AutomationSource,
  MideaSpec,
  Room,
  TuyaDevice,
  TuyaSpec,
} from "$lib/types";
import { UNLOCK_CODES } from "$lib/devices/lock/logic";
import {
  MIDEA_ANGLES,
  MIDEA_BREEZE,
  MIDEA_FRESH_AIR,
  MIDEA_MODES,
  MIDEA_SPEEDS,
  mideaLabels,
} from "$lib/devices/midea/logic";
import { dpDesc, dpLabel, TELEMETRY, TUYA_SWITCH_EXCLUDE } from "$lib/devices/tuya/logic";
import type { FanView } from "$lib/devices/fan/logic";
import type { MideaEntry, MideaView } from "$lib/devices/midea/logic";
import type { TuyaView } from "$lib/devices/tuya/logic";

export type TriggerKind = "manual" | "tuya_dp" | "midea_state" | "thing_state" | "time";
export type ControlKind = "bool" | "enum" | "number" | "text";
export type DeviceDriver = "fan" | "tuya" | "midea";

export interface Choice {
  value: string;
  label: string;
}

export interface EventOption extends Choice {
  kind: ControlKind | "event";
  dps?: string[];
  values?: Choice[];
  min?: number;
  max?: number;
  step?: number;
  numeric?: boolean;
  hint?: string;
}

export interface ControlOption extends Choice {
  kind: ControlKind;
  values?: Choice[];
  min?: number;
  max?: number;
  step?: number;
  /** Enum cujos valores devem ser enviados como numero (ex.: velocidade). */
  numeric?: boolean;
  /** Valor inicial ao escolher esta acao. */
  defaultValue?: string;
}

// DeviceContract e a visao unica das tres familias (ventilador, Tuya e Midea)
// para a UI de automacoes: todo device expoe as mesmas listas de eventos e
// acoes, e a UI nao precisa saber a diferenca antes de o operador escolher.
export interface DeviceContract {
  key: string;
  id: string;
  /** Driver de transporte (fan/tuya/midea). Nao e uma categoria da UI. */
  driver: DeviceDriver;
  sourceType: Exclude<TriggerKind, "manual">;
  name: string;
  room: string;
  online: boolean;
  events: EventOption[];
  actions: ControlOption[];
}

export function contractKey(driver: DeviceDriver, id: string): string {
  return `${driver}:${id}`;
}

export interface ActionDraft {
  kind: "fan" | "tuya" | "midea";
  target: string;
  control: string;
  controlKind: ControlKind;
  value: string;
}

export interface AutomationDraft {
  name: string;
  enabled: boolean;
  triggerKind: TriggerKind;
  triggerDevice: string;
  triggerEvent: string;
  customDP: string;
  condition: "any" | "true" | "false" | "value";
  value: string;
  /** Gatilho de horário: "relative" (daqui a…) ou "clock" (às HH:MM). */
  timeMode: "relative" | "clock";
  timeSeconds: string;
  timeUnit: "minutes" | "hours";
  timeAt: string;
  /** Dias do gatilho semanal (0=Dom..6=Sáb). Vazio = disparo único. */
  timeDays: number[];
  /** Volátil: dispara uma vez e some. No horário, só quando não é semanal. */
  once: boolean;
  scheduleEnabled: boolean;
  scheduleStart: string;
  scheduleEnd: string;
  scheduleDays: number[];
  delayEnabled: boolean;
  delayMs: string;
  actions: ActionDraft[];
  /** Acoes de formatos desconhecidos: preservadas sem edicao amigavel. */
  extraActions: AutomationAction[];
  /** Campos desconhecidos da fonte, preservados ao salvar. */
  baseSource: AutomationSource;
}

export const ALL_DAYS = [1, 2, 3, 4, 5, 6, 0];
export const DAY_OPTIONS = [
  { value: 1, label: "Seg" },
  { value: 2, label: "Ter" },
  { value: 3, label: "Qua" },
  { value: 4, label: "Qui" },
  { value: 5, label: "Sex" },
  { value: 6, label: "Sáb" },
  { value: 0, label: "Dom" },
];
const DAY_SHORT = ["Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"];

export const FAN_STATE_LABELS: Record<string, string> = {
  power: "Ventilador",
  light: "Luz",
  speed: "Velocidade",
  direction: "Direção",
};

const MIDEA_TELEMETRY = ["indoor_temp", "outdoor_temp", "humidity"];
const MIDEA_VALUE_LISTS: Record<string, readonly MideaEntry<string | number>[]> = {
  mode: MIDEA_MODES,
  fan_speed: MIDEA_SPEEDS,
  breeze: MIDEA_BREEZE,
  fresh_air: MIDEA_FRESH_AIR,
  airflow_angle: MIDEA_ANGLES,
};

// enumValues normaliza `values`/`options` (lista crua ou objeto {range:[...]}).
export function enumValues(v: unknown): string[] {
  let raw = v;
  if (raw && !Array.isArray(raw) && Array.isArray((raw as { range?: unknown }).range)) {
    raw = (raw as { range: unknown[] }).range;
  }
  return Array.isArray(raw) ? raw.map((x) => String(x)) : [];
}

// numberMeta extrai min/max/step de `values`/`options`.
export function numberMeta(v: unknown): { min?: number; max?: number; step?: number } {
  const meta = (v as { min?: number; max?: number; step?: number } | null) ?? {};
  return { min: meta.min, max: meta.max, step: meta.step };
}

function roomName(rooms: Room[], id?: string): string {
  if (!id) return "";
  return rooms.find((r) => r.id === id)?.name ?? "";
}

/* ---------- catalogo de gatilhos ---------- */

const TELEMETRY_CODES = new Set(TELEMETRY.flatMap((t) => t.codes));
// DPs de configuracao do aparelho que nao costumam ser eventos de automacao.
const TRIGGER_IGNORE = new Set([
  ...TUYA_SWITCH_EXCLUDE,
  "relay_status",
  "power_off",
  "light_mode",
  "cycle_time",
  "random_time",
  "countdown",
  "countdown_1",
]);

function tuyaControl(spec: TuyaSpec): ControlOption {
  if (spec.type === "Boolean") {
    return { value: spec.dp_id, label: dpLabel(spec), kind: "bool" };
  }
  if (spec.type === "Enum") {
    const values = enumValues(spec.values).map((v) => ({ value: v, label: v }));
    return { value: spec.dp_id, label: dpLabel(spec), kind: "enum", values };
  }
  if (spec.type === "Integer" || spec.type === "Number") {
    return { value: spec.dp_id, label: dpLabel(spec), kind: "number", ...numberMeta(spec.values) };
  }
  return { value: spec.dp_id, label: dpLabel(spec), kind: "text" };
}

function specToEvent(spec: TuyaSpec): EventOption {
  const base = tuyaControl(spec);
  return {
    value: base.value,
    label: base.label,
    kind: base.kind,
    values: base.values,
    min: base.min,
    max: base.max,
    step: base.step,
    numeric: base.numeric,
    hint: dpDesc(spec),
  };
}

/** Eventos uteis de um device Tuya. Numa fechadura, agrupa os metodos de abertura. */
export function tuyaEventOptions(_device: TuyaDevice | undefined, specs: TuyaSpec[] = []): EventOption[] {
  const out: EventOption[] = [];
  const seen = new Set<string>();
  const add = (opt: EventOption) => {
    if (!opt.value || seen.has(opt.value)) return;
    seen.add(opt.value);
    out.push(opt);
  };

  const unlockSpecs = specs.filter((s) => s.dp_id && UNLOCK_CODES.includes(s.code));
  if (unlockSpecs.length > 0) {
    add({
      value: "__unlock__",
      label: "Abertura (qualquer método)",
      kind: "event",
      dps: unlockSpecs.map((s) => s.dp_id),
      hint: "Dispara com digital, senha, cartão, app ou senha temporária.",
    });
  }

  for (const spec of unlockSpecs) {
    if (!spec.dp_id) continue;
    add({
      value: spec.dp_id,
      label: `Abertura com ${dpLabel(spec).toLowerCase()}`,
      kind: "event",
      hint: "Qualquer abertura por este método.",
    });
  }

  for (const spec of specs) {
    if (!spec.dp_id || TELEMETRY_CODES.has(spec.code) || UNLOCK_CODES.includes(spec.code) || TRIGGER_IGNORE.has(spec.code)) continue;
    if (spec.code === "alarm_lock") {
      const values = enumValues(spec.values);
      add({
        value: spec.dp_id,
        label: "Alarme",
        kind: values.length ? "enum" : "text",
        values: values.map((v) => ({ value: v, label: v })),
        hint: dpDesc(spec),
      });
      continue;
    }
    add(specToEvent(spec));
  }

  add({ value: "__custom__", label: "Outro DP…", kind: "text" });
  return out;
}

function mideaValues(key: string, raw: unknown): Choice[] {
  const values = enumValues(raw);
  const list = MIDEA_VALUE_LISTS[key];
  return values.map((v) => {
    const numeric = key === "airflow_angle" ? Number(v) : v;
    const label = list ? mideaLabels(list, numeric) : "";
    return { value: v, label: label || v };
  });
}

function mideaControl(spec: MideaSpec): ControlOption {
  if (spec.type === "boolean") {
    return { value: spec.key, label: spec.label || spec.key, kind: "bool" };
  }
  if (spec.type === "enum") {
    return {
      value: spec.key,
      label: spec.label || spec.key,
      kind: "enum",
      numeric: spec.key === "airflow_angle",
      values: mideaValues(spec.key, spec.options),
    };
  }
  if (spec.type === "integer" || spec.type === "number") {
    return {
      value: spec.key,
      label: spec.label || spec.key,
      kind: "number",
      ...numberMeta(spec.options),
    };
  }
  return { value: spec.key, label: spec.label || spec.key, kind: "text" };
}

/** Eventos uteis de um ar-condicionado (potencia, modo, temperatura, ...). */
export function mideaEventOptions(device: { actions?: MideaSpec[]; specs?: MideaSpec[] } | undefined): EventOption[] {
  const out: EventOption[] = [];
  for (const spec of device?.specs ?? device?.actions ?? []) {
    if (!spec.key || MIDEA_TELEMETRY.includes(spec.key) || spec.writable === false) continue;
    const base = mideaControl(spec);
    out.push({
      value: base.value,
      label: base.label,
      kind: base.kind,
      values: base.values,
      min: base.min,
      max: base.max,
      step: base.step,
      numeric: base.numeric,
    });
  }
  out.push({ value: "__custom__", label: "Outro estado…", kind: "text" });
  return out;
}

export function conditionChoices(event?: EventOption): Choice[] {
  if (!event) return [{ value: "any", label: "Qualquer mudança" }];
  if (event.kind === "event") return [{ value: "any", label: "Qualquer abertura" }];
  if (event.kind === "bool") {
    return [
      { value: "any", label: "Qualquer mudança" },
      { value: "true", label: "Ligar" },
      { value: "false", label: "Desligar" },
    ];
  }
  return [
    { value: "any", label: "Qualquer mudança" },
    { value: "value", label: "Mudou para…" },
  ];
}

/* ---------- catalogo de acoes ---------- */

const FAN_SPEEDS: Choice[] = ["0", "1", "2", "3", "4", "5", "6"].map((v, i) => ({
  value: v,
  label: ["Off", "Suave", "Brisa", "Ativo", "Forte", "Turbo", "Max"][i],
}));

const FAN_DIRECTIONS: Choice[] = [
  { value: "forward", label: "Normal" },
  { value: "reverse", label: "Reverso" },
];

export function fanEventOptions(fan: FanView | undefined): ControlOption[] {
  const out: ControlOption[] = [];
  for (const [key, cap] of Object.entries(fan?.caps ?? {})) {
    if (cap.writable === false) continue;
    const label = FAN_STATE_LABELS[key] ?? key;
    if (cap.kind === "boolean" || cap.kind === "switch") {
      out.push({ value: key, label, kind: "bool" });
    } else if (cap.kind === "enum") {
      const values =
        key === "speed"
          ? FAN_SPEEDS
          : key === "direction"
            ? FAN_DIRECTIONS
            : enumValues(cap.values).map((v) => ({ value: v, label: v }));
      out.push({ value: key, label, kind: "enum", numeric: key === "speed", values });
    } else if (cap.kind === "number" || cap.kind === "integer") {
      out.push({ value: key, label, kind: "number", min: cap.min, max: cap.max, step: cap.step });
    }
  }
  return out;
}

// fanActionOptions lista os comandos que o controle RF aceita de verdade. Nao
// existe "ligar": a ventoinha e ligada escolhendo uma velocidade (1..6). O
// desligamento pode ser "tudo" (power:false) ou velocidade 0.
export function fanActionOptions(fan: FanView | undefined): ControlOption[] {
  const caps = fan?.caps ?? {};
  const out: ControlOption[] = [];
  if ("speed" in caps) {
    out.push({
      value: "speed",
      label: "Velocidade",
      kind: "enum",
      numeric: true,
      values: FAN_SPEEDS,
      defaultValue: "1",
    });
  }
  if ("light" in caps) {
    out.push({ value: "light", label: "Luz", kind: "bool" });
  }
  if ("direction" in caps) {
    out.push({ value: "direction", label: "Direção", kind: "enum", values: FAN_DIRECTIONS });
  }
  if ("power" in caps) {
    out.push({
      value: "power",
      label: "Desligar tudo",
      kind: "bool",
      values: [{ value: "false", label: "Desligar tudo" }],
      defaultValue: "false",
    });
  }
  return out;
}

export function tuyaControlOptions(specs: TuyaSpec[] = []): ControlOption[] {
  return specs
    .filter((s) => s.dp_id && s.writable !== false && !TELEMETRY_CODES.has(s.code))
    .map(tuyaControl);
}

export function mideaControlOptions(device: MideaView | undefined): ControlOption[] {
  return (device?.specs ?? [])
    .filter((s) => s.key && s.writable !== false && !MIDEA_TELEMETRY.includes(s.key))
    .map(mideaControl);
}

const FAN_EVENT_LABELS: Record<string, string> = {
  power: "Ventilador",
  light: "Luz",
  speed: "Velocidade",
  direction: "Direção",
};

function controlAsEvent(c: ControlOption): EventOption {
  return {
    value: c.value,
    label: FAN_EVENT_LABELS[c.value] ?? c.label,
    kind: c.kind,
    values: c.values,
    min: c.min,
    max: c.max,
    step: c.step,
    numeric: c.numeric,
  };
}

// buildDeviceContracts converge as tres familias para o mesmo contrato. A UI
// escolhe primeiro o device e so depois descobre eventos e acoes dele.
export function buildDeviceContracts(
  fans: FanView[],
  tuya: TuyaView[],
  midea: MideaView[],
  specs: Record<string, TuyaSpec[]>,
  rooms: Room[],
): DeviceContract[] {
  const out: DeviceContract[] = [];

  for (const f of fans) {
    const events = fanEventOptions(f);
    out.push({
      key: contractKey("fan", f.id),
      id: f.id,
      driver: "fan",
      sourceType: "thing_state",
      name: f.name || f.device,
      room: roomName(rooms, f.room),
      online: f.online,
      events: events.map(controlAsEvent),
      actions: fanActionOptions(f),
    });
  }

  for (const d of tuya) {
    const events = tuyaEventOptions(d, specs[d.id] ?? []).filter((e) => e.value !== "__custom__");
    out.push({
      key: contractKey("tuya", d.id),
      id: d.id,
      driver: "tuya",
      sourceType: "tuya_dp",
      name: d.name || d.id,
      room: roomName(rooms, d.room),
      online: d.online,
      events,
      actions: tuyaControlOptions(specs[d.id] ?? []),
    });
  }

  for (const d of midea) {
    const events = mideaEventOptions(d).filter((e) => e.value !== "__custom__");
    out.push({
      key: contractKey("midea", d.id),
      id: d.id,
      driver: "midea",
      sourceType: "midea_state",
      name: d.name || d.id,
      room: roomName(rooms, d.room),
      online: d.online,
      events,
      actions: mideaControlOptions(d),
    });
  }

  return out.sort((a, b) => a.name.localeCompare(b.name, "pt-BR"));
}

export function actionValueChoices(control?: ControlOption): Choice[] {
  if (!control) return [];
  if (control.values?.length) return control.values;
  if (control.kind === "bool") {
    return [
      { value: "true", label: "Ligar" },
      { value: "false", label: "Desligar" },
    ];
  }
  if (control.kind === "enum") return control.values ?? [];
  return [];
}

export function coerceValue(kind: ControlKind, value: string): unknown {
  if (kind === "bool") return value === "true";
  if (kind === "number") return Number(value);
  return value;
}

/* ---------- rascunho <-> JSON ---------- */

export function emptyDraft(): AutomationDraft {
  return {
    name: "Nova automação",
    enabled: true,
    triggerKind: "tuya_dp",
    triggerDevice: "",
    triggerEvent: "",
    customDP: "",
    condition: "any",
    value: "",
    timeMode: "relative",
    timeSeconds: "30",
    timeUnit: "minutes",
    timeAt: "22:00",
    timeDays: [],
    once: false,
    scheduleEnabled: false,
    scheduleStart: "18:00",
    scheduleEnd: "03:00",
    scheduleDays: [...ALL_DAYS],
    delayEnabled: false,
    delayMs: "1500",
    actions: [{ kind: "tuya", target: "", control: "", controlKind: "bool", value: "true" }],
    extraActions: [],
    baseSource: {},
  };
}

function controlKindOf(value: unknown): ControlKind {
  if (typeof value === "boolean") return "bool";
  if (typeof value === "number") return "number";
  return "text";
}

function actionDrafts(a: AutomationAction): ActionDraft[] {
  if (a.kind === "fan") {
    const target = `${a.thing ?? ""}/${a.device_id ?? ""}`;
    const state = Object.entries(a.state ?? {});
    if (!state.length) {
      return [{ kind: "fan", target, control: "", controlKind: "bool", value: "true" }];
    }
    const hasSpeed = state.some(([control]) => control === "speed");
    return state
      .filter(([control, value]) => !(control === "power" && value === true && hasSpeed))
      .map(([control, value]) => {
        // Regras antigas podiam pedir power:true. O controle RF nao tem esse
        // comando: ligar significa escolher uma velocidade.
        if (control === "power" && value === true) {
          return {
            kind: "fan" as const,
            target,
            control: "speed",
            controlKind: "number" as const,
            value: "1",
          };
        }
        return {
          kind: "fan" as const,
          target,
          control,
          controlKind: controlKindOf(value),
          value: String(value),
        };
      });
  }
  if (a.kind === "tuya") {
    return [{
      kind: "tuya",
      target: a.device ?? "",
      control: a.dp ?? "",
      controlKind: controlKindOf(a.value),
      value: a.value == null ? "" : String(a.value),
    }];
  }
  if (a.kind === "midea") {
    const state = Object.entries(a.state ?? {});
    if (!state.length) {
      return [{ kind: "midea", target: a.device ?? "", control: "", controlKind: "bool", value: "true" }];
    }
    return state.map(([control, value]) => ({
      kind: "midea" as const,
      target: a.device ?? "",
      control,
      controlKind: controlKindOf(value),
      value: String(value),
    }));
  }
  return [];
}

export function draftFromAutomation(a: Automation): AutomationDraft {
  const d = emptyDraft();
  const source = a.source ?? {};
  const dps = source.dps ?? [];
  const triggerKind: TriggerKind =
    source.type === "time"
      ? "time"
      : source.type === "midea_state"
        ? "midea_state"
        : source.type === "tuya_dp"
          ? "tuya_dp"
          : source.type === "thing_state"
            ? "thing_state"
            : "manual";

  d.name = a.name ?? "";
  d.enabled = a.enabled !== false;
  d.once = a.once === true;
  d.triggerKind = triggerKind;
  d.triggerDevice = source.device ?? "";
  d.triggerEvent = dps.length > 1 ? "__unlock__" : (source.dp ?? dps[0] ?? "");
  d.baseSource = { ...source };

  if (triggerKind === "time") {
    d.timeDays = source.days?.length ? [...source.days] : [];
    if (source.at) {
      d.timeMode = "clock";
      d.timeAt = source.at;
    } else if (source.in_seconds && source.in_seconds > 0) {
      d.timeMode = "relative";
      const secs = source.in_seconds;
      if (secs >= 3600 && secs % 3600 === 0) {
        d.timeUnit = "hours";
        d.timeSeconds = String(secs / 3600);
      } else {
        d.timeUnit = "minutes";
        d.timeSeconds = String(Math.max(1, Math.round(secs / 60)));
      }
    } else if (source.fire_at) {
      // Já resolvido pelo hub: exibe como horário de relógio.
      const t = new Date(source.fire_at);
      d.timeMode = "clock";
      d.timeAt = Number.isNaN(t.getTime())
        ? "22:00"
        : `${String(t.getHours()).padStart(2, "0")}:${String(t.getMinutes()).padStart(2, "0")}`;
    }
  }

  if (source.value === true) d.condition = "true";
  else if (source.value === false) d.condition = "false";
  else if (source.value != null) {
    d.condition = "value";
    d.value = String(source.value);
  }

  const schedule = a.schedule;
  if (schedule?.start || schedule?.end) {
    d.scheduleEnabled = true;
    d.scheduleStart = schedule.start || "18:00";
    d.scheduleEnd = schedule.end || "03:00";
    d.scheduleDays = schedule.days?.length ? [...schedule.days] : [...ALL_DAYS];
  }

  if (a.delay) {
    d.delayEnabled = !!a.delay.enabled;
    d.delayMs = String(a.delay.ms ?? 1500);
  }

  for (const action of a.actions ?? []) {
    const drafts = actionDrafts(action);
    if (drafts.length) d.actions.push(...drafts);
    else d.extraActions.push(action);
  }
  if (d.actions.length) d.actions.shift();
  if (!d.actions.length && !d.extraActions.length) d.actions.push(emptyDraft().actions[0]);
  return d;
}

export function sourceFromDraft(d: AutomationDraft, events: EventOption[]): AutomationSource {
  if (d.triggerKind === "manual") {
    return { ...d.baseSource, type: "manual", device: undefined, dp: undefined, dps: undefined, value: undefined };
  }
  if (d.triggerKind === "time") {
    const base: AutomationSource = {
      ...d.baseSource,
      type: "time",
      device: undefined,
      dp: undefined,
      dps: undefined,
      value: undefined,
    };
    if (d.timeMode === "clock") {
      return {
        ...base,
        at: d.timeAt,
        date: undefined,
        in_seconds: undefined,
        fire_at: undefined,
        days: d.timeDays.length ? [...d.timeDays] : undefined,
      };
    }
    const n = Math.max(1, Number(d.timeSeconds) || 1);
    const seconds = d.timeUnit === "hours" ? Math.round(n * 3600) : Math.round(n * 60);
    return { ...base, at: undefined, date: undefined, in_seconds: seconds, fire_at: undefined, days: undefined };
  }
  const event = events.find((e) => e.value === d.triggerEvent);
  const legacyDps = d.baseSource.dps ?? [];
  const dps =
    event?.dps ??
    (d.triggerEvent === "__custom__"
      ? [d.customDP.trim()]
      : d.triggerEvent === "__unlock__" && legacyDps.length > 1
        ? legacyDps
        : [d.triggerEvent]);
  const valid = dps.filter(Boolean);
  const value =
    d.condition === "any"
      ? undefined
      : d.condition === "true"
        ? true
        : d.condition === "false"
          ? false
          : coerceValue(event?.numeric ? "number" : event?.kind === "event" ? "text" : event?.kind ?? "text", d.value);
  return {
    ...d.baseSource,
    type: d.triggerKind,
    device: d.triggerDevice,
    dp: valid.length === 1 ? valid[0] : undefined,
    dps: valid.length > 1 ? valid : undefined,
    value,
  };
}

function splitFanTarget(target: string): { thing: string; device: string } {
  const i = target.indexOf("/");
  if (i === -1) return { thing: target, device: "" };
  return { thing: target.slice(0, i), device: target.slice(i + 1) };
}

export function actionFromDraft(a: ActionDraft): AutomationAction {
  const value = coerceValue(a.controlKind, a.value);
  if (a.kind === "fan") {
    const { thing, device } = splitFanTarget(a.target);
    return { kind: "fan", thing, device_id: device, state: { [a.control]: value } };
  }
  if (a.kind === "tuya") {
    return { kind: "tuya", device: a.target, dp: a.control, value };
  }
  return { kind: "midea", device: a.target, state: { [a.control]: value } };
}

export function automationFromDraft(
  d: AutomationDraft,
  events: EventOption[],
  id?: string,
): Automation {
  const actions = [...d.actions.map(actionFromDraft), ...d.extraActions];
  const schedule: AutomationSchedule | undefined =
    d.scheduleEnabled && d.triggerKind !== "manual" && d.triggerKind !== "time"
      ? { start: d.scheduleStart, end: d.scheduleEnd, days: [...d.scheduleDays] }
      : undefined;
  const delay = d.delayEnabled
    ? { enabled: true, ms: Number(d.delayMs) || 0 }
    : undefined;
  // Um horário com dias marcados é semanal (fica na lista e se reagenda); sem
  // dias, é o volátil de sempre (dispara uma vez e some).
  const weekly = d.triggerKind === "time" && d.timeMode === "clock" && d.timeDays.length > 0;
  const once = d.triggerKind === "time" ? !weekly : d.once;
  return {
    id: id ?? "",
    name: d.name.trim(),
    enabled: d.enabled,
    once: once ? true : undefined,
    source: sourceFromDraft(d, events),
    schedule,
    delay,
    actions,
  };
}

export function validateDraft(d: AutomationDraft, events: EventOption[]): string | null {
  if (!d.name.trim()) return "Dê um nome para a automação.";
  if (d.triggerKind === "time") {
    if (d.timeMode === "clock") {
      if (!/^\d{1,2}:\d{2}$/.test(d.timeAt.trim())) return "Informe um horário válido (HH:MM).";
    } else {
      const n = Number(d.timeSeconds);
      if (!Number.isFinite(n) || n <= 0) return "Informe um tempo maior que zero.";
    }
  } else if (d.triggerKind !== "manual") {
    if (!d.triggerDevice) return "Escolha o dispositivo que dispara a automação.";
    if (!d.triggerEvent) return "Escolha o evento que dispara a automação.";
    if (d.triggerEvent === "__custom__" && !d.customDP.trim()) return "Informe o id do data point.";
    if (!events.some((e) => e.value === d.triggerEvent)) return "Escolha um evento válido.";
    if (d.condition === "value" && !d.value.trim()) return "Informe o valor do evento.";
  }
  if (!d.actions.length && !d.extraActions.length) return "Adicione pelo menos uma ação.";
  const fanTargets = new Set<string>();
  for (const a of d.actions) {
    if (!a.target) return "Escolha o dispositivo de uma das ações.";
    if (!a.control) return "Escolha o que a ação deve fazer.";
    if (!a.value.trim()) return "Informe o valor da ação.";
    if (a.kind === "fan") {
      if (fanTargets.has(a.target)) return "Use só um comando por ventilador em cada automação.";
      fanTargets.add(a.target);
    }
  }
  if (d.scheduleEnabled && d.triggerKind !== "time") {
    if (!d.scheduleStart || !d.scheduleEnd) return "Informe o horário inicial e o final.";
    if (!d.scheduleDays.length) return "Escolha pelo menos um dia da semana.";
  }
  if (d.delayEnabled) {
    const ms = Number(d.delayMs);
    if (!Number.isFinite(ms) || ms <= 0) return "Informe um atraso maior que zero.";
    if (ms > 60000) return "O atraso máximo é 60.000 ms.";
  }
  return null;
}

/* ---------- rotulos ---------- */

export function daysLabel(days?: number[]): string {
  if (!days || !days.length || days.length === 7) return "todos os dias";
  const set = new Set(days);
  if (days.length === 5 && [1, 2, 3, 4, 5].every((d) => set.has(d))) return "seg–sex";
  if (days.length === 2 && set.has(0) && set.has(6)) return "fim de semana";
  return ALL_DAYS.filter((d) => set.has(d)).map((d) => DAY_SHORT[d]).join(", ");
}

export function scheduleLabel(schedule?: AutomationSchedule): string {
  if (!schedule?.start || !schedule?.end) return "Sempre";
  return `${schedule.start} → ${schedule.end} · ${daysLabel(schedule.days)}`;
}

function sameDps(a: string[] = [], b: string[] = []): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((x) => set.has(x));
}

function eventLabel(event: EventOption | undefined, dp: string, eventCount = 1): string {
  if (event) return event.label;
  if (eventCount > 1) return `Evento (${eventCount} DPs)`;
  return dp ? `DP ${dp}` : "evento";
}

function valueLabel(event: EventOption | undefined, value: unknown): string {
  if (value === true) return "ligou";
  if (value === false) return "desligou";
  if (value == null) return event?.kind === "event" ? "" : "mudou";
  const hit = event?.values?.find((v) => v.value === String(value));
  return hit ? `mudou para ${hit.label}` : `= ${fmtValue(value)}`;
}

export interface SummaryLookups {
  deviceName?: (id: string) => string;
  tuyaDP?: (device: string, dp: string) => string;
  mideaKey?: (device: string, key: string) => string;
}

export function sourceSummary(
  a: Automation | AutomationSource,
  events: EventOption[],
  lookups: SummaryLookups = {},
): string {
  const src: AutomationSource =
    "source" in a ? ((a as Automation).source ?? {}) : (a as AutomationSource);
  if (!src.type || src.type === "manual") return "Cena manual";
  if (src.type === "time") {
    if (src.at) {
      return src.days?.length ? `${daysLabel(src.days)} · às ${src.at}` : `às ${src.at}`;
    }
    if (src.in_seconds) return `daqui a ${fmtDuration(src.in_seconds)}`;
    if (src.fire_at) {
      const t = new Date(src.fire_at);
      if (!Number.isNaN(t.getTime())) {
        const at = `às ${String(t.getHours()).padStart(2, "0")}:${String(t.getMinutes()).padStart(2, "0")}`;
        return src.days?.length ? `${daysLabel(src.days)} · ${at}` : at;
      }
    }
    return "no horário";
  }
  const name = lookups.deviceName?.(src.device ?? "") || src.device || "dispositivo";
  const dps = src.dps ?? (src.dp ? [src.dp] : []);
  const event =
    dps.length > 1
      ? events.find((e) => e.dps && sameDps(e.dps, dps))
      : events.find((e) => e.value === (src.dp ?? dps[0]));
  const eventText = eventLabel(event, src.dp ?? dps[0] ?? "", dps.length);
  const condition = valueLabel(event, src.value);
  return `${name} · ${eventText}${condition ? ` · ${condition}` : ""}`;
}

export function actionSummary(a: AutomationAction, lookups: SummaryLookups = {}): string {
  if (a.kind === "fan") {
    const id = `${a.thing ?? ""}/${a.device_id ?? ""}`;
    const name = lookups.deviceName?.(id) || id;
    const parts = Object.entries(a.state ?? {}).map(([key, value]) => {
      if (key === "power" && value === false) return "desligar tudo";
      if (key === "speed") return `velocidade ${fmtValue(value)}`;
      const label = FAN_STATE_LABELS[key] ?? key;
      return `${label} ${value === true ? "ligar" : value === false ? "desligar" : `= ${fmtValue(value)}`}`;
    });
    return `${name} · ${parts.join(", ")}`;
  }
  if (a.kind === "tuya") {
    const name = lookups.deviceName?.(a.device ?? "") || a.device || "dispositivo";
    const dp = lookups.tuyaDP?.(a.device ?? "", a.dp ?? "") || a.dp || "DP";
    return `${name} · ${dp} ${a.value === true ? "ligar" : a.value === false ? "desligar" : `= ${fmtValue(a.value)}`}`;
  }
  if (a.kind === "midea") {
    const name = lookups.deviceName?.(a.device ?? "") || a.device || "ar-condicionado";
    const parts = Object.entries(a.state ?? {}).map(([key, value]) => {
      const label = lookups.mideaKey?.(a.device ?? "", key) || key;
      return `${label} ${value === true ? "ligar" : value === false ? "desligar" : `= ${fmtValue(value)}`}`;
    });
    return `${name} · ${parts.join(", ")}`;
  }
  return a.kind || "ação";
}

export function actionsSummary(actions: AutomationAction[] = [], lookups: SummaryLookups = {}): string {
  return actions.map((a) => actionSummary(a, lookups)).join(" + ") || "nenhuma ação";
}