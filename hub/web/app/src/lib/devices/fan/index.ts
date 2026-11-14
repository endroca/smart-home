import { registerDevice } from "$lib/devices/registry";
import Device from "./Device.svelte";
import Config from "./config/Config.svelte";

registerDevice({
  id: "fan.ventisol",
  label: "Ventilador (Ventisol)",
  kinds: ["fan"],
  hint: "Rotor, velocidade 0–6, direção e luz.",
  match: (d) => d.kind === "fan",
  component: Device,
  config: Config,
});
