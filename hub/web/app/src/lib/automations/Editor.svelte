<script lang="ts">
  // Editor visual de automacoes. O formulario fala em "Quando / So neste
  // horario / Entao"; o JSON continua existindo apenas como opcao avancada.
  import { untrack } from "svelte";
  import { hub } from "$lib/stores/data.svelte";
  import { buildFans } from "$lib/devices/fan/logic";
  import { buildMideaDevices } from "$lib/devices/midea/logic";
  import { buildTuyaDevices, dpLabel } from "$lib/devices/tuya/logic";
  import AsyncButton from "$lib/ui/AsyncButton.svelte";
  import Field from "$lib/ui/Field.svelte";
  import GhostButton from "$lib/ui/GhostButton.svelte";
  import Input from "$lib/ui/Input.svelte";
  import Select from "$lib/ui/Select.svelte";
  import Sheet from "$lib/ui/Sheet.svelte";
  import Toggle from "$lib/ui/Toggle.svelte";
  import type { Automation } from "$lib/types";
  import {
    ALL_DAYS,
    DAY_OPTIONS,
    actionValueChoices,
    actionsSummary,
    automationFromDraft,
    buildDeviceContracts,
    conditionChoices,
    contractKey,
    emptyDraft,
    fanActionOptions,
    type ActionDraft,
    type AutomationDraft,
    type Choice,
    type ControlKind,
    type ControlOption,
    type DeviceContract,
    type DeviceDriver,
    type EventOption,
    type SummaryLookups,
    draftFromAutomation,
    mideaControlOptions,
    scheduleLabel,
    sourceSummary,
    tuyaControlOptions,
    validateDraft,
  } from "./logic";

  let {
    automation,
    open = $bindable(false),
    onsave,
  }: {
    automation: Automation;
    open?: boolean;
    onsave: (body: Automation) => Promise<void>;
  } = $props();

  const fans = $derived(buildFans(hub.things, hub.devices));
  const tuyaViews = $derived(buildTuyaDevices(hub.tuya.devices, hub.specs, hub.tuya.listening, hub.devices));
  const mideaViews = $derived(buildMideaDevices(hub.midea.devices, hub.devices));
  const contracts = $derived(
    buildDeviceContracts(fans, tuyaViews, mideaViews, hub.specs, hub.rooms),
  );

  let draft = $state<AutomationDraft>(emptyDraft());
  let error = $state("");
  let busy = $state(false);
  let advancedMode = $state(false);
  let advancedText = $state("");

  // O rascunho e (re)carregado quando o painel ABRE — e nunca quando o hub
  // atualiza. Sem isso, um evento SSE/poll no meio da edicao re-executava o
  // efeito (ele lê estado reativo do store ao carregar as specs) e reiniciava o
  // formulario, apagando o que o operador ja tinha preenchido.
  let wasOpen = false;
  $effect(() => {
    const isOpen = open;
    if (isOpen && !wasOpen) {
      untrack(() => {
        draft = draftFromAutomation(automation);
        error = "";
        advancedMode = false;
        advancedText = "";
        void hub.loadSpecsForDevices();
      });
    }
    wasOpen = isOpen;
  });

  function driverForSource(source: string): DeviceDriver {
    if (source === "thing_state") return "fan";
    if (source === "midea_state") return "midea";
    return "tuya";
  }

  const triggerKey = $derived(
    draft.triggerKind === "manual"
      ? "__manual__"
      : draft.triggerKind === "time"
        ? "__time__"
        : draft.triggerDevice
          ? contractKey(driverForSource(draft.triggerKind), draft.triggerDevice)
          : "",
  );

  const triggerOptions = $derived([
    { value: "", label: "Selecione…" },
    { value: "__manual__", label: "Cena manual" },
    { value: "__time__", label: "Horário / Agendamento" },
    ...contracts.map((c) => ({
      value: c.key,
      label: c.room ? `${c.name} · ${c.room}` : c.name,
    })),
  ]);

  const selectedContract = $derived(contracts.find((c) => c.key === triggerKey));

  // withCurrent mantem uma opcao que existe no JSON salvo mas nao esta no
  // catalogo agora (device sem specs, aparelho removido, ...).
  function withCurrentEvent(list: EventOption[], current: string, label: string): EventOption[] {
    if (!current || current === "__custom__" || list.some((e) => e.value === current)) return list;
    return [{ value: current, label: `${label} ${current}`, kind: "text" }, ...list];
  }

  const eventOptions = $derived.by(() => {
    if (draft.triggerKind === "manual" || draft.triggerKind === "time") return [];
    const list = [...(selectedContract?.events ?? [])];
    if (draft.triggerKind !== "thing_state") {
      list.push({
        value: "__custom__",
        label: draft.triggerKind === "midea_state" ? "Outro estado…" : "Outro DP…",
        kind: "text",
      });
    }
    return withCurrentEvent(list, draft.triggerEvent, selectedContract ? "Evento" : "DP");
  });

  const selectedEvent = $derived(eventOptions.find((e) => e.value === draft.triggerEvent));
  const eventValues = $derived.by(() => {
    const list = selectedEvent?.values ?? [];
    if (draft.value && !list.some((v) => v.value === draft.value)) {
      return [{ value: draft.value, label: draft.value }, ...list];
    }
    return list;
  });
  const conditions = $derived.by(() => {
    const list = conditionChoices(selectedEvent);
    if (!draft.condition || list.some((c) => c.value === draft.condition)) return list;
    const label =
      draft.condition === "true" ? "Ligar" : draft.condition === "false" ? "Desligar" : "Mudou para…";
    return [...list, { value: draft.condition, label }];
  });

  function setTriggerChoice(value: string) {
    if (!value) return;
    if (value === "__manual__") {
      draft.triggerKind = "manual";
      draft.triggerDevice = "";
      draft.triggerEvent = "";
      draft.customDP = "";
      draft.condition = "any";
      draft.value = "";
      return;
    }
    if (value === "__time__") {
      draft.triggerKind = "time";
      draft.triggerDevice = "";
      draft.triggerEvent = "";
      draft.customDP = "";
      draft.condition = "any";
      draft.value = "";
      draft.scheduleEnabled = false;
      draft.once = true;
      return;
    }
    const contract = contracts.find((c) => c.key === value);
    if (!contract) return;
    draft.triggerKind = contract.sourceType;
    draft.triggerDevice = contract.id;
    draft.triggerEvent = "";
    draft.customDP = "";
    draft.condition = "any";
    draft.value = "";
  }

  function setEvent(value: string) {
    draft.triggerEvent = value;
    draft.condition = "any";
    draft.value = "";
  }

  function setCondition(value: string) {
    draft.condition = value as AutomationDraft["condition"];
    if (value !== "value") draft.value = "";
  }

  function toggleSchedule() {
    draft.scheduleEnabled = !draft.scheduleEnabled;
    if (draft.scheduleEnabled) {
      if (!draft.scheduleStart) draft.scheduleStart = "18:00";
      if (!draft.scheduleEnd) draft.scheduleEnd = "03:00";
      if (!draft.scheduleDays.length) draft.scheduleDays = [...ALL_DAYS];
    }
  }

  function toggleDay(day: number) {
    const i = draft.scheduleDays.indexOf(day);
    if (i >= 0) {
      if (draft.scheduleDays.length > 1) draft.scheduleDays.splice(i, 1);
      return;
    }
    draft.scheduleDays.push(day);
  }

  // Dias do gatilho semanal. Vazio = disparo único (volátil).
  function toggleTimeDay(day: number) {
    const i = draft.timeDays.indexOf(day);
    if (i >= 0) draft.timeDays.splice(i, 1);
    else draft.timeDays.push(day);
  }

  function actionContract(action: ActionDraft): DeviceContract | undefined {
    return contracts.find((c) => c.driver === action.kind && c.id === action.target);
  }

  function actionKey(action: ActionDraft): string {
    return action.target ? contractKey(action.kind, action.target) : "";
  }

  function actionTargets(): Choice[] {
    return contracts
      .filter((c) => c.actions.length > 0)
      .map((c) => ({
        value: c.key,
        label: c.room ? `${c.name} · ${c.room}` : c.name,
      }));
  }

  function targetsFor(action: ActionDraft): Choice[] {
    const list = actionTargets();
    const current = action.target ? contractKey(action.kind, action.target) : "";
    if (current && !list.some((x) => x.value === current)) {
      return [{ value: current, label: action.target }, ...list];
    }
    return list;
  }

  function controlsFor(action: ActionDraft): ControlOption[] {
    const contract = actionContract(action);
    const list =
      contract?.actions ??
      (action.kind === "fan"
        ? fanActionOptions(fans.find((f) => f.id === action.target))
        : action.kind === "tuya"
          ? tuyaControlOptions(hub.specs[action.target] ?? [])
          : mideaControlOptions(mideaViews.find((d) => d.id === action.target)));
    if (action.control && !list.some((x) => x.value === action.control)) {
      return [{ value: action.control, label: action.control, kind: action.controlKind }, ...list];
    }
    return list;
  }

  function selectedControl(action: ActionDraft): ControlOption | undefined {
    return controlsFor(action).find((c) => c.value === action.control);
  }

  function valuesFor(action: ActionDraft): Choice[] {
    const list = actionValueChoices(selectedControl(action));
    if (action.value && list.length && !list.some((v) => v.value === action.value)) {
      return [{ value: action.value, label: action.value }, ...list];
    }
    return list;
  }

  function defaultActionValue(control: ControlOption | undefined): string {
    if (!control) return "";
    if (control.defaultValue) return control.defaultValue;
    if (control.kind === "bool") return "true";
    if (control.kind === "enum") return control.values?.[0]?.value ?? "";
    if (control.kind === "number") return String(control.min ?? 0);
    return "";
  }

  function setActionTarget(action: ActionDraft, key: string) {
    if (!key) {
      action.target = "";
      action.control = "";
      action.controlKind = "bool";
      action.value = "true";
      return;
    }
    const contract = contracts.find((c) => c.key === key);
    if (contract) {
      action.kind = contract.driver;
      action.target = contract.id;
    } else {
      const i = key.indexOf(":");
      const kind = i === -1 ? "tuya" : key.slice(0, i);
      action.kind = kind === "fan" || kind === "midea" ? kind : "tuya";
      action.target = i === -1 ? key : key.slice(i + 1);
    }
    action.control = "";
    action.controlKind = "bool";
    action.value = "true";
  }

  function setActionControl(action: ActionDraft, value: string) {
    const control = controlsFor(action).find((c) => c.value === value);
    action.control = value;
    action.controlKind = (control?.numeric ? "number" : control?.kind ?? "text") as ControlKind;
    action.value = defaultActionValue(control);
  }

  function addAction() {
    draft.actions.push({ kind: "tuya", target: "", control: "", controlKind: "bool", value: "true" });
  }

  function removeAction(index: number) {
    if (draft.actions.length <= 1) return;
    draft.actions.splice(index, 1);
  }

  function openAdvanced() {
    advancedText = JSON.stringify(automationFromDraft(draft, eventOptions, automation.id || undefined), null, 2);
    advancedMode = true;
  }

  async function save() {
    error = "";
    let body: Automation;
    if (advancedMode) {
      try {
        body = JSON.parse(advancedText) as Automation;
      } catch {
        error = "JSON inválido.";
        return;
      }
      body.id = automation.id || body.id || "";
      if (body.enabled === undefined) body.enabled = true;
    } else {
      const message = validateDraft(draft, eventOptions);
      if (message) {
        error = message;
        return;
      }
      body = automationFromDraft(draft, eventOptions, automation.id || undefined);
    }
    busy = true;
    try {
      await onsave(body);
      open = false;
    } catch (e) {
      error = (e as Error).message;
    } finally {
      busy = false;
    }
  }

  const lookups: SummaryLookups = {
    deviceName: (id) => {
      if (!id) return "";
      const row = hub.devices.find((d) => d.id === id);
      const tuya = hub.tuya.devices.find((d) => d.id === id);
      const midea = hub.midea.devices.find((d) => d.id === id);
      return row?.name || tuya?.name || midea?.name || id;
    },
    tuyaDP: (device, dp) => {
      const specs = hub.specs[device] ?? [];
      const spec = specs.find((s) => s.dp_id === dp) ?? specs.find((s) => s.code === dp);
      return spec ? dpLabel(spec) : dp;
    },
    mideaKey: (device, key) =>
      hub.midea.devices.find((d) => d.id === device)?.actions?.find((a) => a.key === key)?.label || key,
  };

  const preview = $derived.by(() => {
    if (!draft.name.trim()) return "";
    if (draft.triggerKind !== "manual" && draft.triggerKind !== "time" && !draft.triggerDevice) return "";
    if (!draft.actions.some((a) => a.target && a.control)) return "";
    const body = automationFromDraft(draft, eventOptions, automation.id || undefined);
    const src = body.source ?? {};
    const trigger = src.type === "manual" ? "cena manual" : `quando ${sourceSummary(src, eventOptions, lookups)}`;
    const when =
      draft.scheduleEnabled && src.type !== "manual" && src.type !== "time"
        ? ` (${scheduleLabel({ start: draft.scheduleStart, end: draft.scheduleEnd, days: draft.scheduleDays })})`
        : "";
    return `${trigger}${when} → ${actionsSummary(body.actions, lookups)}`;
  });
