// Testes da logica pura de automacoes: catalogo, conversao e rotulos.

import { describe, expect, it } from "vitest";
import { buildFans } from "$lib/devices/fan/logic";
import { buildMideaDevices } from "$lib/devices/midea/logic";
import { buildTuyaDevices } from "$lib/devices/tuya/logic";
import {
  cap,
  deviceRow,
  manifestDevice,
  mideaDevice,
  sampleRooms,
  thing,
  tuyaDevice,
  tuyaSpec,
} from "$lib/test/fixtures";
import type { Automation, MideaSpec, TuyaSpec } from "$lib/types";
import {
  actionsSummary,
  automationFromDraft,
  buildDeviceContracts,
  contractKey,
  draftFromAutomation,
  emptyDraft,
  mideaEventOptions,
  scheduleLabel,
  sourceFromDraft,
  sourceSummary,
  tuyaEventOptions,
  validateDraft,
} from "./logic";

const lock = tuyaDevice({ id: "lock1", name: "Porta", category: "jtmspro" });

const lockSpecs: TuyaSpec[] = [
  tuyaSpec({ dp_id: "1", code: "unlock_fingerprint", type: "Enum", writable: false }),
  tuyaSpec({ dp_id: "2", code: "unlock_password", type: "Enum", writable: false }),
  tuyaSpec({ dp_id: "3", code: "alarm_lock", type: "Enum", writable: false }),
];

const switchSpecs: TuyaSpec[] = [
  tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Chave 1" }),
  tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean", name: "Chave 2" }),
];

const midea = mideaDevice({
  id: "ac1",
  name: "Ar da sala",
  actions: [
    { key: "power", label: "Energia", type: "boolean", writable: true } as MideaSpec,
    { key: "mode", label: "Modo", type: "enum", writable: true, options: ["auto", "cool", "heat"] } as MideaSpec,
    { key: "target_temp", label: "Temperatura", type: "integer", writable: true, options: { min: 16, max: 30, step: 1 } } as MideaSpec,
  ],
});

describe("catalogo de eventos", () => {
  it("agrupa os métodos de abertura de uma fechadura", () => {
    const events = tuyaEventOptions(lock, lockSpecs);
    expect(events[0]).toMatchObject({
      value: "__unlock__",
      label: "Abertura (qualquer método)",
      kind: "event",
      dps: ["1", "2"],
    });
    expect(events.some((e) => e.value === "1" && e.label === "Abertura com digital")).toBe(true);
    expect(events.some((e) => e.value === "3" && e.label === "Alarme")).toBe(true);
  });

  it("lista as chaves de um interruptor pelo nome", () => {
    const events = tuyaEventOptions(tuyaDevice({ id: "sw1" }), switchSpecs);
    expect(events.find((e) => e.value === "1")?.label).toBe("Chave 1");
    expect(events.find((e) => e.value === "2")?.kind).toBe("bool");
  });

  it("lista potência, modo e temperatura do ar-condicionado", () => {
    const events = mideaEventOptions(midea);
    expect(events.find((e) => e.value === "power")?.kind).toBe("bool");
    expect(events.find((e) => e.value === "mode")?.values?.map((v) => v.value)).toEqual(["auto", "cool", "heat"]);
    expect(events.find((e) => e.value === "target_temp")?.min).toBe(16);
  });
});

