// Teste do "Ou personalize a ação": um interruptor de três chaves agenda a
// chave escolhida, não só a chave mestre.

import { render, screen } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { tick } from "svelte";
import { setApi } from "$lib/api";
import { hub } from "$lib/stores/data.svelte";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import { tuyaSpec } from "$lib/test/fixtures";
import type { Automation, HubDevice } from "$lib/types";
import TimerSheet from "./TimerSheet.svelte";

const device: HubDevice = {
  id: "sw1",
  kind: "tuya",
  name: "Interruptor",
  room: "",
  online: true,
  template: "",
  specs: [
    tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean" }),
    tuyaSpec({ dp_id: "2", code: "switch_2", type: "Boolean" }),
    tuyaSpec({ dp_id: "3", code: "switch_3", type: "Boolean" }),
  ],
} as HubDevice;

let fake: FakeApi;

beforeEach(() => {
  fake = makeFakeApi();
  setApi(fake.api);
  hub.things = [];
  hub.devices = [];
  hub.rooms = [];
  hub.automations = [];
  hub.specs = {};
});

afterEach(() => {
  setApi(null);
});

describe("Temporizador personalizado", () => {
  it("agenda a chave 2 de um interruptor de três chaves", async () => {
    const user = userEvent.setup();
    render(TimerSheet, { props: { device, open: true } });

    await user.click(screen.getByRole("button", { name: /Ou personalize a ação/ }));
    await tick();

    const action = screen.getByLabelText("Ação");
    await user.selectOptions(action, "2");
    await tick();

    const value = screen.getByLabelText("Valor");
    await user.selectOptions(value, "false");
    await tick();

    await user.click(screen.getByRole("button", { name: "em 30 min" }));
    await tick();

    const call = fake.calls.find((c) => c.method === "post" && c.args[0] === "/api/automations");
    expect(call).toBeTruthy();
    const body = call!.args[1] as Automation;
    expect(body.once).toBe(true);
    expect(body.source).toEqual({ type: "time", in_seconds: 1800 });
    expect(body.actions).toEqual([{ kind: "tuya", device: "sw1", dp: "2", value: false }]);
    expect(body.name).toBe("Interruptor: Desligar Chave 2 daqui a 30 min");
  });
});