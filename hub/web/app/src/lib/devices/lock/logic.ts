// Logica PURA do layout de fechadura Tuya (categoria jtmspro e afins).
//
// A fechadura e um device de bateria que dorme: o hub nao mantem conexao local
// com ela (cloud_only) e le o estado pela nuvem. O que chega aqui e o ultimo
// estado reportado, nao um valor ao vivo — por isso o card fala em "ultimo".

import type { TuyaSpec } from "$lib/types";
import { DP_INFO, dpLabel, specByCode, tuyaVal, type TuyaView } from "../tuya/logic";

// LOCK_CATEGORIES sao as categorias Tuya de fechadura/controle de acesso.
export const LOCK_CATEGORIES = ["mk", "ms", "jtmsbh", "jtmspro", "gyms", "hotelms", "videolock", "photolock"];

export function isLockCategory(category?: string): boolean {
  return LOCK_CATEGORIES.includes(String(category ?? "").trim().toLowerCase());
}

export interface LockView extends TuyaView {
  category?: string;
  cloud_only?: boolean;
}

// DPs que o card trata por si; o resto vai para a configuracao avancada.
export const BATTERY_CODES = ["residual_electricity", "battery_state", "battery_percentage"];

// UNLOCK_CODES: metodos de abertura, na ordem em que o card os mostra. O valor
// de cada DP e o id do metodo usado na ultima vez (nao um horario).
export const UNLOCK_CODES = ["unlock_fingerprint", "unlock_password", "unlock_temporary", "unlock_card", "unlock_app"];

export type LockState = Pick<LockView, "specs" | "state">;

// lockBattery devolve a carga restante (0..100) ou null quando desconhecida.
export function lockBattery(d: LockState): number | null {
  const spec = specByCode(d.specs, BATTERY_CODES);
  if (!spec) return null;
  const n = Number(tuyaVal(d, spec));
  return Number.isFinite(n) ? n : null;
}

// ALARM_LABELS traduz o enum alarm_lock para pt-BR.
export const ALARM_LABELS: Record<string, string> = {
  wrong_finger: "Digital inválida",
  wrong_password: "Senha inválida",
  wrong_card: "Cartão inválido",
  wrong_face: "Face inválida",
  tongue_bad: "Lingueta travada",
  too_hot: "Temperatura alta",
  unclosed_time: "Porta aberta por muito tempo",
  tongue_not_out: "Lingueta não saiu",
  pry: "Arrombamento",
  key_in: "Chave inserida",
  low_battery: "Bateria fraca",
  power_off: "Sem energia",
  shock: "Vibração/choque",
  defense: "Proteção",
};

// lockAlarm devolve o ultimo alarme conhecido (codigo cru + rotulo) ou null.
export function lockAlarm(d: LockState): { code: string; label: string } | null {
  const spec = specByCode(d.specs, ["alarm_lock"]);
  if (!spec) return null;
  const raw = tuyaVal(d, spec);
  const code = typeof raw === "string" ? raw.trim() : "";
  if (!code) return null;
  return { code, label: ALARM_LABELS[code] ?? code };
}

export interface LockMethod {
  spec: TuyaSpec;
  label: string;
  value: unknown;
}

// lockMethods lista os metodos de abertura presentes, com o ultimo id usado.
export function lockMethods(d: LockState): LockMethod[] {
  return UNLOCK_CODES.map((code) => specByCode(d.specs, [code]))
    .filter((s): s is TuyaSpec => !!s)
    .map((spec) => ({ spec, label: dpLabel(spec) || spec.code, value: tuyaVal(d, spec) }));
}

// LockFlags sao os indicadores booleanos da fechadura.
export interface LockFlags {
  reverse: boolean;
  hijack: boolean;
  doorbell: boolean;
}

export function lockFlags(d: LockState): LockFlags {
  const on = (code: string) => {
    const spec = specByCode(d.specs, [code]);
    return spec ? tuyaVal(d, spec) === true : false;
  };
  return { reverse: on("reverse_lock"), hijack: on("hijack"), doorbell: on("doorbell") };
}

// lockExtraSpecs sao as specs que nao cabem no card: digitais/senhas/cartoes
// vinculados, mensagens iniciadas pelo device e o handshake da abertura remota.
export const LOCK_INTERNAL_CODES = [
  ...BATTERY_CODES,
  ...UNLOCK_CODES,
  "alarm_lock",
  "reverse_lock",
  "hijack",
  "doorbell",
  "unlock_offline_pd",
  "unlock_request",
  "update_all_finger",
  "update_all_password",
  "update_all_card",
  "initiative_message",
];

export function lockExtraSpecs(specs?: TuyaSpec[]): TuyaSpec[] {
  return (specs ?? []).filter((s) => !LOCK_INTERNAL_CODES.includes(s.code));
}

// unlockCodeLabel resolve o codigo de um metodo de abertura (num registro de
// historico) para o rotulo pt-BR do dicionario de DPs.
export function unlockCodeLabel(code?: string): string {
  const c = String(code ?? "").trim();
  if (!c) return "Abertura";
  return DP_INFO[c]?.label ?? c;
}
