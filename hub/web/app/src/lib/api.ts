// Cliente HTTP do hub. `request` e o unico ponto que fala com fetch; os modulos
// de device e as telas usam os atalhos (get/post/del) ou `api.request`.
//
// O hub responde JSON e usa `{"ok": false, "error": {"code", "message"}}` para
// falhas; `request` transforma isso em ApiError.

import type {
  Automation,
  DeviceFunction,
  DeviceRow,
  FirmwareFile,
  FirmwareInfo,
  FunctionCatalog,
  Health,
  MideaDevice,
  MideaScanDevice,
  Room,
  Thing,
  TuyaCloudDevice,
  TuyaDevice,
  TuyaOpenRecord,
  TuyaScanDevice,
  TuyaSpec,
} from "./types";

export class ApiError extends Error {
  status: number;
  code?: string;

  constructor(message: string, status: number, code?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export interface RequestOptions {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
  headers?: Record<string, string>;
}

async function request<T = unknown>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { ...(opts.headers ?? {}) };
  let body: BodyInit | undefined;

  if (opts.body !== undefined && opts.body !== null) {
    if (typeof opts.body === "string" || opts.body instanceof FormData || opts.body instanceof Blob) {
      body = opts.body as BodyInit;
    } else {
      body = JSON.stringify(opts.body);
      headers["Content-Type"] = "application/json";
    }
  }

  const res = await fetch(path, { method: opts.method ?? "GET", headers, body, signal: opts.signal });

  let data: unknown = {};
  const text = await res.text();
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = {};
    }
  }

  const payload = data as { ok?: boolean; error?: { code?: string; message?: string } };
  if (!res.ok || payload.ok === false) {
    const err = payload.error ?? {};
    throw new ApiError(err.message || err.code || `HTTP ${res.status}`, res.status, err.code);
  }
  return data as T;
}

export const realApi = {
  request,
  get: <T = unknown>(path: string, opts?: RequestOptions) => request<T>(path, { ...opts, method: "GET" }),
  post: <T = unknown>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "POST", body }),
  del: <T = unknown>(path: string, opts?: RequestOptions) => request<T>(path, { ...opts, method: "DELETE" }),

  enc: (v: string) => encodeURIComponent(v),

  health: async (): Promise<Health> => {
    try {
      return (await fetch("/api/health", { cache: "no-store" })).json();
    } catch {
      return {};
    }
  },

  things: () => request<{ things: Thing[] }>("/api/things"),
  rooms: () => request<{ rooms: Room[] }>("/api/rooms"),
  devices: () => request<{ devices: DeviceRow[] }>("/api/devices"),
  automations: () => request<{ automations: Automation[] }>("/api/automations"),
  firmware: () => request<FirmwareInfo>("/api/firmware"),
  // Envia um .bin para o diretorio de firmware do hub (multipart).
  firmwareUpload: (file: File) => {
    const fd = new FormData();
    fd.append("file", file, file.name);
    return request<{ files: FirmwareFile[] }>("/api/firmware", { method: "POST", body: fd });
  },
  firmwareDelete: (name: string) =>
    request<{ files: FirmwareFile[] }>(`/api/firmware/${encodeURIComponent(name)}`, { method: "DELETE" }),

  tuyaDevices: () =>
    request<{ devices: TuyaDevice[]; cloud: boolean; listening: string[] }>("/api/tuya/devices"),
  tuyaScan: () => request<{ devices: TuyaScanDevice[] }>("/api/tuya/scan", { method: "POST", body: {} }),
  // Devices do app Tuya (nuvem). Necessario para cadastrar um device que nao
  // aparece na varredura de LAN, como uma fechadura que dorme.
  tuyaCloud: () => request<{ devices: TuyaCloudDevice[] }>("/api/tuya/cloud"),
  tuyaRegister: (id: string) => request("/api/tuya/register", { method: "POST", body: { id } }),
  tuyaForget: (id: string) => request("/api/tuya/forget", { method: "POST", body: { id } }),
  tuyaListen: (id: string, action: string) => request("/api/tuya/listen", { method: "POST", body: { id, action } }),
  tuyaControl: (id: string, dp: string, value: unknown) =>
    request("/api/tuya/control", { method: "POST", body: { id, dp, value } }),
  tuyaSpecs: (id: string) => request<{ specs: TuyaSpec[] }>(`/api/tuya/specs?id=${encodeURIComponent(id)}`),
  tuyaState: (id: string) => request<{ state: Record<string, unknown> }>(`/api/tuya/state?id=${encodeURIComponent(id)}`),
  tuyaEnergy: (id: string, range: string) =>
    request<{ series?: unknown[]; range?: string }>(`/api/tuya/energy?id=${encodeURIComponent(id)}&range=${encodeURIComponent(range)}`),
  // Historico de aberturas de uma fechadura (nuvem Smart Lock). Sempre responde
  // 200: o corpo traz available=false e um reason quando a nuvem nao responde.
  tuyaRecords: (id: string, limit?: number) =>
    request<{ available?: boolean; reason?: string; message?: string; total?: number; records?: TuyaOpenRecord[] }>(
      `/api/tuya/records?id=${encodeURIComponent(id)}${limit ? `&limit=${limit}` : ""}`,
    ),

  mideaDevices: () => request<{ devices: MideaDevice[]; cloud: boolean }>("/api/midea/devices"),
  mideaScan: (ip?: string) =>
    request<{ devices: MideaScanDevice[] }>(`/api/midea/scan${ip ? `?ip=${encodeURIComponent(ip)}` : ""}`, {
      method: "POST",
      body: {},
    }),
  mideaRegister: (id: string, name?: string, ip?: string) =>
    request("/api/midea/register", { method: "POST", body: { id, name, ip } }),
  mideaForget: (id: string) => request("/api/midea/forget", { method: "POST", body: { id } }),
  mideaControl: (id: string, state: Record<string, unknown>) =>
    request("/api/midea/control", { method: "POST", body: { id, state } }),
  mideaState: (id: string) => request<{ state: Record<string, unknown> }>(`/api/midea/state?id=${encodeURIComponent(id)}`),
  mideaSpecs: (id: string) => request(`/api/midea/specs?id=${encodeURIComponent(id)}`),
  mideaEnergy: (id: string, range: string) =>
    request<{ series?: unknown[]; range?: string }>(`/api/midea/energy?id=${encodeURIComponent(id)}&range=${encodeURIComponent(range)}`),

  voiceCommand: (text: string) => request("/api/voice/command", { method: "POST", body: { text } }),

  // Catalogo de funcoes do device: rotulos para a voz e acoes de "desligar
  // tudo". Derivado das capacidades quando nao foi configurado.
  deviceFunctions: (id: string) =>
    request<{ catalog: FunctionCatalog }>(`/api/devices/${encodeURIComponent(id)}/functions`),
  saveDeviceFunctions: (id: string, functions: DeviceFunction[]) =>
    request<{ catalog: FunctionCatalog }>(`/api/devices/${encodeURIComponent(id)}/functions`, {
      method: "PUT",
      body: { functions },
    }),
  // Restaura os padroes derivados (limpa a configuracao explicita).
  resetDeviceFunctions: (id: string) =>
    request<{ catalog: FunctionCatalog }>(`/api/devices/${encodeURIComponent(id)}/functions`, {
      method: "PUT",
      body: {},
    }),
};

export type Api = typeof realApi;

// `api` e um binding vivo: os testes trocam a implementacao inteira por um fake
// (setApi) sem precisar mockar modulo, e todos os importadores veem a troca.
export let api: Api = realApi;

export function setApi(next: Api | null): void {
  api = next ?? realApi;
}
