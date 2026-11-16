// Teste da configuracao de funcoes: carrega o catalogo derivado e salva os
// rotulos/flags editados.

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { setApi } from "$lib/api";
import { hub } from "$lib/stores/data.svelte";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import type { FunctionCatalog } from "$lib/types";
import FunctionsConfig from "./FunctionsConfig.svelte";

const catalog: FunctionCatalog = {
  device_id: "plug",
  driver: "tuya",
  configured: false,
  functions: [
    {
      key: "1",
      label: "Chave 1",
      voice: true,
      off: true,
      off_value: false,
      driver: "tuya",
      kind: "bool",
    },
    {
      key: "2",
      label: "Modo",
      voice: true,
      off: false,
      driver: "tuya",
      kind: "enum",
      values: ["auto", "cool"],
    },
  ],
};

let fake: FakeApi;
beforeEach(() => {
  fake = makeFakeApi({
    deviceFunctions: { catalog },
    saveDeviceFunctions: { catalog: { ...catalog, configured: true } },
  });
  setApi(fake.api);
});
afterEach(async () => {
  await new Promise((r) => setTimeout(r, 0));
  setApi(null);
});

describe("FunctionsConfig", () => {
  it("lista as funcoes do catalogo derivado", async () => {
    render(FunctionsConfig, { props: { deviceId: "plug" } });
    await waitFor(() => expect(screen.getByDisplayValue("Chave 1")).toBeInTheDocument());
    expect(screen.getByDisplayValue("Modo")).toBeInTheDocument();
    expect(fake.calls).toContainEqual({ method: "deviceFunctions", args: ["plug"] });
  });

  it("envia os rotulos editados ao salvar", async () => {
    const user = userEvent.setup();
    render(FunctionsConfig, { props: { deviceId: "plug" } });
    const label = await screen.findByDisplayValue("Chave 1");
    await user.clear(label);
    await user.type(label, "Tomada do PC");
    await user.click(screen.getByRole("button", { name: /salvar fun/i }));

    await waitFor(() => {
      const call = fake.calls.find((c) => c.method === "saveDeviceFunctions");
      expect(call).toBeTruthy();
      expect(call?.args[0]).toBe("plug");
      const sent = call?.args[1] as { key: string; label: string }[];
      expect(sent[0].key).toBe("1");
      expect(sent[0].label).toBe("Tomada do PC");
    });
  });

  it("nao renderiza nada quando o device nao tem funcoes", async () => {
    fake = makeFakeApi({ deviceFunctions: { catalog: { ...catalog, functions: [] } } });
    setApi(fake.api);
    render(FunctionsConfig, { props: { deviceId: "plug" } });
    await waitFor(() => expect(fake.calls).toContainEqual({ method: "deviceFunctions", args: ["plug"] }));
    expect(screen.queryByText(/Funções e voz/)).not.toBeInTheDocument();
  });

  it("publica os rotulos salvos no store, para o card mostrar o nome cadastrado", async () => {
    const user = userEvent.setup();
    const saved: FunctionCatalog = {
      ...catalog,
      configured: true,
      functions: [{ ...catalog.functions[0], label: "Luz Principal" }, catalog.functions[1]],
    };
    fake = makeFakeApi({ deviceFunctions: { catalog }, saveDeviceFunctions: { catalog: saved } });
    setApi(fake.api);
    hub.functionLabels = {};

    render(FunctionsConfig, { props: { deviceId: "plug" } });
    // O carregamento do catalogo ja deixa os rotulos derivados disponiveis.
    await waitFor(() => expect(hub.functionLabels["plug"]?.["1"]).toBe("Chave 1"));

    const label = await screen.findByDisplayValue("Chave 1");
    await user.clear(label);
    await user.type(label, "Luz Principal");
    await user.click(screen.getByRole("button", { name: /salvar fun/i }));

    await waitFor(() => expect(hub.functionLabels["plug"]?.["1"]).toBe("Luz Principal"));
  });
});
