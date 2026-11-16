<script lang="ts">
  // Controles avancados de um device Tuya: cada spec vira uma linha com o
  // controle certo para o seu tipo (Boolean, Enum, Integer/Number, texto).
  // Usado dentro da configuracao (modal de detalhe).
  //
  // O campo le SEMPRE o valor do device: o estado chega por DP (uma escrita
  // notifica apenas quem le aquele DP) e a view do device nao e recriada a cada
  // atualizacao, entao um update que nao e deste campo nao o re-renderiza.
  import SmallSwitch from "$lib/ui/SmallSwitch.svelte";
  import Select from "$lib/ui/Select.svelte";
  import { toast } from "$lib/stores/toast.svelte";
  import type { TuyaSpec } from "$lib/types";
  import { dpDesc, dpLabel, tuyaVal, type TuyaView } from "./logic";
  import { tuyaControl } from "./actions";

  let { device, specs = null }: { device: TuyaView; specs?: TuyaSpec[] | null } = $props();

  const list = $derived((specs ?? device.specs ?? []).filter((s) => s.writable !== false));

  /** CONFIRM_MS e quanto esperamos o aparelho reportar de volta o valor enviado. */
  const CONFIRM_MS = 4000;

  // Escritas pendentes: servem APENAS para avisar quando o aparelho ignora o
  // comando. Nao influenciam o que o campo mostra.
  const pending: Record<string, { text: string; timer: ReturnType<typeof setTimeout> }> = {};

  // enumValues normaliza `values` (lista crua ou objeto {range:[...]}).
  function enumValues(spec: TuyaSpec): string[] {
    let v = spec.values;
    if (v && !Array.isArray(v) && Array.isArray((v as { range?: unknown }).range)) {
      v = (v as { range: unknown[] }).range;
    }
    return Array.isArray(v) ? v.map((x) => String(x)) : [];
  }

  // enumOptions lista o range declarado MAIS o valor que o aparelho reporta.
  // Sem isso um valor fora do range (o aparelho manda "off" onde a nuvem declara
  // power_off/power_on/last) nao casa com nenhuma opcao e o <select> fica em
  // branco — o navegador nao tem o que selecionar.
  function enumOptions(spec: TuyaSpec): { value: string; label: string }[] {
    const values = enumValues(spec);
    const current = deviceText(spec);
    if (current !== "" && !values.includes(current)) values.push(current);
    return values.map((v) => ({ value: v, label: v }));
  }

  // numMeta extrai min/max/step de `values` para os campos numericos.
  function numMeta(spec: TuyaSpec): { min?: number; max?: number; step?: number } {
    const v = (spec.values as { min?: number; max?: number; step?: number } | null) ?? {};
    return { min: v.min, max: v.max, step: v.step };
  }

  function deviceText(spec: TuyaSpec): string {
    const val = tuyaVal(device, spec);
    return val != null ? String(val) : "";
  }

  // commit envia o valor e agenda o aviso de "nao confirmado": se em CONFIRM_MS
  // o aparelho ainda reportar outra coisa, o operador fica sabendo (em vez de a
  // tela mentir sobre o aparelho).
  function commit(spec: TuyaSpec, text: string, value: unknown) {
    if (text === deviceText(spec)) return;
    void tuyaControl(device.id, spec.dp_id, value);
    clearTimeout(pending[spec.dp_id]?.timer);
    const timer = setTimeout(() => {
      delete pending[spec.dp_id];
      if (deviceText(spec) !== text) {
        toast.show(`O dispositivo não confirmou “${text}” em ${dpLabel(spec)}`, true);
      }
    }, CONFIRM_MS);
    pending[spec.dp_id] = { text, timer };
  }

  $effect(() => () => {
    for (const p of Object.values(pending)) clearTimeout(p.timer);
  });

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
        value={deviceText(spec)}
        options={enumOptions(spec)}
        onchange={(v) => commit(spec, v, v)}
        cls="max-w-full shrink-0"
      />
    {:else if spec.type === "Integer" || spec.type === "Number"}
      {@const meta = numMeta(spec)}
      <input
        type="number"
        min={meta.min}
        max={meta.max}
        step={meta.step ?? 1}
        value={deviceText(spec)}
        onchange={(e) => {
          const text = (e.currentTarget as HTMLInputElement).value;
          commit(spec, text, Number(text));
        }}
        class="w-24 max-w-full shrink-0 rounded-lg border-outline-variant bg-surface-container text-xs text-on-surface"
      />
    {:else}
      <input
        type="text"
        value={deviceText(spec)}
        onchange={(e) => {
          const text = (e.currentTarget as HTMLInputElement).value;
          commit(spec, text, text);
        }}
        class="w-40 max-w-full shrink-0 rounded-lg border-outline-variant bg-surface-container text-xs text-on-surface"
      />
    {/if}
  </div>
{/each}
