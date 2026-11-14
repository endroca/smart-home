// Testes do catalogo de acoes do "personalize a acao" do temporizador: um
// interruptor de 3 chaves expoe as tres, o ventilador expoe velocidade/desligar
// e a escolha vira a acao que o hub executa.

import { describe, expect, it } from "vitest";
import type { FanView } from "$lib/devices/fan/logic";
import type { MideaView } from "$lib/devices/midea/logic";
import { tuyaSpec } from "$lib/test/fixtures";
import type { HubDevice, TuyaSpec } from "$lib/types";
import {
  customTimer,
  effectiveKind,
  timerActionText,
  timerControls,
  timerDefaultValue,
  timerTarget,
  timerValues,
} from "./timerAction";

function gang(): HubDevice {
  return { id: "sw1", kind: "tuya", name: "Interruptor", room: "", online: true, template: "" };
}

const gangSpecs: TuyaSpec[] = [
  tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
  tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean" }),
  tuyaSpec({ dp_id: "3", code: "switch_3", type: "Boolean" }),
];

function fanView(): FanView {
  return {
    id: "rf01/fan0",
    kind: "fan",
    thing: "rf01",
    device: "fan0",
    name: "Ventilador",
    room: "",
    online: true,
    template: "",
    firmwareName: "",
    caps: {
      speed: { kind: "enum", writable: true },
      power: { kind: "boolean", writable: true },
    },
    actions: [],
    reported: {},
    source: "",
    updatedAt: 0,
    pendingManifest: false,
  };
}

function mideaView(): MideaView {
  return {
    id: "ac1",
    kind: "midea",
    name: "Ar",
    room: "",
    online: true,
    template: "",
    state: {},
    specs: [
      { key: "power", label: "Energia", type: "boolean", writable: true },
      { key: "mode", label: "Modo", type: "enum", writable: true, options: ["auto", "cool"] },
      { key: "target_temp", label: "Temperatura", type: "integer", writable: true, options: { min: 16, max: 30, step: 1 } },
    ],
    model: "",
    ip: "",
  };
}

describe("timerControls", () => {
  it("expõe as três chaves de um interruptor", () => {
    const controls = timerControls(gang(), gangSpecs);
    expect(controls.map((c) => c.value)).toEqual(["1", "2", "3"]);
    expect(controls.map((c) => c.label)).toEqual(["Chave 1", "Chave 2", "Chave 3"]);
  });

  it("expõe velocidade e desligar tudo do ventilador", () => {
    expect(timerControls(fanView(), [], fanView()).map((c) => c.value)).toEqual(["speed", "power"]);
    expect(timerValues(timerControls(fanView(), [], fanView())[0]).map((v) => v.value)).toEqual([
      "0",
      "1",
      "2",
      "3",
      "4",
      "5",
      "6",
    ]);
  });

  it("não inventa ações para device genérico", () => {
    expect(timerControls({ id: "x", kind: "generic", name: "Sensor", room: "", online: true, template: "" })).toEqual([]);
  });
});

describe("timerDefaultValue / effectiveKind", () => {
  it("bool começa ligado e enum no primeiro valor", () => {
    const [speed, power] = timerControls(fanView(), [], fanView());
    expect(timerDefaultValue(speed)).toBe("1");
    expect(timerDefaultValue(power)).toBe("false");
    expect(effectiveKind(speed)).toBe("number");
    expect(effectiveKind(power)).toBe("bool");
  });
});

describe("timerTarget", () => {
  it("monta a ação da chave 2 desligada", () => {
    const control = timerControls(gang(), gangSpecs).find((c) => c.value === "2")!;
    expect(timerTarget(gang(), control, "false")?.action).toEqual({
      kind: "tuya",
      device: "sw1",
      dp: "2",
      value: false,
    });
  });

  it("velocidade vira número no estado do ventilador", () => {
    const speed = timerControls(fanView(), [], fanView())[0];
    expect(timerTarget(fanView(), speed, "3")?.action).toEqual({
      kind: "fan",
      thing: "rf01",
      device_id: "fan0",
      state: { speed: 3 },
    });
  });

  it("ar-condicionado liga a energia", () => {
    const control = timerControls({ id: "ac1", kind: "midea", name: "Ar", room: "", online: true, template: "" }, [], undefined, mideaView()).find(
      (c) => c.value === "power",
    )!;
    expect(timerTarget(mideaView(), control, "true")?.action).toEqual({
      kind: "midea",
      device: "ac1",
      state: { power: true },
    });
  });
});

describe("timerActionText", () => {
  it("descreve ligar/desligar, níveis e modos", () => {
    const [speed, power] = timerControls(fanView(), [], fanView());
    expect(timerActionText(speed, "3")).toBe("Velocidade Ativo");
    expect(timerActionText(power, "false")).toBe("Desligar tudo");
    const chave = timerControls(gang(), gangSpecs)[1];
    expect(timerActionText(chave, "false")).toBe("Desligar Chave 2");
    const mode = timerControls(mideaView(), [], undefined, mideaView()).find((c) => c.value === "mode")!;
    expect(timerActionText(mode, "cool")).toBe("Modo Frio");
  });
});

describe("customTimer", () => {
  it("monta a volátil relativa com o nome do comando", () => {
    const chave = timerControls(gang(), gangSpecs)[1];
    const a = customTimer(gang(), chave, "false", { seconds: 1800 })!;
    expect(a.once).toBe(true);
    expect(a.source).toEqual({ type: "time", in_seconds: 1800 });
    expect(a.name).toBe("Interruptor: Desligar Chave 2 daqui a 30 min");
    expect(a.actions?.[0]).toEqual({ kind: "tuya", device: "sw1", dp: "2", value: false });
  });

  it("monta a volátil de horário", () => {
    const speed = timerControls(fanView(), [], fanView())[0];
    const a = customTimer(fanView(), speed, "3", { at: "07:00" })!;
    expect(a.source).toEqual({ type: "time", at: "07:00" });
    expect(a.name).toBe("Ventilador: Velocidade Ativo às 07:00");
  });

  it("não cria nada sem ação válida", () => {
    expect(customTimer(undefined, undefined, "", { seconds: 60 })).toBeNull();
  });
});