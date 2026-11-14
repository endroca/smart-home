// Teste do controle "Ler pela nuvem" na config compartilhada da familia Tuya:
// o modo cloud vale para qualquer device Tuya e, ligado, esconde o "Ouvir".

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { setApi } from "$lib/api";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import { tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import { buildTuyaDevices, type TuyaView } from "./logic";
import Config from "./Config.svelte";

let fake: FakeApi;

beforeEach(() => {
  fake = makeFakeApi();
  setApi(fake.api);
});

afterEach(() => {
  setApi(null);
});

function view(cloudOnly: boolean): TuyaView {
  const specs = [tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Chave" })];
  const dev = tuyaDevice({ id: "bf1", name: "Tomada", has_key: true, cloud_only: cloudOnly, state: { "1": false } });
  return buildTuyaDevices([dev], { bf1: specs })[0];
}

describe("tuya Config (Ler pela nuvem)", () => {
  it("mostra o controle", () => {
    render(Config, { props: { device: view(false) } });
    expect(screen.getByRole("switch", { name: "Ler pela nuvem" })).toBeInTheDocument();
  });

  it("mostra Ouvir no modo LAN", () => {
    render(Config, { props: { device: view(false) } });
    expect(screen.getByRole("button", { name: "Ouvir" })).toBeInTheDocument();
  });

  it("esconde Ouvir quando o device e lido pela nuvem", () => {
    render(Config, { props: { device: view(true) } });
    expect(screen.queryByRole("button", { name: "Ouvir" })).toBeNull();
  });

  it("desliga o modo nuvem ao tocar o controle", async () => {
    const user = userEvent.setup();
    render(Config, { props: { device: view(true) } });

    await user.click(screen.getByRole("switch", { name: "Ler pela nuvem" }));

    await waitFor(() => {
      expect(fake.calls).toContainEqual({ method: "post", args: ["/api/devices/bf1", { cloud_only: false }] });
    });
  });
});