describe("contrato unico de dispositivo", () => {
  const fanThing = thing("rf01", {
    devices: [
      manifestDevice("fan0", "Ventilador", {
        power: cap("switch"),
        light: cap("switch"),
        speed: cap("enum"),
      }),
    ],
    reported: { fan0: { power: true, light: false, speed: 2 } },
  });
  const fanRows = [
    deviceRow({ id: "rf01/fan0", kind: "fan", owner: "rf01", device_id: "fan0", name: "Ventilador", room_id: "sala" }),
  ];

  it("representa ventilador, Tuya e Midea com as mesmas listas", () => {
    const fans = buildFans([fanThing], fanRows);
    const tuya = buildTuyaDevices(
      [tuyaDevice({ id: "sw1", name: "Interruptor", room_id: "sala" })],
      { sw1: switchSpecs },
      [],
      [],
    );
    const mideaViews = buildMideaDevices([midea], []);
    const contracts = buildDeviceContracts(fans, tuya, mideaViews, { sw1: switchSpecs }, sampleRooms);

    const fan = contracts.find((c) => c.driver === "fan");
    expect(fan?.key).toBe(contractKey("fan", "rf01/fan0"));
    expect(fan?.sourceType).toBe("thing_state");
    expect(fan?.events.find((e) => e.value === "light")?.label).toBe("Luz");
    expect(fan?.actions.find((a) => a.value === "light")?.kind).toBe("bool");
    expect(fan?.actions.find((a) => a.value === "speed")?.defaultValue).toBe("1");
    expect(fan?.actions.find((a) => a.value === "power")?.values).toEqual([
      { value: "false", label: "Desligar tudo" },
    ]);

    const tuyaContract = contracts.find((c) => c.driver === "tuya");
    expect(tuyaContract?.sourceType).toBe("tuya_dp");
    expect(tuyaContract?.events.find((e) => e.value === "1")?.label).toBe("Chave 1");

    const mideaContract = contracts.find((c) => c.driver === "midea");
    expect(mideaContract?.sourceType).toBe("midea_state");
    expect(mideaContract?.events.find((e) => e.value === "power")?.kind).toBe("bool");
    expect(mideaContract?.actions.find((a) => a.value === "mode")?.values?.length).toBe(3);
  });

  it("converte o gatilho de ventilador para thing_state", () => {
    const original: Automation = {
      id: "a3",
      name: "Luz do ventilador",
      enabled: true,
      source: { type: "thing_state", device: "rf01/fan0", dp: "light", value: true },
      actions: [{ kind: "tuya", device: "lamp1", dp: "1", value: true }],
    };
    const draft = draftFromAutomation(original);
    expect(draft.triggerKind).toBe("thing_state");
    expect(draft.triggerEvent).toBe("light");
    expect(draft.condition).toBe("true");

    const saved = automationFromDraft(draft, [], "a3");
    expect(saved.source).toMatchObject({
      type: "thing_state",
      device: "rf01/fan0",
      dp: "light",
      value: true,
    });
  });
});

describe("conversao rascunho <-> JSON", () => {
  it("preserva o grupo de abertura e a janela de horário", () => {
    const original: Automation = {
      id: "a1",
      name: "Porta à noite",
      enabled: true,
      source: { type: "tuya_dp", device: "lock1", dps: ["1", "2"] },
      schedule: { start: "18:00", end: "03:00", days: [5] },
      actions: [{ kind: "tuya", device: "lamp1", dp: "1", value: true }],
    };

    const draft = draftFromAutomation(original);
    expect(draft.triggerEvent).toBe("__unlock__");
    expect(draft.scheduleEnabled).toBe(true);
    expect(draft.scheduleDays).toEqual([5]);
    draft.delayEnabled = true;
    draft.delayMs = "2000";

    const saved = automationFromDraft(draft, tuyaEventOptions(lock, lockSpecs), "a1");
    expect(saved.source).toMatchObject({ type: "tuya_dp", device: "lock1", dps: ["1", "2"] });
    expect(saved.source?.dp).toBeUndefined();
    expect(saved.schedule).toEqual({ start: "18:00", end: "03:00", days: [5] });
    expect(saved.delay).toEqual({ enabled: true, ms: 2000 });
    expect(saved.actions).toEqual([{ kind: "tuya", device: "lamp1", dp: "1", value: true }]);
  });

  it("monta o gatilho de interruptor com valor booleano", () => {
    const draft = emptyDraft();
    draft.name = "Interruptor liga a luz";
    draft.triggerKind = "tuya_dp";
    draft.triggerDevice = "sw1";
    draft.triggerEvent = "1";
    draft.condition = "true";
    draft.actions = [{ kind: "tuya", target: "lamp1", control: "1", controlKind: "bool", value: "true" }];

    const source = sourceFromDraft(draft, tuyaEventOptions(tuyaDevice({ id: "sw1" }), switchSpecs));
    expect(source).toMatchObject({ type: "tuya_dp", device: "sw1", dp: "1", value: true });
    expect(source.dps).toBeUndefined();
  });

  it("converte um power:true legado para velocidade", () => {
    const draft = draftFromAutomation({
      id: "a2",
      name: "Ventilador",
      enabled: true,
      source: { type: "manual" },
      actions: [{ kind: "fan", thing: "rf01", device_id: "fan0", state: { power: true, speed: 3 } }],
    });
    const saved = automationFromDraft(draft, []);
    expect(saved.actions).toEqual([
      { kind: "fan", thing: "rf01", device_id: "fan0", state: { speed: 3 } },
    ]);
  });

  it("converte power:true sem velocidade para velocidade 1", () => {
    const draft = draftFromAutomation({
      id: "a4",
      name: "Ventilador",
      enabled: true,
      source: { type: "manual" },
      actions: [{ kind: "fan", thing: "rf01", device_id: "fan0", state: { power: true } }],
    });
    const saved = automationFromDraft(draft, []);
    expect(saved.actions).toEqual([
      { kind: "fan", thing: "rf01", device_id: "fan0", state: { speed: 1 } },
    ]);
  });

  it("ignora o período de uma cena manual", () => {
    const draft = emptyDraft();
    draft.name = "Cena";
    draft.triggerKind = "manual";
    draft.scheduleEnabled = true;
    draft.actions = [{ kind: "tuya", target: "lamp1", control: "1", controlKind: "bool", value: "true" }];
    const saved = automationFromDraft(draft, []);
    expect(saved.schedule).toBeUndefined();
  });
});

