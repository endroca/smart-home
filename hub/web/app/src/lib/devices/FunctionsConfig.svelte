<!-- Configuracao das funcoes do device: rotulos que a voz reconhece e as acoes
     que "desligar tudo" envia. O catalogo vem derivado das capacidades; aqui o
     operador ajusta rotulo, apelidos e as flags. Compartilhado por todo layout. -->
<script lang="ts">
  import { api } from "$lib/api";
  import { hub } from "$lib/stores/data.svelte";
  import { toast } from "$lib/stores/toast.svelte";
  import type { DeviceFunction, FunctionCatalog } from "$lib/types";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import ConfigSection from "$lib/ui/ConfigSection.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import Input from "$lib/ui/Input.svelte";
  import Select from "$lib/ui/Select.svelte";
  import SmallSwitch from "$lib/ui/SmallSwitch.svelte";

  let { deviceId }: { deviceId: string } = $props();

  let catalog = $state<FunctionCatalog | null>(null);
  let rows = $state<DeviceFunction[]>([]);
  let loading = $state(false);
  let saving = $state(false);
  let aliasesText = $state<Record<string, string>>({});
  let error = $state("");

  const isFan = $derived(catalog?.driver === "fan");

  async function load() {
    loading = true;
    error = "";
    try {
      const r = await api.deviceFunctions(deviceId);
      apply(r.catalog);
      hub.setFunctionLabels(deviceId, r.catalog);
    } catch (e) {
      error = (e as Error).message;
    } finally {
      loading = false;
    }
  }

  function apply(cat: FunctionCatalog) {
    catalog = cat;
    rows = cat.functions.map((f) => ({ ...f }));
    const text: Record<string, string> = {};
    for (const f of rows) text[f.key] = (f.aliases ?? []).join(", ");
    aliasesText = text;
  }

  // Recarrega quando o device muda (a tela de detalhe e reaproveitada).
  $effect(() => {
    void deviceId;
    void load();
  });

  function setAliases(key: string, value: string) {
    aliasesText = { ...aliasesText, [key]: value };
    const aliases = value
      .split(",")
      .map((a) => a.trim())
      .filter(Boolean);
    rows = rows.map((f) => (f.key === key ? { ...f, aliases } : f));
  }

  function toggleVoice(key: string) {
    rows = rows.map((f) => (f.key === key ? { ...f, voice: !f.voice } : f));
  }

  function toggleOff(key: string) {
    rows = rows.map((f) => (f.key === key ? { ...f, off: !f.off } : f));
  }

  function setOffValue(key: string, value: unknown) {
    rows = rows.map((f) => (f.key === key ? { ...f, off_value: value } : f));
  }

  function offValueText(f: DeviceFunction): string {
    return f.off_value == null ? "" : String(f.off_value);
  }

  async function save() {
    saving = true;
    try {
      const r = await api.saveDeviceFunctions(deviceId, rows);
      apply(r.catalog);
      hub.setFunctionLabels(deviceId, r.catalog);
      toast.show("Funções salvas");
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      saving = false;
    }
  }

  async function reset() {
    if (!confirm("Restaurar os padrões derivados deste dispositivo?")) return;
    saving = true;
    try {
      const r = await api.resetDeviceFunctions(deviceId);
      apply(r.catalog);
      hub.setFunctionLabels(deviceId, r.catalog);
      toast.show("Padrões restaurados");
    } catch (e) {
      toast.show((e as Error).message, true);
    } finally {
      saving = false;
    }
  }
</script>

{#if loading && !catalog}
  <p class="font-label-sm text-label-sm text-on-surface-variant">Carregando funções…</p>
{:else if error}
  <p class="font-label-sm text-label-sm text-error">Falha ao carregar funções: {error}</p>
{:else if catalog && rows.length}
  <ConfigSection title="Funções e voz">
    <p class="font-label-sm text-label-sm text-on-surface-variant">
      {#if isFan}
        Cada função é um comando do ventilador. Marque <strong>Usar na voz</strong> para falar o rótulo e
        <strong>Desligar</strong> para o botão “Desligar tudo” agir nela.
      {:else}
        Rótulos que a voz reconhece neste dispositivo e o que “Desligar tudo” envia.
      {/if}
    </p>

    {#each rows as f (f.key)}
      <div class="flex flex-col gap-2 rounded-2xl border border-outline-variant/50 bg-surface-container-low p-3">
        <div class="flex items-center gap-2">
          <Input bind:value={f.label} placeholder={f.key} cls="flex-1" />
          <span class="shrink-0 rounded-lg bg-surface-container-highest px-2 py-1 font-mono text-[11px] text-on-surface-variant">{f.key}</span>
        </div>

        <input
          type="text"
          value={aliasesText[f.key] ?? ""}
          placeholder="Apelidos separados por vírgula"
          oninput={(e) => setAliases(f.key, e.currentTarget.value)}
          class="w-full rounded-xl border-outline-variant bg-surface-container-low font-body-md text-body-md text-on-surface"
        />

        <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
          <label class="flex items-center gap-2 font-label-sm text-label-sm text-on-surface-variant">
            <SmallSwitch on={f.voice} label="Usar na voz" onclick={() => toggleVoice(f.key)} />
            Usar na voz
          </label>
          <label class="flex items-center gap-2 font-label-sm text-label-sm text-on-surface-variant">
            <SmallSwitch on={f.off} label="Desligar" onclick={() => toggleOff(f.key)} />
            Desligar
          </label>
          {#if f.off && f.kind === "enum" && f.values?.length}
            <div class="flex items-center gap-2">
              <span class="font-label-sm text-label-sm text-on-surface-variant">Envia</span>
              <Select
                value={offValueText(f)}
                options={f.values.map((v) => ({ value: v, label: v }))}
                onchange={(v) => setOffValue(f.key, v)}
                cls="px-2 py-1"
              />
            </div>
          {:else if f.off && f.kind === "number"}
            <div class="flex items-center gap-2">
              <span class="font-label-sm text-label-sm text-on-surface-variant">Envia</span>
              <input
                type="number"
                value={offValueText(f)}
                min={f.min}
                max={f.max}
                step={f.step}
                oninput={(e) => setOffValue(f.key, Number(e.currentTarget.value))}
                class="w-24 rounded-xl border-outline-variant bg-surface-container-low font-body-md text-body-md text-on-surface"
              />
            </div>
          {/if}
        </div>
      </div>
    {/each}

    <div class="flex flex-wrap gap-2">
      <AsyncButton label="Salvar funções" icon="check" variant="primary" busy={saving} onclick={save} />
      {#if catalog.configured}
        <GhostButton label="Restaurar padrões" icon="refresh" disabled={saving} onclick={reset} />
      {/if}
    </div>
  </ConfigSection>
{/if}
