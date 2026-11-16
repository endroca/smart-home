// Testes da ordenacao dos devices dentro de um comodo.

import { describe, expect, it } from "vitest";
import type { HubDevice } from "$lib/types";
import { sortDevices } from "./lists";

function dev(id: string, kind: string, sort: number, name = id): HubDevice {
  return { id, kind, name, room: "sala", online: true, template: "", sort };
}

describe("sortDevices", () => {
  it("respeita a posicao configurada", () => {
    const list = [dev("a", "tuya", 2), dev("b", "fan", 0), dev("c", "tuya", 1)];
    expect(sortDevices(list).map((d) => d.id)).toEqual(["b", "c", "a"]);
  });

  it("desempata por tipo e nome quando ninguem foi ordenado", () => {
    const list = [
      dev("m1", "midea", 0, "Ar"),
      dev("t2", "tuya", 0, "Tomada"),
      dev("t1", "tuya", 0, "Luz"),
      dev("f1", "fan", 0, "Ventilador"),
    ];
    expect(sortDevices(list).map((d) => d.id)).toEqual(["f1", "t1", "t2", "m1"]);
  });

  it("nao muda a lista original", () => {
    const list = [dev("a", "tuya", 1), dev("b", "tuya", 0)];
    sortDevices(list);
    expect(list.map((d) => d.id)).toEqual(["a", "b"]);
  });
});
