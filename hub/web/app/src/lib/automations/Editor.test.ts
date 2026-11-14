// Teste de integracao do editor visual: o fluxo normal nao expoe JSON e o
// seletor de eventos e alimentado pelas specs do dispositivo.

import { render, screen } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { tick } from "svelte";
import { automationTemplate } from "$lib/actions/automations";
import { hub } from "$lib/stores/data.svelte";
import { deviceRow, sampleRooms, tuyaDevice, tuyaSpec } from "$lib/test/fixtures";
import Editor from "./Editor.svelte";

beforeEach(() => {
  hub.rooms = sampleRooms;
  hub.devices = [deviceRow({ id: "bf123", kind: "tuya", name: "Tomada", room_id: "sala" })];
  hub.tuya.devices = [tuyaDevice({ id: "bf123", name: "Tomada", room_id: "sala" })];
  hub.specs = {
    bf123: [tuyaSpec({ dp_id: "1", code: "switch_1", type: "Boolean", name: "Chave 1" })],
  };
  hub.midea.devices = [];
  hub.things = [];
});

describe("Editor de automação", () => {
  it("abre no formulário amigável, sem textarea de JSON", () => {
    render(Editor, {
      props: { automation: automationTemplate(), open: true, onsave: async () => {} },
    });

    expect(screen.getByText("Quando")).toBeInTheDocument();
    expect(screen.getByText("Só neste horário")).toBeInTheDocument();
    expect(screen.getByText("Então")).toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: /json/i })).not.toBeInTheDocument();
    expect(screen.getByText("Avançado (JSON)")).toBeInTheDocument();
  });

  it("carrega o evento do dispositivo escolhido", async () => {
    const user = userEvent.setup();
    render(Editor, {
      props: { automation: automationTemplate(), open: true, onsave: async () => {} },
    });

    const device = screen.getByLabelText(/Dispositivo ou cena/);
    await user.selectOptions(device, "tuya:bf123");
    await tick();

    const event = screen.getByLabelText(/Função/);
    expect(event).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Chave 1" })).toBeInTheDocument();

    await user.selectOptions(event, "1");
    await tick();
    const condition = screen.getByLabelText("Condição");
    expect(condition).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Ligar" })).toBeInTheDocument();
  });

  it("não reinicia o formulário quando o hub atualiza (SSE/poll)", async () => {
    const user = userEvent.setup();
    render(Editor, {
      props: { automation: automationTemplate(), open: true, onsave: async () => {} },
    });

    const name = screen.getByLabelText("Nome");
    await user.clear(name);
    await user.type(name, "Chegada");
    expect(name).toHaveValue("Chegada");

    // Uma atualização do store no meio da edição (evento SSE ou o poll de 30s)
    // não pode apagar o que já foi preenchido.
    hub.tuya.devices = [...hub.tuya.devices];
    hub.specs = { ...hub.specs };
    await tick();

    expect(screen.getByLabelText("Nome")).toHaveValue("Chegada");
  });

  it("oferece o gatilho de horário e esconde o dispositivo", async () => {
    const user = userEvent.setup();
    render(Editor, {
      props: { automation: automationTemplate(), open: true, onsave: async () => {} },
    });

    const trigger = screen.getByLabelText(/Dispositivo ou cena/);
    await user.selectOptions(trigger, "__time__");
    await tick();

    expect(screen.getByText("Daqui a…")).toBeInTheDocument();
    expect(screen.getByText("Às HH:MM")).toBeInTheDocument();
    // Sem dispositivo/evento nem "Só neste horário" no gatilho de horário.
    expect(screen.queryByLabelText(/Função/)).not.toBeInTheDocument();
    expect(screen.queryByText("Só neste horário")).not.toBeInTheDocument();
  });
});