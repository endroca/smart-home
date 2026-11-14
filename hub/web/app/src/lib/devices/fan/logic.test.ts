import { describe, expect, it } from "vitest";
import { SPEED_LABELS, buildFans, fanControls, fanSpeedClass, manifestFanData } from "./logic";
import { cap, deviceRow, manifestDevice, thing } from "$lib/test/fixtures";

const fullCaps = {
  power: cap("boolean"),
  speed: cap("enum"),
  light: cap("boolean"),
  direction: cap("enum"),
};

describe("fan/logic", () => {
  it("monta os fans a partir do manifesto", () => {
    const t = thing("rf01", {
      devices: [manifestDevice("fan0", "Sala", fullCaps, ["fan.rename"])],
      reported: { fan0: { power: true, speed: 3, light: false, direction: "forward", source: "rf" } },
    });
    const fans = buildFans([t], []);
    expect(fans).toHaveLength(1);
    expect(fans[0]).toMatchObject({
      id: "rf01/fan0",
      thing: "rf01",
      device: "fan0",
      name: "Sala",
      kind: "fan",
      source: "rf",
      pendingManifest: false,
      online: true,
    });
  });

  it("o banco e a fonte de verdade: aparece antes do manifesto", () => {
    const rows = [deviceRow({ id: "rf01/fan1", kind: "fan", owner: "rf01", device_id: "fan1", name: "Quarto", room_id: "quarto" })];
    const fans = buildFans([], rows);
    expect(fans).toHaveLength(1);
    expect(fans[0]).toMatchObject({ id: "rf01/fan1", name: "Quarto", room: "quarto", pendingManifest: true, online: false });
  });

  it("nao duplica quando o device esta no banco e no manifesto", () => {
    const rows = [deviceRow({ id: "rf01/fan0", kind: "fan", owner: "rf01", device_id: "fan0" })];
    const t = thing("rf01", { devices: [manifestDevice("fan0", "Sala", fullCaps)] });
    expect(buildFans([t], rows)).toHaveLength(1);
  });

  it("marca pendingManifest quando o manifesto ainda nao chegou", () => {
    const rows = [deviceRow({ id: "rf01/fan0", kind: "fan", owner: "rf01", device_id: "fan0" })];
    expect(buildFans([], rows)[0].pendingManifest).toBe(true);
  });

  it("deriva os controles do reported", () => {
    const t = thing("rf01", {
      devices: [manifestDevice("fan0", "Sala", fullCaps)],
      reported: { fan0: { power: true, speed: 4, light: true, direction: "reverse" } },
    });
    const c = fanControls(buildFans([t], [])[0]);
    expect(c).toEqual({
      on: true,
      speed: 4,
      running: true,
      reverse: true,
      hasLight: true,
      hasDirection: true,
      hasSpeed: true,
    });
  });

  it("so roda com power e velocidade > 0", () => {
    const t = thing("rf01", {
      devices: [manifestDevice("fan0", "Sala", fullCaps)],
      reported: { fan0: { power: true, speed: 0 } },
    });
    expect(fanControls(buildFans([t], [])[0]).running).toBe(false);
  });

  it("sem light/direction nao expoe esses controles", () => {
    const t = thing("rf01", {
      devices: [manifestDevice("fan0", "Sala", { power: cap("boolean"), speed: cap("enum") })],
      reported: { fan0: { power: false, speed: 0 } },
    });
    const c = fanControls(buildFans([t], [])[0]);
    expect(c.hasLight).toBe(false);
    expect(c.hasDirection).toBe(false);
    expect(c.hasSpeed).toBe(true);
  });

  it("manifestFanData indexa por thing/device e usa freshness para online", () => {
    const t = thing("rf01", { devices: [manifestDevice("fan0", "Sala", fullCaps)], online: false });
    t.status = { online: false };
    t.updatedAt = new Date().toISOString();
    const map = manifestFanData([t]);
    expect(map["rf01/fan0"].online).toBe(true); // fresh heartbeat
  });

  it("fanSpeedClass reflete velocidade e direcao", () => {
    expect(fanSpeedClass({ on: true, speed: 3, running: true, reverse: true, hasLight: true, hasDirection: true, hasSpeed: true })).toBe(
      "fan-speed-3 fan-reverse",
    );
  });

  it("SPEED_LABELS cobre 0..6", () => {
    expect(SPEED_LABELS).toHaveLength(7);
    expect(SPEED_LABELS[0]).toBe("Off");
  });
});
