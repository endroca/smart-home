<script lang="ts">
  // Configuracao da tomada: reune os controles avancados (modo do rele, tempo de
  // ciclo, protecao de sobrecarga, ...) que ficariam poluindo o card, mais as
  // secoes compartilhadas (conexao/acoes/evento) do modulo tuya.
  import TuyaConfig from "../../tuya/Config.svelte";
  import { MASTER_CODES, TELEMETRY, specByCode, type TuyaView } from "../../tuya/logic";

  let { device }: { device: TuyaView } = $props();

  const specs = $derived(device.specs ?? []);
  const master = $derived(specByCode(specs, MASTER_CODES));
  const countdown = $derived(specByCode(specs, ["countdown_1", "countdown"]));
  const childLock = $derived(specByCode(specs, ["child_lock"]));
  const advanced = $derived(
    specs.filter(
      (s) =>
        s !== master &&
        s !== countdown &&
        s !== childLock &&
        !TELEMETRY.some((t) => t.codes.includes(s.code)),
    ),
  );
</script>

<TuyaConfig {device} {advanced} advancedTitle="Configurações do dispositivo" />
