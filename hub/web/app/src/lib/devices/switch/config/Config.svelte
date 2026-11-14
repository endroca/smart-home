<script lang="ts">
  // Configuracao do interruptor: controles extras nao-booleanos (brilho, modo de
  // trabalho, ...) que nao caberiam no card, mais as secoes compartilhadas do
  // modulo tuya.
  import TuyaConfig from "../../tuya/Config.svelte";
  import { TELEMETRY, type TuyaView } from "../../tuya/logic";

  let { device }: { device: TuyaView } = $props();

  const specs = $derived(device.specs ?? []);
  const extras = $derived(
    specs.filter((s) => s.type !== "Boolean" && TELEMETRY.every((t) => !t.codes.includes(s.code))),
  );
</script>

<TuyaConfig {device} advanced={extras} advancedTitle="Controles extras" />
