// O hub padroniza os icones em CONTORNO (stroke): este teste barra a volta de
// glifos preenchidos, que quebravam a coerencia visual entre os cards.

import { describe, expect, it } from "vitest";
import { ICONS } from "./icons";

describe("ICONS", () => {
  it("cada icone e apenas o miolo do <svg> (string)", () => {
    for (const [name, path] of Object.entries(ICONS)) {
      expect(typeof path, name).toBe("string");
      expect(path.length, name).toBeGreaterThan(0);
    }
  });

  it("nenhum icone define fill/stroke proprios (contorno vem do Icon.svelte)", () => {
    for (const [name, path] of Object.entries(ICONS)) {
      expect(path, name).not.toMatch(/fill=/i);
      expect(path, name).not.toMatch(/stroke=/i);
    }
  });

  it("mantem o fallback `dot` e os icones usados pelos cards", () => {
    for (const name of ["dot", "power", "lightbulb", "plug", "bolt", "sparkle", "swap_vert"]) {
      expect(ICONS[name], name).toBeTruthy();
    }
  });
});
