// Logica PURA da familia Tuya: protocolo de DPs (specs), telemetria de energia
// e montagem da lista de devices a partir do banco + do estado ao vivo.
//
// Sem Svelte, sem api e sem stores: so tipos. Os layouts (socket/switch) e os
// componentes so apresentam o que estas funcoes derivam.

import type { HubDevice, TuyaDevice, TuyaSpec } from "$lib/types";

// TuyaView e o device Tuya ja normalizado para os cards consumirem: alem dos
// campos do HubDevice, carrega as specs, o estado (dp_id -> valor) e se o
// listener esta aberto.
export interface TuyaView extends HubDevice {
  kind: "tuya";
  name: string;
  room: string;
  online: boolean;
  template: string;
  specs: TuyaSpec[];
  state: Record<string, unknown>;
  listening: boolean;
  ip?: string;
  has_key?: boolean;
  category?: string;
  cloud_only?: boolean;
}

export interface DpInfo {
  label: string;
  desc?: string;
}

// DP_INFO e um dicionario curado (pt-BR) dos codigos de DP mais comuns, usado
// quando a nuvem Tuya nao devolveu nome/descricao. O `name` da spec vence.
export const DP_INFO: Record<string, DpInfo> = {
  relay_status: {
    label: "Estado do relé",
    desc: "Como o relé fica ao voltar a energia: desligado, ligado ou memória.",
  },
  power_off: { label: "Memória de energia", desc: "Comportamento de desligamento ao faltar energia." },
  light_mode: {
    label: "Modo do indicador",
    desc: "Quando o LED do aparelho fica aceso (relé, posição, nenhum, sempre).",
  },
  relay: { label: "Relé", desc: "Estado do relé." },
  cycle_time: { label: "Tempo de ciclo", desc: "Intervalo do modo intermitente (liga/desliga alternado)." },
  random_time: { label: "Tempo aleatório", desc: "Intervalo aleatório usado no modo intermitente." },
  switch_inching: { label: "Pulsação (inching)", desc: "Liga por alguns segundos e desliga sozinho." },
  overcharge_switch: {
    label: "Proteção de sobrecarga",
    desc: "Desliga o dispositivo quando a potência fica abaixo de 3 W por 40 minutos.",
  },
  cur_power: { label: "Potência", desc: "Potência instantânea medida (W)." },
  cur_voltage: { label: "Tensão", desc: "Tensão da rede medida (V)." },
  cur_current: { label: "Corrente", desc: "Corrente medida (A)." },
  countdown_1: { label: "Timer", desc: "Desliga após o tempo definido." },
  countdown: { label: "Timer", desc: "Desliga após o tempo definido." },
  child_lock: { label: "Trava infantil", desc: "Bloqueia o controle físico do aparelho." },
  switch_1: { label: "Chave 1" },
  switch_2: { label: "Chave 2" },
  switch_3: { label: "Chave 3" },
  switch_4: { label: "Chave 4" },
  switch_5: { label: "Chave 5" },
  switch_6: { label: "Chave 6" },
  switch: { label: "Chave" },
  switch_led: { label: "Luz" },
  switch_usb: { label: "USB" },
  bright_value: { label: "Brilho" },
  temp_value: { label: "Temperatura da luz" },
  work_mode: { label: "Modo de trabalho" },
  // Fechaduras (categoria jtmspro e afins). O valor de cada DP de abertura e o
  // id do metodo usado (digital, senha, ...), nao um horario.
  unlock_fingerprint: { label: "Digital", desc: "Última abertura por impressão digital." },
  unlock_password: { label: "Senha", desc: "Última abertura por senha." },
  unlock_temporary: { label: "Senha temporária", desc: "Última abertura por senha temporária." },
  unlock_card: { label: "Cartão", desc: "Última abertura por cartão." },
  unlock_app: { label: "App", desc: "Última abertura pelo aplicativo." },
  unlock_offline_pd: { label: "Abertura offline" },
  unlock_request: { label: "Pedido de abertura" },
  residual_electricity: { label: "Bateria", desc: "Carga restante da bateria (%)." },
  battery_state: { label: "Bateria" },
  alarm_lock: { label: "Alarme", desc: "Último alarme reportado pela fechadura." },
  reverse_lock: { label: "Tranca reversa", desc: "Ferrolho invertido / tranca por dentro." },
  hijack: { label: "Coação", desc: "Abertura com código de coação." },
  doorbell: { label: "Campainha" },
  update_all_finger: { label: "Digitais cadastradas" },
  update_all_password: { label: "Senhas cadastradas" },
  update_all_card: { label: "Cartões cadastrados" },
  remote_no_dp_key: { label: "Chave de abertura remota" },
  remote_no_pd_setkey: { label: "Config. de abertura remota" },
  initiative_message: { label: "Mensagem do dispositivo" },
};

// dpLabel resolve o rotulo de uma spec: nome da nuvem > dicionario > codigo.
export function dpLabel(spec?: TuyaSpec | null): string {
  if (!spec) return "";
  const info = DP_INFO[spec.code];
  return spec.name || info?.label || spec.code || "";
}

