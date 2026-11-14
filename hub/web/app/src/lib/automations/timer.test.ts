// Testes da logica pura dos temporizadores volateis (timer.ts) e da conversao
// do editor para o gatilho de horario (logic.ts).

import { describe, expect, it } from "vitest";
import { tuyaSpec } from "$lib/test/fixtures";
import type { Automation, HubDevice } from "$lib/types";
import {
  clockTimer,
  fmtDuration,
  offTarget,
  onTarget,
  pairTimers,
  relativeTimer,
  remainingLabel,
  TIMER_PRESETS,
  timersFor,
  volatileLabel,
} from "./timer";
import { automationFromDraft, draftFromAutomation, sourceFromDraft, validateDraft } from "./logic";

function fan(): HubDevice {
  return { id: "rf01/fan0", kind: "fan", name: "Ventilador", thing: "rf01", device: "fan0", room: "", online: true, template: "" };
}

function tuya(): HubDevice {
  return {
    id: "bf123",
    kind: "tuya",
    name: "Tomada",
    room: "",
    online: true,
    template: "",
    specs: [tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" })],
  };
}

function midea(): HubDevice {
  return { id: "ac1", kind: "midea", name: "Ar", room: "", online: true, template: "" };
}

describe("offTarget", () => {
  it("resolve um ventilador para desligar tudo", () => {
    const t = offTarget(fan());
    expect(t?.driver).toBe("fan");
    expect(t?.action).toEqual({ kind: "fan", thing: "rf01", device_id: "fan0", state: { power: false } });
  });

  it("resolve uma tomada para o DP mestre em false", () => {
    const t = offTarget(tuya());
    expect(t?.action).toEqual({ kind: "tuya", device: "bf123", dp: "1", value: false });
  });

  it("resolve um ar-condicionado", () => {
    expect(offTarget(midea())?.action).toEqual({ kind: "midea", device: "ac1", state: { power: false } });
  });

  it("não inventa ação para um device genérico", () => {
    expect(offTarget({ id: "x", kind: "generic", name: "Sensor", room: "", online: true, template: "" })).toBeNull();
    expect(offTarget(undefined)).toBeNull();
  });
});

describe("onTarget", () => {
  it("ligar ventilador é escolher a velocidade 1", () => {
    expect(onTarget(fan())?.action).toEqual({ kind: "fan", thing: "rf01", device_id: "fan0", state: { speed: 1 } });
  });

  it("ligar tomada é o DP em true", () => {
    expect(onTarget(tuya())?.action).toEqual({ kind: "tuya", device: "bf123", dp: "1", value: true });
  });
});

describe("fmtDuration / remainingLabel", () => {
  it("formata segundos, minutos e horas", () => {
    expect(fmtDuration(45)).toBe("45 s");
    expect(fmtDuration(600)).toBe("10 min");
    expect(fmtDuration(5400)).toBe("1 h 30 min");
    expect(fmtDuration(7200)).toBe("2 h");
  });

  it("descreve o que falta até fire_at", () => {
    const now = Date.parse("2026-11-13T14:00:00-03:00");
    const fire = new Date(now + 9 * 60 * 1000).toISOString();
    expect(remainingLabel(fire, now)).toBe("em 9 min");
    expect(remainingLabel("", now)).toBe("");
    expect(remainingLabel(new Date(now - 5000).toISOString(), now)).toBe("agora");
  });
});

describe("construção das voláteis", () => {
  it("presets são 30 min, 1 h, 2 h e 4 h", () => {
    expect(TIMER_PRESETS.map((p) => p.label)).toEqual(["30 min", "1 h", "2 h", "4 h"]);
  });

  it("relativeTimer marca once e usa in_seconds", () => {
    const t = offTarget(tuya())!;
    const a = relativeTimer(t, 600);
    expect(a.once).toBe(true);
    expect(a.source).toEqual({ type: "time", in_seconds: 600 });
    expect(a.actions?.length).toBe(1);
  });

  it("relativeTimer usa o verbo no nome (ligar)", () => {
    const a = relativeTimer(onTarget(tuya())!, 1800, "Ligar");
    expect(a.name).toBe("Ligar Tomada daqui a 30 min");
    expect(a.actions?.[0]).toEqual({ kind: "tuya", device: "bf123", dp: "1", value: true });
  });

  it("clockTimer usa at", () => {
    const a = clockTimer(offTarget(fan())!, "14:00");
    expect(a.source).toEqual({ type: "time", at: "14:00" });
    expect(a.once).toBe(true);
  });

  it("clockTimer usa o verbo no nome (ligar)", () => {
    expect(clockTimer(onTarget(fan())!, "07:00", "Ligar").name).toBe("Ligar Ventilador às 07:00");
  });

  it("pairTimers cria ligar e desligar", () => {
    const pair = pairTimers(fan(), fan(), "10:00", "14:00");
    expect(pair).toHaveLength(2);
    expect(pair[0].source?.at).toBe("10:00");
    expect(pair[1].source?.at).toBe("14:00");
  });
});

describe("timersFor / volatileLabel", () => {
  const device = tuya();
  const volatile: Automation = {
    id: "t1",
    name: "Desliga daqui a 10 min",
    enabled: true,
    once: true,
    source: { type: "time", fire_at: "2026-11-13T14:09:00-03:00" },
    actions: [{ kind: "tuya", device: "bf123", dp: "1", value: false }],
  };

  it("encontra a volátil do device", () => {
    expect(timersFor(device, [volatile]).map((a) => a.id)).toEqual(["t1"]);
    expect(timersFor(midea(), [volatile])).toHaveLength(0);
  });

  it("ignora automações que não são voláteis de horário", () => {
    const event: Automation = { id: "e1", once: false, source: { type: "tuya_dp" }, actions: [{ kind: "tuya", device: "bf123", dp: "1", value: true }] };
    expect(timersFor(device, [event])).toHaveLength(0);
  });

  it("resume o tempo restante", () => {
    const now = Date.parse("2026-11-13T14:00:00-03:00");
    expect(volatileLabel(volatile, now)).toBe("Volátil · em 9 min");
  });
});

describe("editor: gatilho de horário", () => {
  function timeDraft() {
    const d = draftFromAutomation({
      id: "t1",
      name: "Desliga",
      enabled: true,
      once: true,
      source: { type: "time", in_seconds: 600 },
      actions: [{ kind: "tuya", device: "bf123", dp: "1", value: false }],
    });
    return d;
  }

  it("carrega um gatilho relativo", () => {
    const d = timeDraft();
    expect(d.triggerKind).toBe("time");
    expect(d.timeMode).toBe("relative");
    expect(d.timeSeconds).toBe("10");
    expect(d.timeUnit).toBe("minutes");
    expect(d.once).toBe(true);
  });

  it("gera uma fonte de horário relativa", () => {
    const d = timeDraft();
    const src = sourceFromDraft(d, []);
    expect(src.type).toBe("time");
    expect(src.in_seconds).toBe(600);
    const body = automationFromDraft(d, [], "t1");
    expect(body.once).toBe(true);
    expect(body.source?.type).toBe("time");
  });

  it("valida o gatilho de horário sem dispositivo", () => {
    const d = timeDraft();
    d.name = "Desliga";
    d.actions = [{ kind: "tuya", target: "bf123", control: "1", controlKind: "bool", value: "false" }];
    d.timeSeconds = "0";
    expect(validateDraft(d, [])).toMatch(/maior que zero/);
    d.timeSeconds = "30";
    expect(validateDraft(d, [])).toBeNull();
  });
});
