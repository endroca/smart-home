// Layout Fechadura (Tuya). A inferencia o escolhe pela categoria do device
// (jtmspro, ms, mk, ...). A fechadura dorme, entao e lida pela nuvem: o card
// mostra bateria, alarmes e os ultimos metodos de abertura; o destrancar remoto
// vem numa fase posterior.

import { registerDevice } from "$lib/devices/registry";
import Device from "./Device.svelte";
import Config from "./config/Config.svelte";
import { isLockCategory, type LockView } from "./logic";

registerDevice({
  id: "tuya.lock",
  label: "Fechadura",
  kinds: ["tuya"],
  hint: "Bateria, alarmes e métodos de abertura de uma fechadura Tuya (lida pela nuvem).",
  match: (d) => d.kind === "tuya" && isLockCategory((d as LockView).category),
  component: Device,
  config: Config,
});