// dpDesc resolve a descricao: a da spec (quando util) > dicionario.
export function dpDesc(spec?: TuyaSpec | null): string {
  if (!spec) return "";
  const d = String(spec.desc ?? "").trim();
  if (d && d !== "{}" && d !== "null") return d;
  return DP_INFO[spec.code]?.desc ?? "";
}

/* ---------- specs/DP ---------- */

// MASTER_CODES sao os DPs que representam a chave mestre de uma tomada.
export const MASTER_CODES = ["switch_1", "switch", "power", "switch_led"];

export interface TelemetryDef {
  key: string;
  codes: string[];
  scale: number;
  digits: number;
  unit: string;
  label: string;
  icon: string;
  accent: string;
  max: number;
}

// TELEMETRY descreve as grandezas eletricas exibidas no card da tomada.
export const TELEMETRY: TelemetryDef[] = [
  { key: "power", codes: ["cur_power"], scale: 0.1, digits: 1, unit: "W", label: "Potência", icon: "bolt", accent: "tertiary", max: 2200 },
  { key: "voltage", codes: ["cur_voltage"], scale: 0.1, digits: 1, unit: "V", label: "Tensão", icon: "plug", accent: "secondary", max: 260 },
  { key: "current", codes: ["cur_current"], scale: 0.001, digits: 2, unit: "A", label: "Corrente", icon: "gauge", accent: "primary", max: 16 },
];

export interface TelemetryItem {
  t: TelemetryDef;
  spec: TuyaSpec;
}

// specByCode devolve a primeira spec cujo codigo esta na lista.
export function specByCode(specs: TuyaSpec[] | undefined, codes: string[]): TuyaSpec | null {
  for (const s of specs ?? []) if (codes.includes(s.code)) return s;
  return null;
}

// tuyaVal le o valor de um DP no estado do device (chaveado pelo dp_id).
export function tuyaVal(d: { state?: Record<string, unknown> }, spec?: TuyaSpec | null): unknown {
  if (!spec) return undefined;
  return (d.state ?? {})[spec.dp_id];
}

// tuyaTelemetry pareia cada grandeza de TELEMETRY com a spec correspondente.
export function tuyaTelemetry(specs: TuyaSpec[] | undefined): TelemetryItem[] {
  return TELEMETRY.map((t) => ({ t, spec: specByCode(specs, t.codes) })).filter(
    (x): x is TelemetryItem => x.spec !== null,
  );
}

// tuyaBoolSpecs lista as specs booleanas (chaves liga/desliga).
export function tuyaBoolSpecs(specs: TuyaSpec[] | undefined): TuyaSpec[] {
  return (specs ?? []).filter((s) => s.type === "Boolean");
}

// TUYA_SWITCH_EXCLUDE sao DPs booleanos que configuram o aparelho em vez de
// acionar uma saida, entao "desligar tudo" precisa deixa-los em paz.
export const TUYA_SWITCH_EXCLUDE = ["switch_inching", "child_lock", "overcharge_switch"];

// tuyaOffSpecs lista as saidas booleanas graváveis de um device Tuya: cada
// gang de um multi-switch, mais as chaves power/relay/light/USB.
export function tuyaOffSpecs(d: { specs?: TuyaSpec[] }): TuyaSpec[] {
  return tuyaBoolSpecs(d.specs ?? [])
    .filter((s) => s.writable !== false)
    .filter((s) => !TUYA_SWITCH_EXCLUDE.includes(s.code))
    .filter((s) => s.code === "power" || s.code === "relay" || /^switch/.test(s.code ?? ""));
}

// tuyaHasTelemetry indica se o device expoe alguma grandeza de medicao.
export function tuyaHasTelemetry(specs: TuyaSpec[] | undefined): boolean {
  return tuyaTelemetry(specs ?? []).length > 0;
}

/* ---------- dados ---------- */

// Linha minima do registro do hub necessaria para enriquecer o device.
export interface TuyaRow {
  id: string;
  kind: string;
  room_id?: string;
  template?: string;
}

// buildTuyaDevices junta os devices Tuya (fonte de verdade) com as specs
// carregadas, o estado, a lista de listeners e as linhas do banco (room/
// template). O banco vence o que veio no proprio device.
export function buildTuyaDevices(
  devices: TuyaDevice[],
  specs: Record<string, TuyaSpec[]>,
  listening: string[] = [],
  rows: TuyaRow[] = [],
): TuyaView[] {
  const byId: Record<string, TuyaRow> = {};
  for (const r of rows) if (r.kind === "tuya") byId[r.id] = r;
  return devices.map((d) => {
    const stored = byId[d.id] ?? ({} as Partial<TuyaRow>);
    const extra = d as TuyaDevice & { listening?: boolean };
    return {
      ...d,
      kind: "tuya" as const,
      name: d.name || d.id,
      room: stored.room_id || d.room_id || "",
      template: stored.template || d.template || "",
      category: d.category,
      cloud_only: d.cloud_only,
      specs: specs[d.id] ?? [],
      state: d.state ?? {},
      listening: extra.listening ?? listening.includes(d.id),
      online: !!d.online,
    };
  });
}
