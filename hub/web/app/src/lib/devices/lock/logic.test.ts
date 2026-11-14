// Testes da logica pura da fechadura: categoria, bateria, alarme, metodos e a
// inferencia do layout.

import { describe, expect, it } from "vitest";
import { tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import type { TuyaSpec } from "$lib/types";
import { buildTuyaDevices } from "../tuya/logic";
import { resolveTemplate } from "../registry";
import "../index";
import { isLockCategory, lockAlarm, lockBattery, lockFlags, lockMethods, unlockCodeLabel, type LockView } from "./logic";

const lockSpecs = (): TuyaSpec[] => [
  tuyaSpec({ dp_id: "12", code: "residual_electricity", type: "Integer", writable: false }),
  tuyaSpec({ dp_id: "1", code: "unlock_fingerprint", type: "Integer", writable: false }),
  tuyaSpec({ dp_id: "2", code: "unlock_password", type: "Integer", writable: false }),
  tuyaSpec({ dp_id: "8", code: "alarm_lock", type: "Enum", writable: false }),
  tuyaSpec({ dp_id: "13", code: "reverse_lock", type: "Boolean", writable: false }),
  tuyaSpec({ dp_id: "16", code: "hijack", type: "Boolean", writable: false }),
  tuyaSpec({ dp_id: "19", code: "doorbell", type: "Boolean", writable: false }),
];

function lockView(state: Record<string, unknown>, specs: TuyaSpec[] = lockSpecs()): LockView {
  const dev = tuyaDevice({ id: "lock1", name: "Fechadura", category: "jtmspro", cloud_only: true, state });
  return buildTuyaDevices([dev], { lock1: specs })[0] as LockView;
}

describe("isLockCategory", () => {
  it("reconhece as categorias de fechadura (case-insensitive)", () => {
    expect(isLockCategory("jtmspro")).toBe(true);
    expect(isLockCategory(" JTMSPRO ")).toBe(true);
    expect(isLockCategory("ms")).toBe(true);
    expect(isLockCategory("cz")).toBe(false);
    expect(isLockCategory(undefined)).toBe(false);
  });
});

describe("buildTuyaDevices pass-through", () => {
  it("carrega a categoria e o flag de nuvem", () => {
    const v = lockView({ "12": 70 });
    expect(v.category).toBe("jtmspro");
    expect(v.cloud_only).toBe(true);
  });

  it("a inferencia escolhe o layout da fechadura", () => {
    expect(resolveTemplate(lockView({ "12": 70 })).id).toBe("tuya.lock");
  });
});

describe("lockBattery", () => {
  it("le a carga restante", () => {
    expect(lockBattery(lockView({ "12": 70 }))).toBe(70);
  });
  it("devolve null quando desconhecida", () => {
    expect(lockBattery(lockView({}))).toBeNull();
  });
});

describe("lockAlarm", () => {
  it("traduz o enum para pt-BR", () => {
    expect(lockAlarm(lockView({ "8": "pry" }))).toEqual({ code: "pry", label: "Arrombamento" });
  });
  it("devolve null sem alarme", () => {
    expect(lockAlarm(lockView({ "8": "" }))).toBeNull();
    expect(lockAlarm(lockView({}))).toBeNull();
  });
});

describe("lockMethods", () => {
  it("lista so os metodos presentes, com o ultimo id", () => {
    const methods = lockMethods(lockView({ "1": 11, "2": 2 }));
    expect(methods.map((m) => m.label)).toEqual(["Digital", "Senha"]);
    expect(methods.map((m) => m.value)).toEqual([11, 2]);
  });
});

describe("lockFlags", () => {
  it("le os indicadores booleanos", () => {
    expect(lockFlags(lockView({ "13": true, "16": false, "19": true }))).toEqual({
      reverse: true,
      hijack: false,
      doorbell: true,
    });
  });
});

describe("unlockCodeLabel", () => {
  it("traduz os codigos conhecidos", () => {
    expect(unlockCodeLabel("unlock_fingerprint")).toBe("Digital");
    expect(unlockCodeLabel("unlock_password")).toBe("Senha");
  });
  it("cai para o codigo cru e para um rotulo generico", () => {
    expect(unlockCodeLabel("desconhecido")).toBe("desconhecido");
    expect(unlockCodeLabel("")).toBe("Abertura");
  });
});
