// Sucessor do antigo smoke.mjs: monta cada tela com dados de exemplo e falha
// se algo lancar (campo renomeado no backend so aparece quebrando a tela).
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/svelte";
import { setApi } from "$lib/api";
import { hub } from "$lib/stores/data.svelte";
import { nav, roomFilter } from "$lib/stores/nav.svelte";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import { cap, deviceRow, manifestDevice, mideaDevice, sampleRooms, thing, tuyaDevice } from "$lib/test/fixtures";
import Home from "./Home.svelte";
import Rooms from "./Rooms.svelte";
import Automations from "./Automations.svelte";
import Settings from "./Settings.svelte";
import DeviceDetail from "./DeviceDetail.svelte";

let fake: FakeApi;

function seed() {
  hub.things = [
    thing("rf01", {
      actions: ["fan.add"],
      devices: [manifestDevice("fan0", "Ventilador", { power: cap("boolean"), speed: cap("enum") }, ["fan.rename"])],
      reported: { fan0: { power: true, speed: 2 } },
    }),
  ];
  hub.devices = [
    deviceRow({ id: "rf01/fan0", kind: "fan", owner: "rf01", device_id: "fan0", name: "Ventilador", room_id: "sala" }),
    deviceRow({ id: "rf01", kind: "thing", name: "Gateway da sala" }),
  ];
  hub.rooms = sampleRooms;
  hub.automations = [
    { id: "a1", name: "Cena noite", enabled: true, source: { type: "manual" }, actions: [] },
    { id: "a2", name: "Botão → luz", enabled: false, source: { type: "tuya_dp", device: "bf123", dp: "1" }, actions: [] },
    {
      id: "a3",
      name: "Desliga em 10 min",
      enabled: true,
      once: true,
      source: { type: "time", in_seconds: 600 },
      actions: [{ kind: "tuya", device: "bf123", dp: "1", value: false }],
    },
  ];
  hub.tuya.devices = [tuyaDevice({ id: "bf123", name: "Tomada", room_id: "sala" })];
  hub.tuya.available = true;
  hub.midea.devices = [mideaDevice({ id: "ac1", name: "Ar da sala", room_id: "sala" })];
  hub.midea.available = true;
  hub.firmware = { files: [{ name: "smart_control-1.0.0.bin", version: "1.0.0", size: 100 }], pending: {} };
  hub.loaded = true;
}

beforeEach(() => {
  fake = makeFakeApi();
  setApi(fake.api);
  nav.page = "home";
  nav.detail = null;
  seed();
});
afterEach(() => {
  setApi(null);
  nav.detail = null;
  roomFilter.active = "";
});

describe("páginas renderizam com dados de exemplo", () => {
  it("Início mostra o card do ventilador e as abas de cômodo", () => {
    render(Home);
    expect(screen.getByText("Ventilador")).toBeInTheDocument();
    // "Sala" aparece na aba de comodo e no chip do card.
    expect(screen.getAllByText("Sala").length).toBeGreaterThan(0);
    expect(screen.getByLabelText("Desligar tudo")).toBeInTheDocument();
  });

  it("Início rola a barra de cômodos até o cômodo ativo ao abrir", () => {
    roomFilter.active = "quarto";
    const spy = vi.spyOn(Element.prototype, "scrollIntoView");
    try {
      render(Home);
      const chip = document.querySelector('[data-room-id="quarto"]');
      expect(chip).not.toBeNull();
      // O chip ativo e trazido para a area visivel da barra.
      expect(spy).toHaveBeenCalled();
      expect(spy.mock.instances[0]).toBe(chip);
    } finally {
      spy.mockRestore();
    }
  });

  it("Cômodos lista os grupos", async () => {
    render(Rooms);
    expect(screen.getAllByText("Sem cômodo").length).toBeGreaterThan(0);
    // O nome do cômodo e preenchido por um $effect no mount.
    expect((await screen.findAllByDisplayValue("Sala")).length).toBeGreaterThan(0);
  });

  it("Automações lista as automações", () => {
    render(Automations);
    expect(screen.getByText("Cena noite")).toBeInTheDocument();
    expect(screen.getByText("Botão → luz")).toBeInTheDocument();
    // Uma volátil de horário ganha o rótulo temporário.
    expect(screen.getByText("Desliga em 10 min")).toBeInTheDocument();
    expect(screen.getByText(/Volátil/)).toBeInTheDocument();
  });

  it("Ajustes mostra o inventário unificado", () => {
    render(Settings);
    expect(screen.getByText("Gateways RF")).toBeInTheDocument();
    expect(screen.getByText("Tomada")).toBeInTheDocument();
    expect(screen.getByText("Ar da sala")).toBeInTheDocument();
  });

  it("Detalhe do dispositivo monta a config do layout", () => {
    nav.detail = { type: "device", id: "rf01/fan0" };
    render(DeviceDetail);
    expect(screen.getByRole("heading", { level: 2 }).textContent).toBe("Ventilador");
    expect(screen.getByText("Toque = envia RF • Segure = corrige o estado sem RF")).toBeInTheDocument();
  });

  it("Detalhe do gateway mostra firmware e ações", () => {
    nav.detail = { type: "gateway", id: "rf01" };
    render(DeviceDetail);
    expect(screen.getByText("Firmware")).toBeInTheDocument();
    expect(screen.getByText("Diagnóstico")).toBeInTheDocument();
    expect(screen.getByText("smart_control-1.0.0.bin")).toBeInTheDocument();
  });
});
