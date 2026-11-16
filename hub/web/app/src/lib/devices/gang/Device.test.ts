// Teste de DOM do card do interruptor multi-tecla: as teclas da placa, os DPs
// de configuracao fora dela, os botoes em lote e a inferencia do layout.

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { setApi } from "$lib/api";
import { hub } from "$lib/stores/data.svelte";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import { tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import { buildTuyaDevices, type TuyaView } from "../tuya/logic";
import { resolveTemplate } from "../registry";
import "../index";
import Device from "./Device.svelte";

let fake: FakeApi;

beforeEach(() => {
  fake = makeFakeApi();
  setApi(fake.api);
  hub.functionLabels = {};
});

afterEach(() => {
  setApi(null);
});

// Um interruptor de 3 teclas como o "Interruptor-Inteligente-lite-3-bot-es":
// tres saidas switch_N mais os DPs que configuram o aparelho.
const gangSpecs = [
  tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
  tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean" }),
  tuyaSpec({ dp_id: "3", code: "switch_3", type: "Boolean" }),
  tuyaSpec({ dp_id: "16", code: "switch_backlight", type: "Boolean" }),
  tuyaSpec({ dp_id: "14", code: "relay_status", type: "Enum" }),
  tuyaSpec({ dp_id: "7", code: "countdown_1", type: "Integer" }),
];

function gangView(state: Record<string, unknown> = {}): TuyaView {
  const dev = tuyaDevice({ id: "sw3", name: "Interruptor", category: "kg", state });
  return buildTuyaDevices([dev], { sw3: gangSpecs })[0];
}

describe("gang Device", () => {
  it("a inferencia escolhe o layout multi-tecla", () => {
    expect(resolveTemplate(gangView()).id).toBe("tuya.gang");
  });

  it("mostra uma tecla por saida, com o estado de cada uma", () => {
    render(Device, { props: { device: gangView({ "1": false, "2": true, "3": false }) } });

    expect(screen.getByRole("heading", { name: "Interruptor" })).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Chave 1" })).toHaveAttribute("aria-checked", "false");
    expect(screen.getByRole("switch", { name: "Chave 2" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("switch", { name: "Chave 3" })).toHaveAttribute("aria-checked", "false");
    expect(screen.getAllByText("Ligada")).toHaveLength(1);
    expect(screen.getAllByText("Desligada")).toHaveLength(2);
  });

  it("usa o rotulo cadastrado (Funcoes e voz) no lugar do nome padrao", () => {
    hub.functionLabels = { sw3: { "1": "Luz Principal", "2": "Luz Suave", "3": "Spots" } };
    render(Device, { props: { device: gangView({ "1": true, "2": false, "3": false }) } });

    expect(screen.getByText("Luz Principal")).toBeInTheDocument();
    expect(screen.getByText("Luz Suave")).toBeInTheDocument();
    expect(screen.getByText("Spots")).toBeInTheDocument();
    expect(screen.queryByText("Chave 1")).toBeNull();
    // O comando continua saindo pelo DP certo, com o rotulo cadastrado.
    expect(screen.getByRole("switch", { name: "Luz Principal" })).toHaveAttribute("aria-checked", "true");
  });

  it("nao expoe o backlight nem os countdowns como teclas", () => {
    render(Device, { props: { device: gangView() } });
    expect(screen.queryByRole("switch", { name: "Luz de fundo" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Timer" })).toBeNull();
  });

  it("tocar numa tecla manda o DP certo", async () => {
    const user = userEvent.setup();
    render(Device, { props: { device: gangView({ "1": false, "2": false, "3": false }) } });

    await user.click(screen.getByRole("switch", { name: "Chave 3" }));

    await waitFor(() => {
      expect(fake.calls).toContainEqual({ method: "tuyaControl", args: ["sw3", "3", true] });
    });
  });

  it("Ligar todas manda so as teclas que estao desligadas", async () => {
    const user = userEvent.setup();
    render(Device, { props: { device: gangView({ "1": false, "2": true, "3": false }) } });

    await user.click(screen.getByRole("button", { name: "Ligar todas" }));

    await waitFor(() => {
      expect(fake.calls).toContainEqual({ method: "tuyaControl", args: ["sw3", "1", true] });
      expect(fake.calls).toContainEqual({ method: "tuyaControl", args: ["sw3", "3", true] });
    });
    expect(fake.calls.filter((c) => c.method === "tuyaControl")).toHaveLength(2);
  });

  it("Desligar todas desliga as tres teclas", async () => {
    const user = userEvent.setup();
    render(Device, { props: { device: gangView({ "1": true, "2": true, "3": true }) } });

    await user.click(screen.getByRole("button", { name: "Desligar todas" }));

    await waitFor(() => {
      expect(fake.calls.filter((c) => c.method === "tuyaControl")).toHaveLength(3);
    });
  });

  it("sem teclas: orienta a usar a engrenagem", () => {
    const dev = tuyaDevice({ id: "sw3", name: "Interruptor" });
    const view = buildTuyaDevices([dev], { sw3: [] })[0];
    render(Device, { props: { device: view } });
    expect(screen.getByText("Sem teclas carregadas. Use a engrenagem para carregar os data points.")).toBeInTheDocument();
  });
});
