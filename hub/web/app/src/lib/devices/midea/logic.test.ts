import { describe, expect, it } from "vitest";
import { deviceRow, mideaDevice } from "$lib/test/fixtures";
import {
  buildMideaDevices,
  energyRangeLabel,
  MIDEA_ANGLES,
  MIDEA_BREEZE,
  MIDEA_FRESH_AIR,
  MIDEA_MODES,
  MIDEA_SPEEDS,
  mideaHasSpec,
  mideaLabels,
  mideaTargetTemp,
  mideaTelemetry,
  mideaTelemetryStats,
  mideaTempLimits,
} from "./logic";

describe("buildMideaDevices", () => {
  it("normaliza a lista do servico e mescla o registro do hub", () => {
    const devices = [
      mideaDevice({
        id: "a1",
        name: "Sala AC",
        ip: "10.0.0.9",
        model: "V3",
        room_id: "sala",
        template: "midea.ac",
        online: true,
        state: { power: true, target_temp: 22 },
        actions: [{ key: "power" }, { key: "target_temp" }],
      }),
      mideaDevice({ id: "a2", online: false, state: {}, actions: [] }),
    ];
    const rows = [deviceRow({ id: "a1", kind: "midea", room_id: "quarto", template: "midea.ac" })];

    const [a1, a2] = buildMideaDevices(devices, rows);

    expect(a1).toMatchObject({
      id: "a1",
      kind: "midea",
      name: "Sala AC",
      room: "quarto", // o registro do hub vence o room_id do aparelho
      template: "midea.ac",
      online: true,
      model: "V3",
      ip: "10.0.0.9",
      state: { power: true, target_temp: 22 },
    });
    expect(a1.specs).toHaveLength(2);
    expect(a1.specs[0].key).toBe("power");

    expect(a2).toMatchObject({ id: "a2", kind: "midea", room: "", online: false, state: {}, specs: [] });
  });

  it("usa room_id/template do proprio aparelho quando nao ha rows", () => {
    const [v] = buildMideaDevices([mideaDevice({ id: "x", room_id: "sala", template: "midea.ac", online: true })]);
    expect(v.room).toBe("sala");
    expect(v.template).toBe("midea.ac");
    expect(v.name).toBe("");
    expect(v.model).toBe("");
    expect(v.ip).toBe("");
  });

  it("aguenta lista vazia/indefinida", () => {
    expect(buildMideaDevices([])).toEqual([]);
  });
});

describe("mideaLabels", () => {
  it("traduz modo, velocidade, angulo, brisa e ar externo", () => {
    expect(mideaLabels(MIDEA_MODES, "auto")).toBe("Auto");
    expect(mideaLabels(MIDEA_MODES, "cool")).toBe("Frio");
    expect(mideaLabels(MIDEA_SPEEDS, "high")).toBe("Máx");
    expect(mideaLabels(MIDEA_SPEEDS, "full")).toBe("Turbo");
    expect(mideaLabels(MIDEA_ANGLES, 25)).toBe("Pos 2");
    expect(mideaLabels(MIDEA_ANGLES, 100)).toBe("Aberta");
    expect(mideaLabels(MIDEA_BREEZE, "breezeless")).toBe("Sem vento");
    expect(mideaLabels(MIDEA_FRESH_AIR, "boost")).toBe("Boost");
  });

  it("devolve vazio para valor desconhecido", () => {
    expect(mideaLabels(MIDEA_MODES, "nope")).toBe("");
    expect(mideaLabels(MIDEA_ANGLES, 999)).toBe("");
  });
});

describe("mideaTelemetry", () => {
  it("le temperatura ambiente/externa e potencia", () => {
    expect(mideaTelemetry({ indoor_temp: 22.5, outdoor_temp: 31, power: true })).toEqual({
      indoor: 22.5,
      outdoor: 31,
      power: true,
    });
  });

  it("usa null quando a leitura nao veio", () => {
    expect(mideaTelemetry({})).toEqual({ indoor: null, outdoor: null, power: false });
    expect(mideaTelemetry(undefined)).toEqual({ indoor: null, outdoor: null, power: false });
    expect(mideaTelemetry({ indoor_temp: "x" })).toEqual({ indoor: null, outdoor: null, power: false });
  });

  it("formata os tiles so quando ha leitura", () => {
    const stats = mideaTelemetryStats({ indoor_temp: 22.5, outdoor_temp: 31 });
    expect(stats.map((s) => [s.key, s.value, s.unit])).toEqual([
      ["indoor", "22.5", "°C"],
      ["outdoor", "31.0", "°C"],
    ]);
    expect(mideaTelemetryStats({})).toEqual([]);
  });
});

describe("specs / temperatura", () => {
  it("detecta funcao suportada", () => {
    expect(mideaHasSpec([{ key: "power" }, { key: "clean" }], "clean")).toBe(true);
    expect(mideaHasSpec([{ key: "power" }], "clean")).toBe(false);
    expect(mideaHasSpec(undefined, "clean")).toBe(false);
  });

  it("le limites com fallback 16..30", () => {
    expect(mideaTempLimits([{ key: "target_temp", options: { min: 17, max: 28 } }])).toEqual({ min: 17, max: 28 });
    expect(mideaTempLimits([])).toEqual({ min: 16, max: 30 });
  });

  it("limita e arredonda a temperatura alvo", () => {
    const specs = [{ key: "target_temp", options: { min: 16, max: 30 } }];
    expect(mideaTargetTemp({ target_temp: 22.4 }, specs)).toBe(22);
    expect(mideaTargetTemp({ target_temp: 99 }, specs)).toBe(30);
    expect(mideaTargetTemp({ target_temp: 1 }, specs)).toBe(16);
    expect(mideaTargetTemp({}, specs)).toBe(24);
  });
});

describe("energyRangeLabel", () => {
  it("rotula os intervalos do contador", () => {
    expect(energyRangeLabel("total")).toBe("Acumulado");
    expect(energyRangeLabel("run")).toBe("Ciclo atual");
    expect(energyRangeLabel(undefined)).toBe("Ciclo atual");
  });
});
