import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { hub } from "$lib/stores/data.svelte";
import { roomFilter } from "$lib/stores/nav.svelte";
import { kindBadge, roomChipName } from "./display";
import { sampleRooms } from "$lib/test/fixtures";

beforeEach(() => {
  hub.rooms = sampleRooms;
  roomFilter.active = "";
});
afterEach(() => {
  roomFilter.active = "";
});

describe("etiqueta de familia", () => {
  it("mostra Tuya, Midea e custom", () => {
    expect(kindBadge("tuya")).toBe("Tuya");
    expect(kindBadge("midea")).toBe("Midea");
    expect(kindBadge("fan")).toBe("custom");
  });

  it("cai no proprio kind quando desconhecido", () => {
    expect(kindBadge("thing")).toBe("thing");
  });
});

describe("chip de comodo", () => {
  it("mostra o nome do comodo na aba Todos", () => {
    expect(roomChipName("sala")).toBe("Sala");
  });

  it("nao mostra nada quando um comodo esta filtrado", () => {
    roomFilter.active = "sala";
    expect(roomChipName("sala")).toBe("");
  });

  it("nao mostra nada sem comodo", () => {
    expect(roomChipName("")).toBe("");
  });

  it("nao mostra nada para um comodo desconhecido", () => {
    expect(roomChipName("nao-existe")).toBe("");
  });
});
