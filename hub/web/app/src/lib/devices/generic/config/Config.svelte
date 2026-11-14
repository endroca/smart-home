<script lang="ts">
  import ConfigSection from "$lib/ui/ConfigSection.svelte";
  import TuyaConfig from "../../tuya/Config.svelte";
  import type { TuyaView } from "../../tuya/logic";
  import { genericChips, type GenericView } from "../logic";

  let { device }: { device: GenericView } = $props();

  const chips = $derived(genericChips(device));
  const asTuya = $derived(device as unknown as TuyaView);
</script>

{#if device.kind === "tuya"}
  <TuyaConfig device={asTuya} advanced={device.specs ?? []} />
{:else if chips.length}
  <ConfigSection title="Estado conhecido">
    <div class="flex flex-wrap gap-1.5">
      {#each chips as c (c.label)}
        <span class="rounded-full border border-outline-variant bg-surface-container-low px-2 py-0.5 font-label-sm text-label-sm text-on-surface-variant">
          {c.label}: {c.value}
        </span>
      {/each}
    </div>
  </ConfigSection>
{:else}
  <p class="py-6 text-center font-body-sm text-body-sm text-on-surface-variant">Nada para configurar neste dispositivo.</p>
{/if}
