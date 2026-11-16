// Testes da logica pura da familia Tuya: sem DOM, sem api e sem Svelte.

import { describe, expect, it } from "vitest";
import { tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import {
  buildTuyaDevices,
  dpDesc,
  dpLabel,
  DP_INFO,
  isTuyaGangCode,
  isTuyaOutputCode,
  keyLabel,
  MASTER_CODES,
  specByCode,
  TELEMETRY,
  tuyaBoolSpecs,
  tuyaGangSpecs,
  tuyaHasTelemetry,
  tuyaIsMultiGang,
  tuyaOffSpecs,
  tuyaTelemetry,
  tuyaVal,
} from "./logic";

// Lista mista representativa de uma tomada: saidas, DPs de configuracao,
// telemetria e um enum.
const mixedSpecs = [
  tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
  tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean" }),
  tuyaSpec({ dp_id: "3", code: "switch_inching", type: "Boolean" }),
  tuyaSpec({ dp_id: "4", code: "child_lock", type: "Boolean" }),
  tuyaSpec({ dp_id: "5", code: "overcharge_switch", type: "Boolean" }),
  tuyaSpec({ dp_id: "6", code: "relay_status", type: "Enum" }),
  tuyaSpec({ dp_id: "18", code: "cur_power", type: "Integer" }),
  tuyaSpec({ dp_id: "19", code: "cur_voltage", type: "Integer" }),
  tuyaSpec({ dp_id: "20", code: "cur_current", type: "Integer" }),
];

const telemetrySpecs = [
  tuyaSpec({ dp_id: "18", code: "cur_power", type: "Integer" }),
  tuyaSpec({ dp_id: "19", code: "cur_voltage", type: "Integer" }),
  tuyaSpec({ dp_id: "20", code: "cur_current", type: "Integer" }),
];

describe("constantes exportadas", () => {
  it("MASTER_CODES cobre as chaves mestras conhecidas", () => {
    expect(MASTER_CODES).toEqual(["switch_1", "switch", "power", "switch_led"]);
  });

  it("TELEMETRY descreve potencia, tensao e corrente", () => {
    expect(TELEMETRY.map((t) => t.key)).toEqual(["power", "voltage", "current"]);
    expect(TELEMETRY[0].codes).toEqual(["cur_power"]);
  });

  it("DP_INFO tem rotulos curados", () => {
    expect(DP_INFO.child_lock.label).toBe("Trava infantil");
  });
});

describe("specByCode", () => {
  it("devolve a primeira spec cujo codigo casa", () => {
    expect(specByCode(mixedSpecs, ["countdown_1", "cur_voltage"])?.dp_id).toBe("19");
  });

  it("devolve null quando nada casa ou a lista e vazia", () => {
    expect(specByCode(mixedSpecs, ["nope"])).toBeNull();
    expect(specByCode(undefined, ["cur_power"])).toBeNull();
  });
});

describe("tuyaVal", () => {
  it("le o valor pelo dp_id no estado do device", () => {
    const d = tuyaDevice({ id: "d1", state: { "1": true, "18": 125 } });
    expect(tuyaVal(d, specByCode(mixedSpecs, ["cur_power"]))).toBe(125);
    expect(tuyaVal(d, null)).toBeUndefined();
  });
});

describe("tuyaBoolSpecs", () => {
  it("lista apenas as specs booleanas, na ordem original", () => {
    expect(tuyaBoolSpecs(mixedSpecs).map((s) => s.code)).toEqual([
      "switch_1",
      "switch_2",
      "switch_inching",
      "child_lock",
      "overcharge_switch",
    ]);
  });
});

describe("tuyaTelemetry", () => {
  it("pareia cada grandeza com sua spec", () => {
    const items = tuyaTelemetry(telemetrySpecs);
    expect(items.map((x) => x.t.key)).toEqual(["power", "voltage", "current"]);
    expect(items[0].spec.code).toBe("cur_power");
  });

  it("ignora grandezas sem spec", () => {
    expect(tuyaTelemetry([tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" })])).toEqual([]);
  });
});

describe("tuyaHasTelemetry", () => {
  it("roteia tomada (com telemetria) x interruptor (sem)", () => {
    expect(tuyaHasTelemetry(telemetrySpecs)).toBe(true);
    expect(tuyaHasTelemetry(mixedSpecs)).toBe(true);
    expect(tuyaHasTelemetry([tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" })])).toBe(false);
    expect(tuyaHasTelemetry(undefined)).toBe(false);
  });
});

describe("tuyaOffSpecs", () => {
  it("lista as saidas booleanas e exclui as de configuracao", () => {
    const codes = tuyaOffSpecs({ specs: mixedSpecs }).map((s) => s.code);
    expect(codes).toEqual(["switch_1", "switch_2"]);
    expect(codes).not.toContain("switch_inching");
    expect(codes).not.toContain("child_lock");
    expect(codes).not.toContain("overcharge_switch");
  });

  it("exclui chaves nao gravaveis", () => {
    const specs = [
      tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
      tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean", writable: false }),
    ];
    expect(tuyaOffSpecs({ specs }).map((s) => s.code)).toEqual(["switch_1"]);
  });

  it("aceita power e relay alem de switch*", () => {
    const specs = [
      tuyaSpec({ dp_id: "1", code: "power", type: "Boolean" }),
      tuyaSpec({ dp_id: "2", code: "relay", type: "Boolean" }),
      tuyaSpec({ dp_id: "3", code: "switch_led", type: "Boolean" }),
    ];
    expect(tuyaOffSpecs({ specs }).map((s) => s.code)).toEqual(["power", "relay", "switch_led"]);
  });

  it("nunca inclui o backlight nem DPs switch_* de configuracao", () => {
    const specs = [
      tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
      tuyaSpec({ dp_id: "16", code: "switch_backlight", type: "Boolean" }),
      tuyaSpec({ dp_id: "4", code: "switch_inching", type: "Boolean" }),
    ];
    expect(tuyaOffSpecs({ specs }).map((s) => s.code)).toEqual(["switch_1"]);
  });
});

describe("isTuyaGangCode / isTuyaOutputCode", () => {
  it("reconhece apenas teclas numeradas", () => {
    expect(isTuyaGangCode("switch_1")).toBe(true);
    expect(isTuyaGangCode("switch_12")).toBe(true);
    expect(isTuyaGangCode("switch_led")).toBe(false);
    expect(isTuyaGangCode("switch_backlight")).toBe(false);
    expect(isTuyaGangCode("switch_usb")).toBe(false);
    expect(isTuyaGangCode("switch")).toBe(false);
    expect(isTuyaGangCode(undefined)).toBe(false);
  });

  it("distingue saida de DP de configuracao", () => {
    expect(isTuyaOutputCode("power")).toBe(true);
    expect(isTuyaOutputCode("switch_usb")).toBe(true);
    expect(isTuyaOutputCode("switch_3")).toBe(true);
    expect(isTuyaOutputCode("switch_backlight")).toBe(false);
    expect(isTuyaOutputCode("countdown_2")).toBe(false);
  });
});

describe("tuyaGangSpecs", () => {
  const gangSpecs = [
    tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean" }),
    tuyaSpec({ dp_id: "10", code: "switch_10", type: "Boolean" }),
    tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
    tuyaSpec({ dp_id: "16", code: "switch_backlight", type: "Boolean" }),
    tuyaSpec({ dp_id: "4", code: "switch_inching", type: "Boolean" }),
    tuyaSpec({ dp_id: "5", code: "switch_3", type: "Boolean", writable: false }),
  ];

  it("lista so as teclas gravaveis, em ordem numerica", () => {
    expect(tuyaGangSpecs(gangSpecs).map((s) => s.code)).toEqual(["switch_1", "switch_2", "switch_10"]);
  });

  it("devolve lista vazia quando nao ha tecla numerada", () => {
    expect(tuyaGangSpecs(undefined)).toEqual([]);
    expect(
      tuyaGangSpecs([
        tuyaSpec({ dp_id: "16", code: "switch_backlight", type: "Boolean" }),
        tuyaSpec({ dp_id: "4", code: "switch_inching", type: "Boolean" }),
      ]),
    ).toEqual([]);
  });
});

describe("tuyaIsMultiGang", () => {
  it("exige duas ou mais teclas", () => {
    const one = [tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" })];
    const two = [
      tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
      tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean" }),
    ];
    expect(tuyaIsMultiGang(one)).toBe(false);
    expect(tuyaIsMultiGang(two)).toBe(true);
    expect(tuyaIsMultiGang(undefined)).toBe(false);
  });
});

describe("buildTuyaDevices", () => {
  it("junta specs, estado, online, listening e o registro do banco", () => {
    const dev = tuyaDevice({
      id: "bf1",
      name: "Tomada",
      room_id: "sala",
      template: "inferido",
      online: true,
      has_key: true,
      state: { "1": true, cur_power: 125 },
    });
    const specs = { bf1: telemetrySpecs };
    const rows = [{ id: "bf1", kind: "tuya", room_id: "quarto", template: "tuya.socket" }];
    const [view] = buildTuyaDevices([dev], specs, ["bf1"], rows);

    expect(view.kind).toBe("tuya");
    expect(view.name).toBe("Tomada");
    expect(view.specs).toBe(telemetrySpecs);
    expect(view.state).toEqual({ "1": true, cur_power: 125 });
    expect(view.online).toBe(true);
    expect(view.listening).toBe(true);
    // o registro do banco vence o que veio no proprio device
    expect(view.room).toBe("quarto");
    expect(view.template).toBe("tuya.socket");
  });

  it("sem listener nem banco, usa o device e marca offline", () => {
    const dev = tuyaDevice({ id: "bf2", name: "Plug", room_id: "sala", online: false });
    const [view] = buildTuyaDevices([dev], {});
    expect(view.listening).toBe(false);
    expect(view.online).toBe(false);
    expect(view.room).toBe("sala");
    expect(view.template).toBe("");
    expect(view.specs).toEqual([]);
    expect(view.state).toEqual({});
  });

  it("respeita um listening explicito no proprio device", () => {
    const dev = tuyaDevice({ id: "bf3" }) as ReturnType<typeof tuyaDevice> & { listening?: boolean };
    dev.listening = true;
    const [view] = buildTuyaDevices([dev], {});
    expect(view.listening).toBe(true);
  });

  it("usa o id como nome quando o device nao tem nome", () => {
    const [view] = buildTuyaDevices([tuyaDevice({ id: "sem-nome" })], {});
    expect(view.name).toBe("sem-nome");
  });
});

describe("keyLabel", () => {
  it("usa o rotulo cadastrado (Funcoes e voz) quando existe", () => {
    const spec = tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" });
    expect(keyLabel(spec, { "1": "Luz Principal" })).toBe("Luz Principal");
  });

  it("cai no dicionario/spec quando nao ha rotulo cadastrado", () => {
    const spec = tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" });
    expect(keyLabel(spec)).toBe("Chave 1");
    expect(keyLabel(spec, {})).toBe("Chave 1");
    expect(keyLabel(spec, { "2": "Outra" })).toBe("Chave 1");
  });

  it("o nome da spec vence o dicionario quando o rotulo nao foi cadastrado", () => {
    const spec = tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Abajur" });
    expect(keyLabel(spec)).toBe("Abajur");
    expect(keyLabel(spec, { "1": "Luz Principal" })).toBe("Luz Principal");
  });
});

describe("dpLabel / dpDesc", () => {
  it("o nome da spec vence o dicionario", () => {
    expect(dpLabel(tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Lâmpada" }))).toBe("Lâmpada");
  });

  it("cai no dicionario quando nao ha nome", () => {
    expect(dpLabel(tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }))).toBe("Chave 1");
  });

  it("cai no codigo quando nada mais existe", () => {
    expect(dpLabel(tuyaSpec({ dp_id: "1", code: "dp_x", type: "Boolean" }))).toBe("dp_x");
    expect(dpLabel(null)).toBe("");
  });

  it("descricao da spec vence, mas ignora placeholders vazios", () => {
    expect(dpDesc(tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", desc: "minha desc" }))).toBe("minha desc");
    expect(dpDesc(tuyaSpec({ dp_id: "1", code: "child_lock", type: "Boolean", desc: "{}" }))).toContain("Bloqueia");
    expect(dpDesc(tuyaSpec({ dp_id: "1", code: "child_lock", type: "Boolean" }))).toContain("Bloqueia");
    expect(dpDesc(tuyaSpec({ dp_id: "1", code: "dp_x", type: "Boolean" }))).toBe("");
  });
});
