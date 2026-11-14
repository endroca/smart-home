// Estado global do hub: uma unica fonte reativa, alimentada pelo fetch inicial,
// pelo SSE (/api/events) e por um poll de fallback. Os componentes leem daqui;
// nenhum deles reconstroi a tela a mao.

import { api } from "$lib/api";
import { toast } from "$lib/stores/toast.svelte";
import type {
  Automation,
  DeviceRow,
  FirmwareInfo,
  Health,
  HubEvent,
  MideaDevice,
  MideaScanDevice,
  Room,
  Thing,
  ThingStatus,
  TuyaDevice,
  TuyaScanDevice,
  TuyaSpec,
} from "$lib/types";

export interface TuyaLast {
  at?: string;
  kind?: string;
  dps?: Record<string, unknown>;
}

const POLL_MS = 30000;

class HubData {
  health = $state<Health>({});
  things = $state<Thing[]>([]);
  rooms = $state<Room[]>([]);
  devices = $state<DeviceRow[]>([]);
  automations = $state<Automation[]>([]);
  firmware = $state<FirmwareInfo>({ files: [], pending: {} });
  specs = $state<Record<string, TuyaSpec[]>>({});

  tuya = $state({
    available: true as boolean | undefined,
    devices: [] as TuyaDevice[],
    listening: [] as string[],
    cloud: false,
    scan: [] as TuyaScanDevice[],
    last: {} as Record<string, TuyaLast>,
  });

  midea = $state({
    available: true as boolean | undefined,
    devices: [] as MideaDevice[],
    cloud: false,
    scan: [] as MideaScanDevice[],
    scanTarget: "",
  });

  // Progresso de OTA por gateway (alimentado pelo evento ota.progress).
  otaProgress = $state<Record<string, { percent: number; version?: string }>>({});

  loading = $state(false);
  loaded = $state(false);

  #es: EventSource | null = null;
  #reconnect: ReturnType<typeof setTimeout> | null = null;
  #poll: ReturnType<typeof setInterval> | null = null;

  // load busca o snapshot completo. `force` ignora o guard de reentrancia.
  async load(force = false) {
    if (this.loading && !force) return;
    this.loading = true;
    try {
      this.health = await api.health();
      const [th, rm, dv, au] = await Promise.all([api.things(), api.rooms(), api.devices(), api.automations()]);
      this.things = th.things ?? [];
      this.rooms = rm.rooms ?? [];
      this.devices = dv.devices ?? [];
      this.automations = au.automations ?? [];

      try {
        this.firmware = await api.firmware();
      } catch {
        this.firmware = { files: [], pending: {} };
      }

      if (this.tuya.available !== false) {
        try {
          const r = await api.tuyaDevices();
          this.tuya.available = true;
          this.tuya.devices = r.devices ?? [];
          this.tuya.listening = r.listening ?? [];
          this.tuya.cloud = !!r.cloud;
        } catch {
          this.tuya.available = false;
        }
      }

      if (this.midea.available !== false) {
        try {
          const r = await api.mideaDevices();
          this.midea.available = true;
          this.midea.devices = r.devices ?? [];
          this.midea.cloud = !!r.cloud;
        } catch {
          this.midea.available = false;
        }
      }

      this.loaded = true;
    } catch (e) {
      // Uma carga interrompida (rede/servidor fora) nunca pode virar unhandled
      // rejection: o estado anterior permanece e o erro vira toast.
      if ((e as Error)?.name !== "AbortError") toast.show((e as Error).message, true);
    } finally {
      this.loading = false;
    }
  }

  // refreshRegistry re-le os things (manifesto) e o registro de devices, sem
  // reabrir o SSE.
  async refreshRegistry() {
    try {
      const [th, dv] = await Promise.all([api.things(), api.devices()]);
      this.things = th.things ?? [];
      this.devices = dv.devices ?? [];
    } catch {
      /* mantem o ultimo snapshot */
    }
  }

  // loadAutomations re-le so a lista de automacoes (usado quando o hub avisa,
  // por SSE, que uma volatil disparou e foi removida).
  async loadAutomations() {
    try {
      const au = await api.automations();
      this.automations = au.automations ?? [];
    } catch {
      /* mantem a ultima lista */
    }
  }

