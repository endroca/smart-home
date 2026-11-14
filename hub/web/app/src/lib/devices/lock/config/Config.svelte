<script lang="ts">
  // Configuração da fechadura: uma nota sobre o modelo de leitura (nuvem), os
  // data points que não cabem no card e as seções compartilhadas do módulo Tuya
  // (conexão, ações, último evento).
  import ConfigSection from "$lib/ui/ConfigSection.svelte";
  import TuyaConfig from "../../tuya/Config.svelte";
  import { lockExtraSpecs, type LockView } from "../logic";

  let { device }: { device: LockView } = $props();

  const extras = $derived(lockExtraSpecs(device.specs));
</script>

<ConfigSection title="Fechadura">
  <div
    class="mt-space-md rounded-lg bg-surface-container-low/60 px-space-md py-space-sm font-label-sm text-label-sm text-on-surface-variant"
  >
    Dispositivo de nuvem (dorme em Wi‑Fi): o hub lê o estado pela Tuya e não mantém uma conexão local.
    {#if device.category}· Categoria <span class="font-mono">{device.category}</span>{/if}
  </div>
</ConfigSection>

<TuyaConfig {device} advanced={extras} advancedTitle="Data points da fechadura" />
