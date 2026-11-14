import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/svelte";
import { setApi } from "$lib/api";
import { hub } from "$lib/stores/data.svelte";
import { makeFakeApi, type RecordedCall } from "$lib/test/fakeApi";
import { mideaDevice } from "$lib/test/fixtures";
import Device from "./Device.svelte";
import EnergyPanel from "./EnergyPanel.svelte";
import Config from "./config/Config.svelte";
import { buildMideaDevices, type MideaView } from "./logic";
import "./index";
import { deviceById } from "$lib/devices/registry";

let calls: RecordedCall[];

beforeEach(() => {
  const fake = makeFakeApi();
  calls = fake.calls;
  setApi(fake.api);
  hub.midea.devices = [];
  hub.midea.scan = [];
  hub.rooms = [];
});

afterEach(() => {
  setApi(null);
});

function view(state: Record<string, unknown> = { power: false, mode: "auto", fan_speed: "auto", target_temp: 24 }): MideaView {
  const [v] = buildMideaDevices([
    mideaDevice({
      id: "ac1",
      name: "Sala",
      ip: "10.0.0.9",
      model: "V3",
      room_id: "sala",
      online: true,
      state,
      actions: [
        { key: "power" },
        { key: "target_temp", options: { min: 16, max: 30 } },
        { key: "mode" },
        { key: "fan_speed" },
        { key: "swing_v" },
      ],
    }),
  ]);
  return v;
}

describe("Device (Midea)", () => {
  it("mostra titulo e controles principais", () => {
    render(Device, { props: { device: view() } });

    expect(screen.getByRole("heading", { name: "Sala" })).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Ligar ou desligar" })).toBeInTheDocument();
    expect(screen.getByText("Temperatura alvo")).toBeInTheDocument();
    expect(screen.getByText("24")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Frio" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Fluxo vert." })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Configurar dispositivo" })).toBeNull();
  });

  it("liga o aparelho e chama api.mideaControl", async () => {
    render(Device, { props: { device: view({ power: false, target_temp: 24 }) } });

    await fireEvent.click(screen.getByRole("switch", { name: "Ligar ou desligar" }));

    await waitFor(() => {
      expect(calls).toContainEqual({ method: "mideaControl", args: ["ac1", { power: true }] });
    });
  });

  it("troca o modo de operacao", async () => {
    render(Device, { props: { device: view({ power: true, mode: "auto", target_temp: 24 }) } });

    await fireEvent.click(screen.getByRole("button", { name: "Frio" }));

    await waitFor(() => {
      expect(calls).toContainEqual({ method: "mideaControl", args: ["ac1", { mode: "cool" }] });
    });
  });

  it("ajusta a temperatura alvo com o stepper", async () => {
    render(Device, { props: { device: view({ power: true, target_temp: 24 }) } });

    await fireEvent.click(screen.getByRole("button", { name: "Aumentar 1 grau" }));

    await waitFor(() => {
      expect(calls).toContainEqual({ method: "mideaControl", args: ["ac1", { target_temp: 25 }] });
    });
  });

  it("abre a configuracao pela engrenagem quando o callback existe", async () => {
    const onOpenConfig = vi.fn();
    render(Device, { props: { device: view(), onOpenConfig } });

    await fireEvent.click(screen.getByRole("button", { name: "Configurar dispositivo" }));

    expect(onOpenConfig).toHaveBeenCalledWith("ac1");
  });

  it("monta o detalhe (config) e o painel de energia", () => {
    const device = view();
    const { unmount } = render(Config, { props: { device } });
    expect(screen.getByText("Consumo (experimental)")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Renomear" })).toBeInTheDocument();
    unmount();

    render(EnergyPanel, { props: { device } });
    expect(screen.getByText("Consumo (experimental)")).toBeInTheDocument();
  });

  it("registra o layout no registry de devices", () => {
    const mod = deviceById("midea.ac");
    expect(mod).toBeDefined();
    expect(mod?.kinds).toContain("midea");
    expect(mod?.match({ id: "x", kind: "midea", name: "", room: "", online: true, template: "" })).toBe(true);
  });
});
