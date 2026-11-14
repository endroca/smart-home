// Teste de DOM do card do interruptor: cobre o hero (uma chave) e o caso de
// varias chaves, confirmando que tocar numa chave manda o DP certo pela API.

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { setApi } from "$lib/api";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import { tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import { buildTuyaDevices, type TuyaView } from "../tuya/logic";
import Device from "./Device.svelte";

let fake: FakeApi;

beforeEach(() => {
  fake = makeFakeApi();
  setApi(fake.api);
});

afterEach(() => {
  setApi(null);
});

function viewWith(specs: ReturnType<typeof tuyaSpec>[], state: Record<string, unknown>): TuyaView {
  const dev = tuyaDevice({ id: "sw1", name: "Interruptor", state });
  return buildTuyaDevices([dev], { sw1: specs })[0];
}

describe("switch Device", () => {
  it("hero: mostra o titulo e a lampada da chave", () => {
    const view = viewWith([tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Lâmpada" })], {
      "1": false,
    });
    render(Device, { props: { device: view } });

    expect(screen.getByRole("heading", { name: "Interruptor" })).toBeInTheDocument();
    expect(screen.getByText("Desligado", { exact: false })).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Alternar" })).toBeInTheDocument();
  });

  it("hero: liga a chave pelo toggle", async () => {
    const user = userEvent.setup();
    const view = viewWith([tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Lâmpada" })], {
      "1": false,
    });
    render(Device, { props: { device: view } });

    await user.click(screen.getByRole("switch", { name: "Alternar" }));

    await waitFor(() => {
      expect(fake.calls).toContainEqual({ method: "tuyaControl", args: ["sw1", "1", true] });
    });
  });

  it("multiplas chaves: cada linha alterna o seu DP", async () => {
    const user = userEvent.setup();
    const view = viewWith(
      [
        tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Lâmpada" }),
        tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean", name: "Ventilador" }),
      ],
      { "1": false, "2": true },
    );
    render(Device, { props: { device: view } });

    await user.click(screen.getByRole("switch", { name: "Lâmpada" }));

    await waitFor(() => {
      expect(fake.calls).toContainEqual({ method: "tuyaControl", args: ["sw1", "1", true] });
    });
  });

  it("sem specs: orienta a usar a engrenagem", () => {
    const view = viewWith([], {});
    render(Device, { props: { device: view } });
    expect(screen.getByText("Sem specs carregadas. Use a engrenagem para carregar os data points.")).toBeInTheDocument();
  });
});
