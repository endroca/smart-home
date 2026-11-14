// Catalogo de acoes do "personalize a acao" do temporizador. Reaproveita o
// catalogo do editor de automacoes (um interruptor de 3 botoes expoe as tres
// chaves, um ventilador expoe velocidade/luz/direcao) e traduz a escolha para
// a acao que o hub executa. Sem Svelte e sem store: testavel sem DOM.

import type { FanView } from "$lib/devices/fan/logic";
import type { MideaView } from "$lib/devices/midea/logic";
import {
  actionFromDraft,
  actionValueChoices,
  fanActionOptions,
  mideaControlOptions,
  tuyaControlOptions,
  type ActionDraft,
  type Choice,
  type ControlKind,
  type ControlOption,
} from "$lib/automations/logic";
import { clockTimer, fmtDuration, relativeTimer, type TimerTarget } from "$lib/automations/timer";
import type { Automation, HubDevice, TuyaSpec } from "$lib/types";

/** timerControls lista o que o device aceita fazer, no formato do editor. */
export function timerControls(
  device: HubDevice | undefined,
  specs: TuyaSpec[] = [],
  fan?: FanView,
  midea?: MideaView,
): ControlOption[] {
  if (!device) return [];
  switch (device.kind) {
    case "fan":
      return fanActionOptions(fan);
    case "tuya":
      return tuyaControlOptions(specs);
    case "midea":
      return mideaControlOptions(midea);
    default:
      return [];
  }
}

/** effectiveKind normaliza enum numerico (velocidade, angulo) para number. */
export function effectiveKind(control: ControlOption): ControlKind {
  return control.numeric ? "number" : control.kind;
}

/** timerValues lista os valores possiveis do controle. */
export function timerValues(control?: ControlOption): Choice[] {
  return actionValueChoices(control);
}

/** timerDefaultValue e o valor inicial ao escolher um controle. */
export function timerDefaultValue(control?: ControlOption): string {
  if (!control) return "";
  if (control.defaultValue) return control.defaultValue;
  if (control.kind === "bool") return control.values?.[0]?.value ?? "true";
  if (control.kind === "enum") return control.values?.[0]?.value ?? "";
  if (control.kind === "number") return String(control.min ?? 0);
  return "";
}

/** timerTarget traduz controle+valor para a acao executavel do hub. */
export function timerTarget(
  device: HubDevice | undefined,
  control: ControlOption | undefined,
  value: string,
): TimerTarget | null {
  if (!device || !control) return null;
  let draft: ActionDraft;
  switch (device.kind) {
    case "fan":
      draft = { kind: "fan", target: device.id, control: control.value, controlKind: effectiveKind(control), value };
      break;
    case "tuya":
      draft = { kind: "tuya", target: device.id, control: control.value, controlKind: effectiveKind(control), value };
      break;
    case "midea":
      draft = { kind: "midea", target: device.id, control: control.value, controlKind: effectiveKind(control), value };
      break;
    default:
      return null;
  }
  return {
    driver: device.kind,
    id: device.id,
    label: device.name || device.id,
    action: actionFromDraft(draft),
  };
}

/** timerActionText descreve o que a acao faz ("Desligar Chave 2", "Velocidade Forte"). */
export function timerActionText(control?: ControlOption, value = ""): string {
  if (!control) return "Ação";
  if (control.kind === "bool") {
    const verb = value === "false" ? "Desligar" : "Ligar";
    if (verb === "Desligar" && /desligar/i.test(control.label)) return control.label;
    return `${verb} ${control.label}`;
  }
  if (!value) return control.label;
  const hit = control.values?.find((v) => v.value === value);
  return hit ? `${control.label} ${hit.label}` : `${control.label} ${value}`;
}

/**
 * customTimer monta a volátil de uma ação escolhida no "personalize".
 * `when` aceita um atraso relativo (presets) ou um horário de relógio.
 */
export function customTimer(
  device: HubDevice | undefined,
  control: ControlOption | undefined,
  value: string,
  when: { seconds: number } | { at: string },
): Automation | null {
  const target = timerTarget(device, control, value);
  if (!target) return null;
  const text = timerActionText(control, value);
  if ("seconds" in when) {
    const name = `${target.label}: ${text} daqui a ${fmtDuration(when.seconds)}`;
    return relativeTimer(target, when.seconds, "", name);
  }
  return clockTimer(target, when.at, "", `${target.label}: ${text} às ${when.at}`);
}