// Layout Interruptor multi-tecla (gang) Tuya: uma placa tatil com uma tecla por
// saida switch_N. Registra-se no nucleo de devices; a inferencia o escolhe
// quando o device tem duas ou mais teclas (senao o switch simples vence).

import { registerDevice } from "$lib/devices/registry";
import Device from "./Device.svelte";
import Config from "./config/Config.svelte";
import { tuyaIsMultiGang, type TuyaView } from "../tuya/logic";

registerDevice({
  id: "tuya.gang",
  label: "Interruptor multi-tecla",
  kinds: ["tuya"],
  hint: "Placa com uma tecla por saída (switch_1, switch_2, ...).",
  match: (d) => d.kind === "tuya" && tuyaIsMultiGang((d as TuyaView).specs ?? []),
  component: Device,
  config: Config,
});
