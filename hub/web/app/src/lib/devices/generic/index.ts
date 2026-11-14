import { registerDevice } from "$lib/devices/registry";
import Device from "./Device.svelte";
import Config from "./config/Config.svelte";

// Fallback do registry: aceita qualquer device e mostra o que se sabe dele.
registerDevice({
  id: "generic",
  label: "Genérico (controles)",
  kinds: ["fan", "tuya", "midea", "thing"],
  hint: "Renderiza os controles conhecidos do dispositivo.",
  match: () => true,
  component: Device,
  config: Config,
});
