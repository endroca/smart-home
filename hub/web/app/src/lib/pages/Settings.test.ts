// Teste da seção Tuya em Ajustes: "Listar da nuvem" busca os devices do app e
// mostra os que não aparecem na varredura de LAN (fechaduras que dormem).

import { afterEach, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { setApi, type Api } from "$lib/api";
import Settings from "./Settings.svelte";

function setCloud(devices: unknown[]): void {
  setApi({ enc: (v: string) => encodeURIComponent(v), tuyaCloud: async () => ({ devices }) } as unknown as Api);
}

afterEach(() => {
  setApi(null);
});

describe("Settings — Tuya da nuvem", () => {
  it("lista os devices do app e oferece registrar", async () => {
    setCloud([
      { id: "lock1", name: "Fechadura", category: "jtmspro", has_key: true, online: true, registered: false },
      { id: "plug1", name: "Tomada", category: "cz", has_key: true, online: true, registered: true },
    ]);
    const user = userEvent.setup();
    render(Settings);

    await user.click(screen.getByRole("button", { name: "Listar da nuvem" }));

    await waitFor(() => expect(screen.getByText("Fechadura")).toBeInTheDocument());
    expect(screen.getByText("Tomada")).toBeInTheDocument();
    // A fechadura (não registrada) oferece o botão; a tomada já está registrada.
    expect(screen.getAllByRole("button", { name: "Registrar" })).toHaveLength(1);
    expect(screen.getByText("registrado")).toBeInTheDocument();
  });

  it("não oferece registrar um device sem chave local", async () => {
    setCloud([{ id: "x", name: "Sem chave", category: "cz", has_key: false, registered: false }]);
    const user = userEvent.setup();
    render(Settings);

    await user.click(screen.getByRole("button", { name: "Listar da nuvem" }));

    await waitFor(() => expect(screen.getByText("sem chave")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Registrar" })).toBeNull();
  });
});
