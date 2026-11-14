<script lang="ts">
  // Chips de leitura crua do estado do device (dp -> valor), limitados aos 12
  // primeiros DPs. Servem de conferencia rapida dentro da configuracao.
  import { fmtValue } from "$lib/format";
  import { dpLabel, type TuyaView } from "./logic";

  let { device }: { device: TuyaView } = $props();

  const state = $derived(device.state ?? {});
  const specs = $derived(device.specs ?? []);
  const keys = $derived(Object.keys(state).slice(0, 12));
</script>

{#if keys.length}
  <div class="flex flex-wrap gap-1.5">
    {#each keys as dp (dp)}
      {@const spec = specs.find((x) => x.dp_id === dp)}
      <span
        class="rounded-full border border-outline-variant bg-surface-container-low px-2 py-0.5 font-label-sm text-label-sm text-on-surface-variant"
      >
        {spec ? dpLabel(spec) : dp}: {fmtValue(state[dp])}
      </span>
    {/each}
  </div>
{/if}