  findThing(id: string): Thing | undefined {
    return this.things.find((t) => t.id === id);
  }

  updateThingState(thing: string, deviceID: string, reported: Record<string, unknown>) {
    const t = this.findThing(thing);
    if (!t || !deviceID) return;
    t.states ??= {};
    const sh = t.states[deviceID] ?? (t.states[deviceID] = {});
    sh.reported = { ...(sh.reported ?? {}), ...reported };
  }

  updateThingStatus(thing: string, status: ThingStatus) {
    const t = this.findThing(thing);
    if (t && status) t.status = { ...(t.status ?? {}), ...status };
  }

  updateThingEvent(thing: string, event: Record<string, unknown>) {
    const t = this.findThing(thing);
    if (t && event) t.lastEvent = event;
  }

  updateTuyaState(id: string, dps: Record<string, unknown>) {
    const d = this.tuya.devices.find((x) => x.id === id);
    if (d) d.state = { ...(d.state ?? {}), ...dps };
  }

  updateMideaState(id: string, state: Record<string, unknown>) {
    const d = this.midea.devices.find((x) => x.id === id);
    if (!d) return;
    d.state = { ...(d.state ?? {}), ...state };
    d.online = true;
  }

  applyEvent(ev: HubEvent) {
    const d = ev.data ?? {};
    switch (ev.type) {
      case "tuya":
        if (d.device && d.dps) {
          this.updateTuyaState(d.device, d.dps);
          if (d.kind && d.kind !== "state") {
            this.tuya.last[d.device] = { at: d.at, kind: d.kind, dps: d.dps };
          }
        }
        break;
      case "midea":
        if (d.device && d.state) this.updateMideaState(d.device, d.state);
        break;
      case "state":
        if (d.thing && d.id && d.reported) this.updateThingState(d.thing, d.id, d.reported);
        break;
      case "status":
        if (d.thing && d.status) this.updateThingStatus(d.thing, d.status);
        break;
      case "manifest":
      case "fan":
        void this.refreshRegistry();
        break;
      case "automations":
        void this.loadAutomations();
        break;
      case "event": {
        if (!d.thing || !d.event) break;
        this.updateThingEvent(d.thing, d.event);
        const name = d.event["event"];
        if (name === "ota.progress") {
          this.otaProgress[d.thing] = {
            percent: Number(d.event["percent"]) || 0,
            version: typeof d.event["version"] === "string" ? d.event["version"] : undefined,
          };
        } else if (name === "ota.done") {
          delete this.otaProgress[d.thing];
        }
        break;
      }
    }
  }

  connectEvents() {
    if (typeof EventSource === "undefined") return;
    if (this.#es) return;
    this.#es = new EventSource("/api/events");
    this.#es.onmessage = (m) => {
      try {
        this.applyEvent(JSON.parse(m.data) as HubEvent);
      } catch {
        /* ignora payload invalido */
      }
    };
    this.#es.onerror = () => {
      this.#es?.close();
      this.#es = null;
      if (this.#reconnect) clearTimeout(this.#reconnect);
      this.#reconnect = setTimeout(() => this.connectEvents(), 3000);
    };
  }

  startPolling(ms = POLL_MS) {
    if (this.#poll) return;
    this.#poll = setInterval(() => void this.load(), ms);
  }

  stop() {
    this.#es?.close();
    this.#es = null;
    if (this.#reconnect) clearTimeout(this.#reconnect);
    if (this.#poll) clearInterval(this.#poll);
    this.#poll = null;
  }

  // loadSpecsForDevices carrega os specs dos devices Tuya que ainda nao tem.
  async loadSpecsForDevices() {
    const pending = (this.tuya.devices ?? []).filter((d) => !this.specs[d.id]);
    if (!pending.length) return;
    const results = await Promise.allSettled(
      pending.map(async (d) => {
        const r = await api.tuyaSpecs(d.id);
        return [d.id, r.specs ?? []] as const;
      }),
    );
    for (const r of results) {
      if (r.status === "fulfilled") this.specs[r.value[0]] = r.value[1];
    }
  }
}

export const hub = new HubData();