</script>

<Sheet title={automation.id ? "Editar automação" : "Nova automação"} bind:open cls="max-w-lg">
  <div class="flex flex-col gap-4">
    <Field label="Nome">
      <Input bind:value={draft.name} placeholder="Ex.: Luz da sala ao abrir a porta" />
    </Field>

    <div class="flex items-center justify-between rounded-xl bg-surface-container-low px-3 py-2.5">
      <div>
        <div class="font-body-md text-body-md text-on-surface">Ativa</div>
        <div class="font-label-sm text-label-sm text-on-surface-variant">Desligada, a automação fica salva sem disparar.</div>
      </div>
      <Toggle on={draft.enabled} label="Ativa" onclick={() => (draft.enabled = !draft.enabled)} />
    </div>

    <section class="flex flex-col gap-2">
      <h3 class="font-label-md text-label-md font-semibold text-on-surface">Quando</h3>

      <Field label="Dispositivo ou cena" hint="Escolha o aparelho; as funções dele aparecem abaixo.">
        <Select
          value={triggerKey}
          options={triggerOptions}
          onchange={setTriggerChoice}
          cls="w-full px-2 py-1.5"
        />
      </Field>

      {#if draft.triggerKind === "time"}
        <div class="flex gap-2">
          <button
            type="button"
            class="press flex-1 rounded-xl px-3 py-1.5 font-label-md text-label-md {draft.timeMode === 'relative'
              ? 'bg-primary text-on-primary'
              : 'border border-outline-variant text-on-surface-variant'}"
            onclick={() => (draft.timeMode = "relative")}
          >
            Daqui a…
          </button>
          <button
            type="button"
            class="press flex-1 rounded-xl px-3 py-1.5 font-label-md text-label-md {draft.timeMode === 'clock'
              ? 'bg-primary text-on-primary'
              : 'border border-outline-variant text-on-surface-variant'}"
            onclick={() => (draft.timeMode = "clock")}
          >
            Às HH:MM
          </button>
        </div>

        {#if draft.timeMode === "relative"}
          <div class="flex gap-2">
            <Field label="Esperar">
              <Input type="number" bind:value={draft.timeSeconds} />
            </Field>
            <Field label="Unidade">
              <Select
                value={draft.timeUnit}
                options={[
                  { value: "minutes", label: "minutos" },
                  { value: "hours", label: "horas" },
                ]}
                onchange={(v) => (draft.timeUnit = v === "hours" ? "hours" : "minutes")}
                cls="w-full px-2 py-1.5"
              />
            </Field>
          </div>
        {:else}
          <Field label="Horário">
            <Input type="time" bind:value={draft.timeAt} />
          </Field>

          <div class="flex flex-col gap-1.5">
            <span class="font-label-sm text-label-sm text-on-surface-variant">
              Repetir nos dias (opcional)
            </span>
            <div class="flex flex-wrap gap-1.5">
              {#each DAY_OPTIONS as day (day.value)}
                {@const active = draft.timeDays.includes(day.value)}
                <button
                  type="button"
                  aria-pressed={active ? "true" : "false"}
                  onclick={() => toggleTimeDay(day.value)}
                  class="press rounded-full px-2.5 py-1 font-label-sm text-label-sm {active
                    ? 'bg-primary text-on-primary'
                    : 'border border-outline-variant/50 bg-surface-container-high/70 text-on-surface-variant'}"
                >
                  {day.label}
                </button>
              {/each}
            </div>
          </div>
        {/if}

        <p class="font-label-sm text-label-sm text-on-surface-variant">
          Usa o horário local do hub.
          {#if draft.timeMode === "clock" && draft.timeDays.length}
            Repete toda semana nos dias marcados, às {draft.timeAt}.
          {:else}
            Sem dias marcados, executa uma vez e some. Se o horário já passou, vale amanhã.
          {/if}
        </p>
      {:else if draft.triggerKind === "manual"}
        <p class="font-label-sm text-label-sm text-on-surface-variant">
          Cena manual: executa sempre que você tocar no botão do Início, sem período.
        </p>
      {:else if draft.triggerDevice}
        {#if selectedContract && selectedContract.events.length === 0}
          <p class="font-label-sm text-label-sm text-on-surface-variant">
            Este dispositivo ainda não expõe funções. Abra-o em Ajustes e atualize as funções, ou use o modo avançado.
          </p>
        {/if}

        <Field label="Função" hint={selectedEvent?.hint ?? ""}>
          <Select
            value={draft.triggerEvent}
            options={[{ value: "", label: "Selecione…" }, ...eventOptions.map((e) => ({ value: e.value, label: e.label }))]}
            onchange={setEvent}
            cls="w-full px-2 py-1.5"
          />
        </Field>

        {#if draft.triggerEvent === "__custom__"}
          <Field
            label={draft.triggerKind === "midea_state" ? "Chave de estado" : "Id do data point"}
            hint="Para aparelhos que ainda não têm specs carregadas."
          >
            <Input
              bind:value={draft.customDP}
              placeholder={draft.triggerKind === "midea_state" ? "Ex.: power" : "Ex.: 1"}
            />
          </Field>
        {/if}

        {#if draft.triggerEvent}
          <Field label="Condição">
            <Select
              value={draft.condition}
              options={conditions}
              onchange={setCondition}
              cls="w-full px-2 py-1.5"
            />
          </Field>

          {#if draft.condition === "value"}
            {#if selectedEvent?.kind === "enum"}
              <Field label="Valor">
                <Select
                  value={draft.value}
                  options={eventValues}
                  onchange={(v) => (draft.value = v)}
                  cls="w-full px-2 py-1.5"
                />
              </Field>
            {:else if selectedEvent?.kind === "number"}
              <Field label="Valor">
                <input
                  type="number"
                  bind:value={draft.value}
                  min={selectedEvent.min}
                  max={selectedEvent.max}
                  step={selectedEvent.step}
                  class="w-full rounded-xl border-outline-variant bg-surface-container-low font-body-md text-body-md text-on-surface"
                />
              </Field>
            {:else}
              <Field label="Valor">
                <Input bind:value={draft.value} placeholder="Valor do evento" />
              </Field>
            {/if}
          {/if}
        {/if}
      {:else}
        <p class="font-label-sm text-label-sm text-on-surface-variant">
          Escolha um dispositivo da lista. Todos os dispositivos aparecem juntos.
        </p>
      {/if}
    </section>

    {#if draft.triggerKind !== "manual" && draft.triggerKind !== "time"}
      <section class="flex flex-col gap-2">
        <div class="flex items-center justify-between">
          <div>
            <h3 class="font-label-md text-label-md font-semibold text-on-surface">Só neste horário</h3>
            <p class="font-label-sm text-label-sm text-on-surface-variant">Vale para o evento automático.</p>
          </div>
          <Toggle on={draft.scheduleEnabled} label="Só neste horário" onclick={toggleSchedule} />
        </div>

        {#if draft.scheduleEnabled}
          <div class="grid grid-cols-2 gap-2">
            <Field label="Início">
              <Input type="time" bind:value={draft.scheduleStart} />
            </Field>
            <Field label="Fim">
              <Input type="time" bind:value={draft.scheduleEnd} />
            </Field>
          </div>

          <div class="flex flex-wrap gap-1.5">
            {#each DAY_OPTIONS as day (day.value)}
              {@const active = draft.scheduleDays.includes(day.value)}
              <button
                type="button"
                aria-pressed={active ? "true" : "false"}
                onclick={() => toggleDay(day.value)}
                class="press rounded-full px-2.5 py-1 font-label-sm text-label-sm {active
                  ? 'bg-primary text-on-primary'
                  : 'border border-outline-variant/50 bg-surface-container-high/70 text-on-surface-variant'}"
              >
                {day.label}
              </button>
            {/each}
          </div>

          <p class="font-label-sm text-label-sm text-on-surface-variant">
            {scheduleLabel({ start: draft.scheduleStart, end: draft.scheduleEnd, days: draft.scheduleDays })}
            {#if draft.scheduleStart === draft.scheduleEnd} · início igual ao fim: vale o dia inteiro{/if}
          </p>
          {#if draft.scheduleStart > draft.scheduleEnd}
            <p class="font-label-sm text-label-sm text-on-surface-variant">
              Atravessa a meia-noite: o dia escolhido é o do início (ex.: sexta 18:00 → sábado 03:00).
            </p>
          {/if}
        {/if}
      </section>
    {:else if draft.triggerKind === "manual"}
      <p class="font-label-sm text-label-sm text-on-surface-variant">
        Cena manual: executa sempre que você tocar no botão do Início, sem período.
      </p>
    {/if}

    <section class="flex flex-col gap-2">
      <div class="flex items-center justify-between">
        <div>
          <h3 class="font-label-md text-label-md font-semibold text-on-surface">Atraso entre operações</h3>
          <p class="font-label-sm text-label-sm text-on-surface-variant">
            Aguarda antes de cada ação. Útil entre frames de ventilador.
          </p>
        </div>
        <Toggle
          on={draft.delayEnabled}
          label="Atraso entre operações"
          onclick={() => (draft.delayEnabled = !draft.delayEnabled)}
        />
      </div>

      {#if draft.delayEnabled}
        <Field label="Atraso (ms)" hint="Entre 1 e 60.000 ms. Ex.: 1500 ms para o RF do ventilador.">
          <input
            type="number"
            min="1"
            max="60000"
            step="100"
            bind:value={draft.delayMs}
            class="w-full rounded-xl border-outline-variant bg-surface-container-low font-body-md text-body-md text-on-surface"
          />
        </Field>
        <p class="font-label-sm text-label-sm text-on-surface-variant">
          O atraso também vale para a primeira ação: é o tempo entre o gatilho e o primeiro comando.
        </p>
      {/if}
    </section>

    <section class="flex flex-col gap-2">
      <div class="flex items-center justify-between">
        <h3 class="font-label-md text-label-md font-semibold text-on-surface">Então</h3>
        <GhostButton label="Adicionar" icon="plus" onclick={addAction} />
      </div>

      {#each draft.actions as action, i (i)}
        <div class="flex flex-col gap-2 rounded-xl border border-outline-variant bg-surface-container-low p-2.5">
          <div class="flex items-center justify-between gap-2">
            <span class="font-label-sm text-label-sm text-on-surface-variant">Ação {i + 1}</span>
            {#if draft.actions.length > 1}
              <GhostButton label="Remover" icon="trash" danger onclick={() => removeAction(i)} />
            {/if}
          </div>

          <Field label="Dispositivo" hint="Todos os dispositivos aparecem na mesma lista.">
            <Select
              value={actionKey(action)}
              options={[{ value: "", label: "Selecione…" }, ...targetsFor(action)]}
              onchange={(v) => setActionTarget(action, v)}
              cls="w-full px-2 py-1.5"
            />
          </Field>

          {#if action.target}
            {#if controlsFor(action).length === 0}
              <p class="font-label-sm text-label-sm text-on-surface-variant">
                Não encontrei controles para este dispositivo. Abra-o em Ajustes e atualize as funções, ou use o
                modo avançado.
              </p>
            {:else}
              <Field label="Função">
                <Select
                  value={action.control}
                  options={[{ value: "", label: "Selecione…" }, ...controlsFor(action).map((c) => ({ value: c.value, label: c.label }))]}
                  onchange={(v) => setActionControl(action, v)}
                  cls="w-full px-2 py-1.5"
                />
              </Field>
            {/if}

            {#if action.control}
              {#if valuesFor(action).length}
                <Field label="Valor">
                  <Select
                    value={action.value}
                    options={valuesFor(action)}
                    onchange={(v) => (action.value = v)}
                    cls="w-full px-2 py-1.5"
                  />
                </Field>
              {:else if action.controlKind === "number"}
                <Field label="Valor">
                  <input
                    type="number"
                    bind:value={action.value}
                    class="w-full rounded-xl border-outline-variant bg-surface-container-low font-body-md text-body-md text-on-surface"
                  />
                </Field>
              {:else}
                <Field label="Valor">
                  <Input bind:value={action.value} placeholder="Valor" />
                </Field>
              {/if}
            {/if}
          {/if}
        </div>
      {/each}
    </section>

    {#if preview}
      <div class="rounded-xl bg-surface-container-low px-3 py-2.5">
        <div class="font-label-sm text-label-sm text-on-surface-variant">Prévia</div>
        <div class="mt-1 font-body-sm text-body-sm text-on-surface">{preview}</div>
      </div>
    {/if}

    {#if draft.extraActions.length}
      <p class="font-label-sm text-label-sm text-on-surface-variant">
        Esta automação tem {draft.extraActions.length} ação(ões) em formato avançado; elas serão preservadas.
      </p>
    {/if}

    {#if error}
      <p class="rounded-xl border border-error/40 bg-error/10 px-3 py-2 font-body-sm text-body-sm text-on-error-container">{error}</p>
    {/if}

    <div>
      {#if !advancedMode}
        <GhostButton label="Avançado (JSON)" icon="edit" onclick={openAdvanced} />
      {:else}
        <div class="flex flex-col gap-2">
          <div class="flex items-center justify-between">
            <span class="font-label-sm text-label-sm text-on-surface-variant">Edição avançada: o formulário acima será ignorado ao salvar.</span>
            <GhostButton label="Voltar ao formulário" onclick={() => (advancedMode = false)} />
          </div>
          <textarea
            bind:value={advancedText}
            spellcheck="false"
            class="min-h-[220px] w-full resize-none rounded-xl border-outline-variant bg-surface-container-lowest font-mono text-xs text-on-surface"
          ></textarea>
        </div>
      {/if}
    </div>
  </div>

  {#snippet footer()}
    <div class="flex gap-2">
      <button
        type="button"
        class="press flex-1 rounded-xl border border-outline-variant py-2 text-xs text-on-surface-variant"
        onclick={() => (open = false)}
      >
        Cancelar
      </button>
      <AsyncButton label="Salvar" variant="primary" busy={busy} onclick={save} cls="flex-1" />
    </div>
  {/snippet}
</Sheet>