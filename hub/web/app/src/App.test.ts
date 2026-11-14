import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/svelte";
import { setApi } from "$lib/api";
import { makeFakeApi, type FakeApi } from "$lib/test/fakeApi";
import App from "./App.svelte";

let fake: FakeApi;
beforeEach(() => {
  fake = makeFakeApi();
  setApi(fake.api);
});
afterEach(async () => {
  // Deixa o load() assincrono terminar antes de devolver a API real.
  await new Promise((r) => setTimeout(r, 0));
  setApi(null);
});

describe("App (shell)", () => {
  it("monta a marca do hub", () => {
    render(App);
    const heading = screen.getByRole("heading", { level: 1 });
    expect(heading.textContent?.replace(/\s+/g, " ").trim()).toBe("smart home");
  });

  it("mostra a navegacao inferior com as quatro areas", () => {
    render(App);
    const nav = screen.getByRole("navigation", { name: "Navegação principal" });
    const labels = within(nav)
      .getAllByRole("button")
      .map((b) => b.textContent?.trim());
    expect(labels).toEqual(["Início", "Cômodos", "Automações", "Ajustes"]);
  });

  it("carrega o snapshot do hub ao montar", async () => {
    render(App);
    await waitFor(() => {
      const methods = fake.calls.map((c) => c.method);
      expect(methods).toContain("things");
      expect(methods).toContain("rooms");
      expect(methods).toContain("devices");
    });
  });
});
