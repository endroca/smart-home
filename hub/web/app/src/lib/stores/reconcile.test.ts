// Testes da reconciliacao: preservar identidade e gravar so o que mudou e o que
// impede um snapshot identico de invalidar a UI inteira.

import { describe, expect, it } from "vitest";
import { deepEqual, reconcileField, reconcileList, reconcileObject } from "./reconcile";

describe("deepEqual", () => {
  it("compara escalares, arrays e objetos aninhados", () => {
    expect(deepEqual(1, 1)).toBe(true);
    expect(deepEqual("a", "b")).toBe(false);
    expect(deepEqual([1, 2], [1, 2])).toBe(true);
    expect(deepEqual([1, 2], [2, 1])).toBe(false);
    expect(deepEqual({ a: { b: [1] } }, { a: { b: [1] } })).toBe(true);
    expect(deepEqual({ a: 1 }, { a: 1, b: 2 })).toBe(false);
    expect(deepEqual(null, undefined)).toBe(false);
  });
});

describe("reconcileObject", () => {
  it("nao grava nada quando o conteudo e igual", () => {
    const target = { a: 1, nested: { x: 1, y: 2 } };
    const changed = reconcileObject(target, { a: 1, nested: { x: 1, y: 2 } });
    expect(changed).toBe(false);
  });

  it("preserva a identidade dos objetos aninhados", () => {
    const target: Record<string, unknown> = { state: { "1": false, "2": true } };
    const state = target.state;
    reconcileObject(target, { state: { "1": false, "2": true } });
    expect(target.state).toBe(state);
  });

  it("grava apenas a chave que mudou (o resto mantem identidade)", () => {
    const target: Record<string, unknown> = { state: { "1": false, "2": true }, name: "sala" };
    const state = target.state;
    const changed = reconcileObject(target, { state: { "1": true, "2": true }, name: "sala" });
    expect(changed).toBe(true);
    expect(target.state).toBe(state);
    expect((target.state as Record<string, unknown>)["1"]).toBe(true);
    expect(target.name).toBe("sala");
  });

  it("remove chaves que sumiram, mas so com payload completo", () => {
    const target: Record<string, unknown> = { a: 1, b: 2 };
    // Evento incremental: nao pode apagar os outros campos.
    expect(reconcileObject(target, { a: 1 })).toBe(false);
    expect(target.b).toBe(2);
    // Snapshot completo: poda o que sumiu.
    expect(reconcileObject(target, { a: 1 }, { prune: true })).toBe(true);
    expect("b" in target).toBe(false);
  });
});

describe("reconcileList", () => {
  it("nao mexe no array quando nada mudou", () => {
    const a = { id: "1", name: "A", state: { x: 1 } };
    const target = [a];
    const changed = reconcileList(target, [{ id: "1", name: "A", state: { x: 1 } }]);
    expect(changed).toBe(false);
    expect(target[0]).toBe(a);
  });

  it("reaproveita o objeto existente quando algo muda", () => {
    const a = { id: "1", name: "A", state: { x: 1 } };
    const target = [a];
    const changed = reconcileList(target, [{ id: "1", name: "A", state: { x: 2 } }]);
    expect(changed).toBe(true);
    expect(target[0]).toBe(a);
    expect((target[0].state as Record<string, unknown>).x).toBe(2);
  });

  it("adiciona e remove pela chave id", () => {
    const a = { id: "1", name: "A" };
    const target = [a];
    expect(reconcileList(target, [{ id: "1", name: "A" }, { id: "2", name: "B" }])).toBe(true);
    expect(target.map((t) => t.id)).toEqual(["1", "2"]);
    expect(target[0]).toBe(a);
    expect(reconcileList(target, [{ id: "2", name: "B" }])).toBe(true);
    expect(target.map((t) => t.id)).toEqual(["2"]);
  });
});

describe("reconcileField", () => {
  it("so grava quando o valor muda", () => {
    const target: Record<string, unknown> = { cloud: false, listening: ["a"] };
    const listening = target.listening;
    expect(reconcileField(target, "cloud", false)).toBe(false);
    expect(reconcileField(target, "listening", ["a"])).toBe(false);
    expect(target.listening).toBe(listening);
    expect(reconcileField(target, "cloud", true)).toBe(true);
    expect(target.cloud).toBe(true);
  });
});
