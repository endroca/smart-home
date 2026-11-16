// Regressao do problema relatado: o campo da configuracao NAO pode ser
// re-renderizado por uma atualizacao que nao e dele.
//
// O hub reenvia o estado a cada poucos segundos (o aparelho reenvia o conjunto
// completo de DPs) e o `load()` reconcilia a cada 30 s. Se qualquer uma dessas
// atualizacoes recriasse os objetos do device, a arvore de componentes seria
// invalidada inteira e o input de um device sem relacao com o update sofreria
// re-render. Aqui a prova e direta: espionamos as escritas em `input.value` e
// exigimos ZERO quando o update nao e daquele campo.

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { setApi } from "$lib/api";
import { hub } from "$lib/stores/data.svelte";
import { nav } from "$lib/stores/nav.svelte";
import { toast } from "$lib/stores/toast.svelte";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import { deviceRow, tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import type { TuyaSpec } from "$lib/types";
import "$lib/devices/index";
import DeviceDetail from "./DeviceDetail.svelte";

// O conjunto que o "Interruptor-Inteligente-lite-3-bot-es" reenvia sem parar.
const DPS: Record<string, unknown> = {
  "1": false,
  "2": true,
  "3": false,
  "14": "off",
  "15": "relay",
  "16": true,
  "17": "",
  "7": 0,
};

const SPECS = (): TuyaSpec[] => [
  tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
  tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean" }),
  tuyaSpec({ dp_id: "3", code: "switch_3", type: "Boolean" }),
  tuyaSpec({ dp_id: "16", code: "switch_backlight", type: "Boolean" }),
  tuyaSpec({ dp_id: "14", code: "relay_status", type: "Enum", values: { range: ["power_off", "power_on", "last"] } }),
  tuyaSpec({ dp_id: "15", code: "light_mode", type: "Enum", values: { range: ["relay", "pos", "none"] } }),
  tuyaSpec({ dp_id: "17", code: "cycle_time", type: "String" }),
  tuyaSpec({ dp_id: "7", code: "countdown_1", type: "Integer", values: { min: 0, max: 86400, step: 1 } }),
];

let fake: FakeApi;

function seedHub() {
  hub.rooms = [{ id: "sala", name: "Sala" }];
  hub.devices = [
    deviceRow({ id: "sw3", kind: "tuya", name: "Interruptor", room_id: "sala" }),
    deviceRow({ id: "outro", kind: "tuya", name: "Tomada", room_id: "sala" }),
  ];
  hub.tuya.devices = [
    tuyaDevice({ id: "sw3", name: "Interruptor", state: { ...DPS } }),
    tuyaDevice({ id: "outro", name: "Tomada", state: { "1": true } }),
  ];
  hub.tuya.available = true;
  hub.specs = { sw3: SPECS(), outro: [tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" })] };
}

beforeEach(() => {
  fake = makeFakeApi({
    health: { ok: true },
    things: { things: [] },
    rooms: { rooms: [{ id: "sala", name: "Sala" }] },
    devices: {
      devices: [
        deviceRow({ id: "sw3", kind: "tuya", name: "Interruptor", room_id: "sala" }),
        deviceRow({ id: "outro", kind: "tuya", name: "Tomada", room_id: "sala" }),
      ],
    },
    automations: { automations: [] },
    firmware: { files: [], pending: {} },
    tuyaDevices: {
      devices: [
        tuyaDevice({ id: "sw3", name: "Interruptor", state: { ...DPS } }),
        tuyaDevice({ id: "outro", name: "Tomada", state: { "1": true } }),
      ],
      cloud: false,
      listening: [],
    },
    mideaDevices: { devices: [], cloud: false },
    deviceFunctions: { catalog: { device_id: "sw3", driver: "tuya", configured: false, functions: [] } },
  });
  setApi(fake.api);
  seedHub();
  nav.page = "settings";
  nav.detail = { type: "device", id: "sw3" };
});

afterEach(() => {
  setApi(null);
  nav.detail = null;
});

/** tick espera o proximo ciclo de microtarefas + render. */
const tick = () => new Promise((r) => setTimeout(r, 20));

/** spyValueWrites conta quantas vezes o `value` de um input e escrito no DOM. */
function spyValueWrites() {
  const desc = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value");
  if (!desc?.set) throw new Error("sem setter de value");
  const writes: string[] = [];
  Object.defineProperty(HTMLInputElement.prototype, "value", {
    configurable: true,
    get: desc.get,
    set(this: HTMLInputElement, v: string) {
      writes.push(String(v));
      desc.set?.call(this, v);
    },
  });
  return {
    writes,
    clear: () => {
      writes.length = 0;
    },
    restore: () => Object.defineProperty(HTMLInputElement.prototype, "value", desc),
  };
}

function rowOf(name: string): HTMLElement {
  const row = screen.getByText(name).closest("div")?.parentElement?.parentElement;
  if (!row) throw new Error(`linha de ${name} nao encontrada`);
  return row as HTMLElement;
}

const textField = (name: string) => rowOf(name).querySelector("input") as HTMLInputElement;
const enumSelect = (name: string) => rowOf(name).querySelector("select") as HTMLSelectElement;

/** snapshot reproduz o que o SSE entrega. */
async function snapshot(device: string, dps: Record<string, unknown>) {
  hub.applyEvent({ type: "tuya", data: { device, dps: { ...dps }, kind: "dp", at: new Date().toISOString() } });
  await tick();
}

describe("o campo de configuracao nao sofre re-render alheio", () => {
  it("um DP de OUTRO device nao toca no input", async () => {
    const user = userEvent.setup();
    render(DeviceDetail);
    const input = textField("Tempo de ciclo");
    await user.type(input, "30");
    expect(input.value).toBe("30");

    const spy = spyValueWrites();
    try {
      await snapshot("outro", { "1": false });
      await snapshot("outro", { "1": true });
      expect(spy.writes).toEqual([]);
      expect(textField("Tempo de ciclo")).toBe(input);
      expect(input.value).toBe("30");
    } finally {
      spy.restore();
    }
  });

  it("um snapshot identico do PROPRIO device nao toca no input", async () => {
    const user = userEvent.setup();
    render(DeviceDetail);
    const input = textField("Tempo de ciclo");
    await user.type(input, "30");

    const spy = spyValueWrites();
    try {
      for (let i = 0; i < 5; i++) await snapshot("sw3", DPS);
      expect(spy.writes).toEqual([]);
    } finally {
      spy.restore();
    }
  });

  it("outro DP do mesmo device nao toca no input", async () => {
    const user = userEvent.setup();
    render(DeviceDetail);
    const input = textField("Tempo de ciclo");
    await user.type(input, "30");

    const spy = spyValueWrites();
    try {
      await snapshot("sw3", { "1": true });
      await snapshot("sw3", { "2": false });
      expect(spy.writes).toEqual([]);
      expect(input.value).toBe("30");
    } finally {
      spy.restore();
    }
  });

  it("um load() identico nao recria a view nem toca no input", async () => {
    const user = userEvent.setup();
    render(DeviceDetail);
    const input = textField("Tempo de ciclo");
    await user.type(input, "30");

    const stored = hub.tuya.devices.find((d) => d.id === "sw3");
    const spy = spyValueWrites();
    try {
      await hub.load(true);
      await tick();
      expect(spy.writes).toEqual([]);
      expect(hub.tuya.devices.find((d) => d.id === "sw3")).toBe(stored);
      expect(textField("Tempo de ciclo")).toBe(input);
      expect(input.value).toBe("30");
    } finally {
      spy.restore();
    }
  });

  it("um load() que muda OUTRO device nao toca no input deste", async () => {
    const user = userEvent.setup();
    render(DeviceDetail);
    const input = textField("Tempo de ciclo");
    await user.type(input, "30");

    const stored = hub.tuya.devices.find((d) => d.id === "sw3");
    fake.api.tuyaDevices = (() =>
      Promise.resolve({
        devices: [
          tuyaDevice({ id: "sw3", name: "Interruptor", state: { ...DPS } }),
          tuyaDevice({ id: "outro", name: "Tomada", state: { "1": false } }),
        ],
        cloud: false,
        listening: [],
      })) as unknown as typeof fake.api.tuyaDevices;

    const spy = spyValueWrites();
    try {
      await hub.load(true);
      await tick();
      expect(hub.tuya.devices.find((d) => d.id === "sw3")).toBe(stored);
      expect(spy.writes).toEqual([]);
      expect(input.value).toBe("30");
    } finally {
      spy.restore();
    }
  });

  it("o campo reage quando e o proprio DP que muda", async () => {
    render(DeviceDetail);
    const input = textField("Tempo de ciclo");
    expect(input.value).toBe("");
    await snapshot("sw3", { "17": "45" });
    expect(textField("Tempo de ciclo").value).toBe("45");
  });

  it("mostra o valor real de um enum mesmo fora do range declarado", () => {
    render(DeviceDetail);
    const sel = enumSelect("Estado do relé");
    expect(sel.value).toBe("off");
    expect([...sel.options].map((o) => o.value)).toContain("off");
  });

  it("nao apaga o texto enquanto ele esta sendo digitado", async () => {
    const user = userEvent.setup();
    render(DeviceDetail);
    const input = textField("Tempo de ciclo");
    await user.click(input);
    await user.type(input, "12");
    await snapshot("sw3", DPS);
    await user.type(input, "0");
    expect(input.value).toBe("120");
  });

  it("avisa quando o aparelho ignora a escrita", async () => {
    const user = userEvent.setup();
    render(DeviceDetail);
    const sel = enumSelect("Estado do relé");
    await user.selectOptions(sel, "power_on");
    await snapshot("sw3", DPS);
    // O aparelho nunca confirma: o campo volta ao estado real e o operador e avisado.
    await new Promise((r) => setTimeout(r, 4200));
    await snapshot("sw3", DPS);
    expect(enumSelect("Estado do relé").value).toBe("off");
    expect(toast.message).toMatch(/não confirmou/);
  }, 10000);
});
