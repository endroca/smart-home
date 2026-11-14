// Teste de DOM do histórico de aberturas da fechadura: recolhido por padrão,
// carrega sob demanda e lista quem/quando/como; explica quando a API não está
// liberada.

import { afterEach, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { setApi, type Api } from "$lib/api";
import { tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import { buildTuyaDevices } from "../tuya/logic";
import LockHistory from "./LockHistory.svelte";
import type { LockView } from "./logic";

function view(): LockView {
  const specs = [tuyaSpec({ dp_id: "12", code: "residual_electricity", type: "Integer", writable: false })];
  const dev = tuyaDevice({ id: "lock1", name: "Fechadura", category: "jtmspro", cloud_only: true, state: { "12": 70 } });
  return buildTuyaDevices([dev], { lock1: specs })[0] as LockView;
}

// setRecords instala um api parcial que responde o histórico pedido.
function setRecords(result: unknown): void {
  setApi({ enc: (v: string) => encodeURIComponent(v), tuyaRecords: async () => result } as unknown as Api);
}

async function open(): Promise<void> {
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: /Histórico de aberturas/ }));
}

afterEach(() => {
  setApi(null);
});

describe("LockHistory", () => {
  it("começa recolhido", () => {
    setRecords({ available: true, records: [] });
    render(LockHistory, { props: { device: view() } });
    expect(screen.queryByText("Abra o painel para carregar o histórico.")).toBeNull();
  });

  it("carrega e lista as aberturas ao abrir", async () => {
    setRecords({
      available: true,
      total: 2,
      records: [
        { at: 1794578619000, code: "unlock_fingerprint", value: "11", unlock_name: "Finger11" },
        { at: 1794575000000, code: "unlock_password", value: "2", unlock_name: "Password andrew" },
      ],
    });
    render(LockHistory, { props: { device: view() } });
    await open();

    await waitFor(() => expect(screen.getByText("Finger11")).toBeInTheDocument());
    expect(screen.getByText("Password andrew")).toBeInTheDocument();
    expect(screen.getByText(/2 aberturas nos últimos 30 dias/)).toBeInTheDocument();
  });

  it("explica quando a API de histórico não está liberada", async () => {
    setRecords({ available: false, reason: "not_subscribed", message: "uri path invalid" });
    render(LockHistory, { props: { device: view() } });
    await open();

    await waitFor(() => expect(screen.getByText(/Smart Lock/)).toBeInTheDocument());
  });

  it("mostra estado vazio quando não há aberturas", async () => {
    setRecords({ available: true, total: 0, records: [] });
    render(LockHistory, { props: { device: view() } });
    await open();

    await waitFor(() => expect(screen.getByText(/Nenhuma abertura registrada/)).toBeInTheDocument());
  });
});
