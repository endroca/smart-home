// Teste de DOM do card da tomada: monta um TuyaView de fixture e confirma que
// tocar o dial manda o comando certo pela API (fake).

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

function socketView(): TuyaView {
  const specs = [
    tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Chave" }),
    tuyaSpec({ dp_id: "18", code: "cur_power", type: "Integer" }),
    tuyaSpec({ dp_id: "19", code: "cur_voltage", type: "Integer" }),
  ];
  const dev = tuyaDevice({
    id: "bf1",
    name: "Tomada da sala",
    has_key: true,
    state: { "1": false, "18": 125, "19": 2200 },
  });
  return buildTuyaDevices([dev], { bf1: specs })[0];
}

describe("socket Device", () => {
  it("mostra o titulo e os controles principais", () => {
    render(Device, { props: { device: socketView() } });
    expect(screen.getByRole("heading", { name: "Tomada da sala" })).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Ligar ou desligar tomada" })).toBeInTheDocument();
    expect(screen.getByText("Potência")).toBeInTheDocument();
    expect(screen.getByText("Tensão")).toBeInTheDocument();
  });

  it("liga a tomada ao tocar o dial (chama api.tuyaControl)", async () => {
    const user = userEvent.setup();
    render(Device, { props: { device: socketView() } });

    await user.click(screen.getByRole("switch", { name: "Ligar ou desligar tomada" }));

    await waitFor(() => {
      expect(fake.calls).toContainEqual({ method: "tuyaControl", args: ["bf1", "1", true] });
    });
  });

  it("não desenha a engrenagem sem onOpenConfig", () => {
    render(Device, { props: { device: socketView() } });
    expect(screen.queryByRole("button", { name: "Configurar dispositivo" })).toBeNull();
  });

  it("abre a config ao tocar a engrenagem quando onOpenConfig é passada", async () => {
    const user = userEvent.setup();
    const opened: string[] = [];
    render(Device, { props: { device: socketView(), onOpenConfig: (id: string) => opened.push(id) } });

    await user.click(screen.getByRole("button", { name: "Configurar dispositivo" }));

    expect(opened).toEqual(["bf1"]);
  });
});
