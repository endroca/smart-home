<script lang="ts">
  // Configuracao do interruptor multi-tecla: as teclas ficam no card, entao aqui
  // so entram os DPs que configuram o aparelho (backlight, pulsacao, rele,
  // countdowns, ...) mais as secoes compartilhadas do modulo tuya.
  import TuyaConfig from "../../tuya/Config.svelte";
  import { TELEMETRY, tuyaGangSpecs, type TuyaView } from "../../tuya/logic";

  let { device }: { device: TuyaView } = $props();

  const specs = $derived(device.specs ?? []);
  const gangIds = $derived(new Set(tuyaGangSpecs(specs).map((s) => s.dp_id)));
  const extras = $derived(
    specs.filter(
      (s) =>
        s.writable !== false &&
        !gangIds.has(s.dp_id) &&
        TELEMETRY.every((t) => !t.codes.includes(s.code)),
    ),
  );
</script>

<TuyaConfig {device} advanced={extras} advancedTitle="Ajustes do interruptor" />
