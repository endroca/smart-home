// Construtores de dados de exemplo para os testes. Os formatos seguem os
// contratos (contracts/*.schema.json) e as structs Go.

import type { Capability, DeviceRow, ManifestDevice, MideaDevice, Room, Thing, TuyaDevice, TuyaSpec } from "$lib/types";

export function room(id: string, name: string): Room {
  return { id, name };
}

export function cap(kind: string, extra: Partial<Capability> = {}): Capability {
  return { kind, ...extra };
}

export function manifestDevice(
  id: string,
  name: string,
  capabilities: Record<string, Capability> = {},
  actions: string[] = [],
): ManifestDevice {
  return { id, name, type: "fan", capabilities, actions };
}

export function thing(
  id: string,
  opts: {
    devices?: ManifestDevice[];
    reported?: Record<string, Record<string, unknown>>;
    online?: boolean;
    version?: string;
    actions?: string[];
    updatedAt?: string;
  } = {},
): Thing {
  const states: Thing["states"] = {};
  for (const [devId, reported] of Object.entries(opts.reported ?? {})) {
    states[devId] = { thing: id, id: devId, reported, updatedAt: opts.updatedAt };
  }
  return {
    id,
    manifest: {
      schemaVersion: 1,
      thing: id,
      model: "smart-control",
      firmware: { version: opts.version ?? "1.0.0" },
      actions: opts.actions ?? [],
      devices: opts.devices ?? [],
    },
    status: { online: opts.online ?? true, thing: id, version: opts.version ?? "1.0.0" },
    states,
    updatedAt: opts.updatedAt,
  };
}

export function deviceRow(partial: Partial<DeviceRow> & { id: string; kind: string }): DeviceRow {
  return { name: "", ip: "", port: 80, version: "", device22: false, ...partial };
}

export function tuyaSpec(partial: Partial<TuyaSpec> & { dp_id: string; code: string; type: string }): TuyaSpec {
  return { writable: true, ...partial };
}

export function tuyaDevice(partial: Partial<TuyaDevice> & { id: string }): TuyaDevice {
  return { name: "", ip: "", port: 6668, version: "3.3", has_key: true, online: true, ...partial };
}

export function mideaDevice(partial: Partial<MideaDevice> & { id: string }): MideaDevice {
  return { name: "", ip: "", port: 6445, has_key: true, online: true, registered: true, ...partial };
}

// ---- amostras prontas ----

export const sampleRooms = [room("sala", "Sala"), room("quarto", "Quarto")];

// Ventilador Ventisol com todas as capabilities.
export function sampleFanThing(): Thing {
  return thing("smart-control-rf01", {
    actions: ["fan.add"],
    devices: [
      manifestDevice(
        "fan0",
        "Ventilador",
        { power: cap("boolean"), speed: cap("enum"), light: cap("boolean"), direction: cap("enum") },
        ["learn.start", "fan.rename"],
      ),
    ],
    reported: { fan0: { power: true, speed: 3, light: false, direction: "forward", source: "rf" } },
    updatedAt: new Date().toISOString(),
  });
}

export function sampleTuyaSocket(): TuyaDevice {
  return tuyaDevice({
    id: "bf123",
    name: "Tomada",
    room_id: "sala",
    state: { "1": true, cur_power: 12.5, cur_voltage: 220 },
  });
}
