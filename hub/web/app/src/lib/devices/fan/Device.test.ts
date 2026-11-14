import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/svelte";
import { setApi } from "$lib/api";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import { cap, deviceRow, manifestDevice, thing } from "$lib/test/fixtures";
import Device from "./Device.svelte";
import { buildFans } from "./logic";

const caps = { power: cap("boolean"), speed: cap("enum"), light: cap("boolean"), direction: cap("enum") };

function fan(reported: Record<string, unknown>) {
  const t = thing("rf01", {
    devices: [manifestDevice("fan0", "Ventilador", caps, ["fan.rename", "learn.start"])],
    reported: { fan0: reported },
  });
  return buildFans([t], [])[0];
}

function tap(el: Element) {
  fireEvent.pointerDown(el);
  fireEvent.pointerUp(el);
}

let fake: FakeApi;
beforeEach(() => {
  fake = makeFakeApi();
  setApi(fake.api);
});
afterEach(async () => {
  // Deixa o hub.load() das acoes terminar antes de devolver a API real.
  await new Promise((r) => setTimeout(r, 0));
  setApi(null);
});

describe("fan/Device", () => {
  it("mostra o nome e a velocidade atual", () => {
    render(Device, { props: { device: fan({ power: true, speed: 3, direction: "forward" }) } });
    expect(screen.getByRole("heading", { level: 3 }).textContent).toBe("Ventilador");
    expect(screen.getByText("3/6 • Ativo")).toBeInTheDocument();
  });

  it("tocar numa velocidade envia RF (setFan com force)", () => {
    render(Device, { props: { device: fan({ power: true, speed: 0 }) } });
    tap(screen.getByLabelText("Velocidade 4: Forte"));
    expect(fake.calls).toContainEqual({
      method: "post",
      args: ["/api/things/rf01/devices/fan0/state", { state: { speed: 4 }, force: true }],
    });
  });

  it("tocar em Desligar manda SO power:false (um frame)", () => {
    render(Device, { props: { device: fan({ power: true, speed: 3, light: true }) } });
    tap(screen.getByLabelText("Desligar"));
    expect(fake.calls).toContainEqual({
      method: "post",
      args: ["/api/things/rf01/devices/fan0/state", { state: { power: false }, force: true }],
    });
  });

  it("segurar corrige o estado sem RF", async () => {
    render(Device, { props: { device: fan({ power: true, speed: 2 }) } });
    const el = screen.getByLabelText("Velocidade 5: Turbo");
    fireEvent.pointerDown(el);
    await new Promise((r) => setTimeout(r, 600));
    fireEvent.pointerUp(el);
    expect(fake.calls).toContainEqual({
      method: "post",
      args: ["/api/things/rf01/devices/fan0/correct", { state: { speed: 5 } }],
    });
  });

  it("segurar NAO e cancelado se o dedo sair do controle (pointerleave)", async () => {
    render(Device, { props: { device: fan({ power: true, speed: 2 }) } });
    const el = screen.getByLabelText("Velocidade 6: Max");
    fireEvent.pointerDown(el);
    fireEvent.pointerLeave(el);
    await new Promise((r) => setTimeout(r, 600));
    fireEvent.pointerUp(el);
    expect(fake.calls).toContainEqual({
      method: "post",
      args: ["/api/things/rf01/devices/fan0/correct", { state: { speed: 6 } }],
    });
    // e o toque simples nao roda junto
    expect(fake.calls.some((c) => JSON.stringify(c.args).includes('"force":true'))).toBe(false);
  });

  it("segurar corrige sem RF no Desligar e na Luz", async () => {
    render(Device, { props: { device: fan({ power: true, speed: 3, light: false }) } });

    const off = screen.getByLabelText("Desligar");
    fireEvent.pointerDown(off);
    await new Promise((r) => setTimeout(r, 600));
    fireEvent.pointerUp(off);
    expect(fake.calls).toContainEqual({
      method: "post",
      args: ["/api/things/rf01/devices/fan0/correct", { state: { power: false } }],
    });

    const light = screen.getByLabelText("Luz");
    fireEvent.pointerDown(light);
    await new Promise((r) => setTimeout(r, 600));
    fireEvent.pointerUp(light);
    expect(fake.calls).toContainEqual({
      method: "post",
      args: ["/api/things/rf01/devices/fan0/correct", { state: { light: true } }],
    });
  });

  it("pointercancel (scroll/gesto do sistema) nao dispara nada", async () => {
    render(Device, { props: { device: fan({ power: true, speed: 2 }) } });
    const el = screen.getByLabelText("Velocidade 4: Forte");
    fireEvent.pointerDown(el);
    await new Promise((r) => setTimeout(r, 100));
    fireEvent.pointerCancel(el);
    await new Promise((r) => setTimeout(r, 600));
    fireEvent.pointerUp(el);
    const posts = fake.calls.filter((c) => c.method === "post");
    expect(posts).toEqual([]);
  });

  it("mostra o card de sincronizacao quando falta o manifesto", () => {
    const pending = buildFans([], [deviceRow({ id: "rf01/fan1", kind: "fan", owner: "rf01", device_id: "fan1", name: "Novo" })])[0];
    render(Device, { props: { device: pending } });
    expect(screen.getByText(/ainda não recebeu o manifesto/)).toBeInTheDocument();
  });

  it("chama onOpenConfig pela engrenagem", async () => {
    const opened: string[] = [];
    render(Device, { props: { device: fan({ power: false, speed: 0 }), onOpenConfig: (id: string) => opened.push(id) } });
    await fireEvent.click(screen.getByLabelText("Configurar dispositivo"));
    expect(opened).toEqual(["rf01/fan0"]);
  });
});
