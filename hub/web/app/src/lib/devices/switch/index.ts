// Layout Interruptor / Luz Tuya: chaves liga/desliga e controles extras.
// Registra-se no nucleo de devices; a inferencia o escolhe quando o device tem
// specs mas nenhuma telemetria de energia (senao a tomada vence).

import { registerDevice } from "$lib/devices/registry";
import Device from "./Device.svelte";
import Config from "./config/Config.svelte";
import type { TuyaView } from "../tuya/logic";

registerDevice({
  id: "tuya.switch",
  label: "Interruptor / Luz",
  kinds: ["tuya"],
  hint: "Chaves liga/desliga e controles extras.",
  match: (d) => d.kind === "tuya" && ((d as TuyaView).specs ?? []).length > 0,
  component: Device,
  config: Config,
});
