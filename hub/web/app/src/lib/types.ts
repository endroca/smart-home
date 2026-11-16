// Formas JSON trocadas com o hub. Espelham os `json:` das structs Go
// (internal/model, internal/db, internal/httpapi) e os contratos em
// contracts/*.schema.json. Escritas a mao por enquanto.

export interface Health {
  ok?: boolean;
  things?: number;
  version?: string;
  uptime?: number;
  build?: string;
  pendingOTA?: Record<string, unknown>;
}

/* ---------- gateway (thing) ---------- */

export interface Capability {
  kind?: string;
  stateful?: boolean;
  writable?: boolean;
  values?: string[];
  min?: number;
  max?: number;
  step?: number;
  unit?: string;
}

export interface ManifestDevice {
  id: string;
  type?: string;
  name?: string;
  capabilities?: Record<string, Capability>;
  actions?: string[];
}

export interface Firmware {
  version?: string;
  build?: string;
}

export interface Manifest {
  schemaVersion?: number;
  manifestVersion?: number;
  thing?: string;
  model?: string;
  protocol?: string;
  firmware?: Firmware;
  http?: { mdns?: string; port?: number };
  actions?: string[];
  devices?: ManifestDevice[];
}

export interface OTAStatus {
  state?: string;
  version?: string;
}

export interface ThingStatus {
  online?: boolean;
  thing?: string;
  version?: string;
  ts?: number;
  ip?: string;
  rssi?: number;
  heap?: number;
  uptime?: number;
  pendingSync?: boolean;
  manifestVersion?: number;
  ota?: OTAStatus;
}

export interface DeviceState {
  thing?: string;
  id?: string;
  reported?: Record<string, unknown>;
  stale?: boolean;
  updatedAt?: string;
}

export interface Thing {
  id: string;
  manifest?: Manifest;
  status?: ThingStatus;
  states?: Record<string, DeviceState>;
  lastEvent?: Record<string, unknown>;
  lastAck?: Record<string, unknown>;
  updatedAt?: string;
}

/* ---------- registro do hub ---------- */

export interface Room {
  id: string;
  name: string;
  icon?: string;
  sort?: number;
  created_at?: string;
  updated_at?: string;
}

export interface DeviceRow {
  id: string;
  kind: string;
  name?: string;
  firmware_name?: string;
  ip?: string;
  port?: number;
  version?: string;
  product_key?: string;
  device22?: boolean;
  key_source?: string;
  key_updated_at?: string;
  token?: string;
  url?: string;
  room_id?: string;
  owner?: string;
  device_id?: string;
  template?: string;
  category?: string;
  cloud_only?: boolean;
  /** Ordem de exibicao dentro do comodo (menor primeiro). */
  sort?: number;
  has_token?: boolean;
  created_at?: string;
  updated_at?: string;
}

/* ---------- Tuya ---------- */

export interface TuyaSpec {
  device_id?: string;
  dp_id: string;
  code: string;
  name?: string;
  desc?: string;
  type: string;
  values?: unknown;
  writable?: boolean;
}

export interface TuyaDevice {
  id: string;
  name?: string;
  ip?: string;
  port?: number;
  version?: string;
  product_key?: string;
  device22?: boolean;
  has_key?: boolean;
  key_source?: string;
  key_updated_at?: string;
  room_id?: string;
  template?: string;
  category?: string;
  cloud_only?: boolean;
  online?: boolean;
  state?: Record<string, unknown>;
  specs?: TuyaSpec[];
}

export interface TuyaScanDevice {
  id: string;
  name?: string;
  ip?: string;
  version?: string;
  productKey?: string;
  registered?: boolean;
  raw?: Record<string, unknown>;
}

export interface TuyaCloudDevice {
  id: string;
  name?: string;
  category?: string;
  product_key?: string;
  has_key?: boolean;
  online?: boolean;
  registered?: boolean;
}

export interface TuyaEnergyPoint {
  time: string;
  value: number;
}

export interface TuyaOpenRecord {
  at: number;
  code: string;
  value?: string;
  unlock_name?: string;
  user_name?: string;
  user_id?: string;
}

/* ---------- Midea ---------- */