describe("rotulos e validacao", () => {
  it("resume uma janela que atravessa a meia-noite", () => {
    expect(scheduleLabel({ start: "18:00", end: "03:00", days: [1, 2, 3, 4, 5] })).toBe(
      "18:00 → 03:00 · seg–sex",
    );
    expect(scheduleLabel(undefined)).toBe("Sempre");
  });

  it("descreve o gatilho e a acao", () => {
    const source = { type: "tuya_dp", device: "lock1", dps: ["1", "2"] };
    const text = sourceSummary(source, tuyaEventOptions(lock, lockSpecs), {
      deviceName: () => "Porta",
    });
    expect(text).toBe("Porta · Abertura (qualquer método)");

    const action = actionsSummary([{ kind: "tuya", device: "lamp1", dp: "1", value: true }], {
      deviceName: () => "Luz da sala",
      tuyaDP: () => "Chave",
    });
    expect(action).toBe("Luz da sala · Chave ligar");
  });

  it("exige dispositivo, evento e acao", () => {
    const draft = emptyDraft();
    draft.name = "";
    expect(validateDraft(draft, [])).toContain("nome");

    draft.name = "Teste";
    draft.triggerKind = "tuya_dp";
    draft.actions = [{ kind: "tuya", target: "lamp1", control: "1", controlKind: "bool", value: "true" }];
    expect(validateDraft(draft, [])).toContain("dispositivo");

    draft.triggerDevice = "sw1";
    draft.triggerEvent = "1";
    expect(validateDraft(draft, tuyaEventOptions(tuyaDevice({ id: "sw1" }), switchSpecs))).toBeNull();
  });
});

describe("editor: agendamento semanal", () => {
  function weeklyDraft() {
    const d = emptyDraft();
    d.name = "Toda segunda";
    d.triggerKind = "time";
    d.timeMode = "clock";
    d.timeAt = "18:00";
    d.actions = [{ kind: "tuya", target: "bf123", control: "1", controlKind: "bool", value: "true" }];
    return d;
  }

  it("com dias marcados gera source.days e não é volátil", () => {
    const d = weeklyDraft();
    d.timeDays = [1, 3];
    const body = automationFromDraft(d, []);
    expect(body.once).toBeUndefined();
    expect(body.source).toMatchObject({ type: "time", at: "18:00", days: [1, 3] });
  });

  it("sem dias continua volátil (once) e sem days", () => {
    const d = weeklyDraft();
    d.timeDays = [];
    const body = automationFromDraft(d, []);
    expect(body.once).toBe(true);
    expect(body.source?.days).toBeUndefined();
  });

  it("o modo relativo ignora os dias", () => {
    const d = weeklyDraft();
    d.timeMode = "relative";
    d.timeSeconds = "15";
    d.timeDays = [1, 3];
    const src = sourceFromDraft(d, []);
    expect(src.in_seconds).toBe(900);
    expect(src.days).toBeUndefined();
  });

  it("carrega os dias do JSON salvo", () => {
    const d = draftFromAutomation({
      id: "w",
      name: "Toda segunda",
      enabled: true,
      source: { type: "time", at: "18:00", days: [1, 3] },
      actions: [{ kind: "tuya", device: "bf123", dp: "1", value: true }],
    });
    expect(d.timeMode).toBe("clock");
    expect(d.timeAt).toBe("18:00");
    expect(d.timeDays).toEqual([1, 3]);
  });

  it("resume o gatilho semanal", () => {
    expect(sourceSummary({ type: "time", at: "18:00", days: [1, 3] }, [])).toBe("Seg, Qua · às 18:00");
    expect(sourceSummary({ type: "time", at: "18:00" }, [])).toBe("às 18:00");
  });
});