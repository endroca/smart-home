// Registro do layout do ar-condicionado Midea no registry de devices. O import
// tem efeito colateral (registerDevice); o nucleo resolve o card pelo kind.
import { registerDevice } from "$lib/devices/registry";
import Device from "./Device.svelte";
import Config from "./config/Config.svelte";

registerDevice({
  id: "midea.ac",
  label: "Ar-condicionado Midea",
  kinds: ["midea"],
  hint: "Temperatura, modo, ventilação e funções do ar-condicionado.",
  match: (d) => d.kind === "midea",
  component: Device,
  config: Config,
});
