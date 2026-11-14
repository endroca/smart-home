<script lang="ts">
  // Controles avancados de um device Tuya: cada spec vira uma linha com o
  // controle certo para o seu tipo (Boolean, Enum, Integer/Number, texto).
  // Usado dentro da configuracao (modal de detalhe).
  import SmallSwitch from "$lib/ui/SmallSwitch.svelte";
  import Select from "$lib/ui/Select.svelte";
  import type { TuyaSpec } from "$lib/types";
  import { dpDesc, dpLabel, tuyaVal, type TuyaView } from "./logic";
  import { tuyaControl } from "./actions";

  let { device, specs = null }: { device: TuyaView; specs?: TuyaSpec[] | null } = $props();

  const list = $derived((specs ?? device.specs ?? []).filter((s) => s.writable !== false));

  // enumValues normaliza `values` (lista crua ou objeto {range:[...]}).
  function enumValues(spec: TuyaSpec): string[] {
    let v = spec.values;
    if (v && !Array.isArray(v) && Array.isArray((v as { range?: unknown }).range)) {
      v = (v as { range: unknown[] }).range;
    }
    return Array.isArray(v) ? v.map((x) => String(x)) : [];
  }

  // numMeta extrai min/max/step de `values` para os campos numericos.
  function numMeta(spec: TuyaSpec): { min?: number; max?: number; step?: number } {
    const v = (spec.values as { min?: number; max?: number; step?: number } | null) ?? {};
    return { min: v.min, max: v.max, step: v.step };
  }

  const ROW = "flex flex-wrap items-center justify-between gap-2 rounded-xl bg-surface-container-low px-3 py-2.5";
</script>

{#each list as spec (spec.dp_id)}
  {@const val = tuyaVal(device, spec)}
  <div class={ROW}>
    <div class="min-w-[150px] flex-1">
      <div class="break-words font-body-md text-body-md text-on-surface">{dpLabel(spec)}</div>
      {#if dpDesc(spec)}
        <div class="mt-0.5 font-label-sm text-label-sm leading-snug text-on-surface-variant">{dpDesc(spec)}</div>
      {/if}
    </div>

    {#if spec.type === "Boolean"}
      {@const on = val === true}
      <SmallSwitch {on} label={dpLabel(spec)} onclick={() => tuyaControl(device.id, spec.dp_id, !on)} />
    {:else if spec.type === "Enum"}
      <Select
        value={val != null ? String(val) : ""}
        options={enumValues(spec).map((v) => ({ value: v, label: v }))}
        onchange={(v) => tuyaControl(device.id, spec.dp_id, v)}
        cls="max-w-full shrink-0"
      />
    {:else if spec.type === "Integer" || spec.type === "Number"}
      {@const meta = numMeta(spec)}
      <input
        type="number"
        min={meta.min}
        max={meta.max}
        step={meta.step ?? 1}
        value={val != null ? String(val) : ""}
        onchange={(e) => tuyaControl(device.id, spec.dp_id, Number((e.currentTarget as HTMLInputElement).value))}
        class="w-24 max-w-full shrink-0 rounded-lg border-outline-variant bg-surface-container text-xs text-on-surface"
      />
    {:else}
      <input
        type="text"
        value={val != null ? String(val) : ""}
        onchange={(e) => tuyaControl(device.id, spec.dp_id, (e.currentTarget as HTMLInputElement).value)}
        class="w-40 max-w-full shrink-0 rounded-lg border-outline-variant bg-surface-container text-xs text-on-surface"
      />
    {/if}
  </div>
{/each}