export interface MideaSpec {
  device_id?: string;
  key: string;
  label?: string;
  type?: string;
  options?: unknown;
  writable?: boolean;
}

export interface MideaDevice {
  id: string;
  name?: string;
  ip?: string;
  port?: number;
  model?: string;
  has_key?: boolean;
  room_id?: string;
  template?: string;
  online?: boolean;
  state?: Record<string, unknown>;
  actions?: MideaSpec[];
  registered?: boolean;
}

export interface MideaScanDevice {
  id: string;
  name?: string;
  ip?: string;
  sn?: string;
  type?: string;
  [k: string]: unknown;
}

/* ---------- automacoes ---------- */

export interface AutomationSchedule {
  start?: string;
  end?: string;
  days?: number[];
}

export interface AutomationDelay {
  enabled?: boolean;
  ms?: number;
}

export interface AutomationSource {
  type?: string;
  device?: string;
  dp?: string;
  dps?: string[];
  value?: unknown;
  /** Gatilho de horário (type: "time"): "HH:MM" local. */
  at?: string;
  /** Data opcional do gatilho de horário ("YYYY-MM-DD"). */
  date?: string;
  /** Atraso relativo em segundos ("desliga daqui a 10 min"). */
  in_seconds?: number;
  /** Instante absoluto resolvido pelo hub (RFC3339); sobrevive a restart. */
  fire_at?: string;
  /** Gatilho semanal: dispara em `at` nos dias escolhidos (0=Dom..6=Sáb). */
  days?: number[];
  /** Só dispara quando o usuário inicia: ignora mudanças causadas por outra
   *  automação. Sem efeito em gatilhos manual/horário. */
  only_user?: boolean;
}

export interface AutomationAction {
  kind?: string;
  thing?: string;
  device_id?: string;
  device?: string;
  dp?: string;
  value?: unknown;
  state?: Record<string, unknown>;
}

export interface Automation {
  id: string;
  name?: string;
  enabled?: boolean;
  /** Volátil: dispara uma vez e some (deletada) ao executar. */
  once?: boolean;
  source?: AutomationSource;
  schedule?: AutomationSchedule;
  delay?: AutomationDelay;
  actions?: AutomationAction[];
  created_at?: string;
  updated_at?: string;
}

/* ---------- firmware ---------- */

export interface FirmwareFile {
  name: string;
  version: string;
  size?: number;
}

export interface FirmwareInfo {
  files: FirmwareFile[];
  pending: Record<string, unknown>;
}

/* ---------- eventos SSE ---------- */

export interface HubEvent {
  type?: string;
  data?: {
    device?: string;
    dps?: Record<string, unknown>;
    kind?: string;
    at?: string;
    state?: Record<string, unknown>;
    thing?: string;
    id?: string;
    reported?: Record<string, unknown>;
    status?: ThingStatus;
    event?: Record<string, unknown>;
    [k: string]: unknown;
  };
}

/* ---------- device normalizado (o que os cards consomem) ---------- */

export interface HubDevice {
  id: string;
  kind: "fan" | "tuya" | "midea" | "generic" | string;
  name: string;
  room: string;
  online: boolean;
  template: string;
  /** Ordem de exibicao dentro do comodo (menor primeiro). */
  sort?: number;
  /** Categoria Tuya (ex.: "jtmspro" numa fechadura); usada pela inferência. */
  category?: string;
  [k: string]: unknown;
}

/* ---------- funcoes do device ---------- */

// DeviceFunction e uma funcao configuravel do device: o rotulo que a voz
// reconhece e, quando marcada, o que "desligar tudo" envia.
export interface DeviceFunction {
  key: string;
  label: string;
  aliases?: string[];
  voice: boolean;
  off: boolean;
  off_value?: unknown;
  driver: string;
  kind: "bool" | "enum" | "number" | "text";
  values?: string[];
  min?: number;
  max?: number;
  step?: number;
  unit?: string;
}

// FunctionCatalog e o catalogo efetivo de um device: derivado das capacidades
// ou o que o operador salvou (configured=true).
export interface FunctionCatalog {
  device_id: string;
  driver: string;
  configured: boolean;
  functions: DeviceFunction[];
}
