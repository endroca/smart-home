// Teste de DOM do card da fechadura: monta um LockView de fixture e confirma
// que bateria, alarme e metodos aparecem.

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import { buildTuyaDevices } from "../tuya/logic";
import Device from "./Device.svelte";
import type { LockView } from "./logic";

function lockView(): LockView {
  const specs = [
    tuyaSpec({ dp_id: "12", code: "residual_electricity", type: "Integer", writable: false }),
    tuyaSpec({ dp_id: "1", code: "unlock_fingerprint", type: "Integer", writable: false }),
    tuyaSpec({ dp_id: "8", code: "alarm_lock", type: "Enum", writable: false }),
  ];
  const dev = tuyaDevice({
    id: "lock1",
    name: "Fechadura porta",
    category: "jtmspro",
    cloud_only: true,
    state: { "12": 70, "1": 11, "8": "pry" },
  });
  return buildTuyaDevices([dev], { lock1: specs })[0] as LockView;
}

describe("lock Device", () => {
  it("mostra titulo, bateria, alarme e metodos", () => {
    render(Device, { props: { device: lockView() } });
    expect(screen.getByRole("heading", { name: "Fechadura porta" })).toBeInTheDocument();
    expect(screen.getByText("Bateria")).toBeInTheDocument();
    expect(screen.getByText("70")).toBeInTheDocument();
    expect(screen.getByText("Arrombamento")).toBeInTheDocument();
    expect(screen.getByText("Digital")).toBeInTheDocument();
  });

  it("marca o device como lido pela nuvem", () => {
    render(Device, { props: { device: lockView() } });
    expect(screen.getByText("nuvem")).toBeInTheDocument();
  });

  it("abre a config ao tocar a engrenagem quando onOpenConfig é passada", async () => {
    const user = userEvent.setup();
    const opened: string[] = [];
    render(Device, { props: { device: lockView(), onOpenConfig: (id: string) => opened.push(id) } });

    await user.click(screen.getByRole("button", { name: "Configurar dispositivo" }));

    expect(opened).toEqual(["lock1"]);
  });

  it("nao desenha a engrenagem sem onOpenConfig", () => {
    render(Device, { props: { device: lockView() } });
    expect(screen.queryByRole("button", { name: "Configurar dispositivo" })).toBeNull();
  });
});
