// Layout Tomada / Plug Tuya: chave mestre + telemetria de energia. Registra-se
// no nucleo de devices para que a inferencia o escolha quando o device expoe
// alguma grandeza de medicao (cur_power/cur_voltage/cur_current).

import { registerDevice } from "$lib/devices/registry";
import Device from "./Device.svelte";
import Config from "./config/Config.svelte";
import { tuyaHasTelemetry, type TuyaView } from "../tuya/logic";

registerDevice({
  id: "tuya.socket",
  label: "Tomada / Plug (telemetria)",
  kinds: ["tuya"],
  hint: "Chave mestre + potência, tensão e corrente.",
  match: (d) => d.kind === "tuya" && tuyaHasTelemetry((d as TuyaView).specs ?? []),
  component: Device,
  config: Config,
});
