// Registry de layouts de device. O nucleo e device-agnostico: cada modulo
// (fan, tuya, socket, switch, midea, generic) registra o seu layout e o nucleo
// so resolve qual usar. Ver README do hub ("Layouts").
//
// A logica de cada device vive em `logic.ts` (puro, testavel sem DOM); o
// componente so apresenta.

import type { Component } from "svelte";
import type { HubDevice } from "$lib/types";

export interface DeviceModule {
  /** id estavel do layout (persistido em devices.template) */
  id: string;
  label: string;
  /** kinds que o layout atende (fan, tuya, midea, ...) */
  kinds: string[];
  hint: string;
  /** decide a inferencia; o primeiro que aceitar vence */
  match: (device: HubDevice) => boolean;
  /** card do Inicio */
  component: Component<any>;
  /** conteudo do detalhe/configuracao do device */
  config: Component<any>;
}

const MODULES: DeviceModule[] = [];

export function registerDevice(mod: DeviceModule): void {
  MODULES.push(mod);
}

export function deviceById(id: string): DeviceModule | undefined {
  return MODULES.find((m) => m.id === id);
}

// listTemplates lista os layouts registrados (id/label/hint), por kind.
export function listTemplates(kind?: string): { id: string; label: string; hint: string }[] {
  return MODULES.filter((m) => !kind || m.kinds.includes(kind)).map((m) => ({ id: m.id, label: m.label, hint: m.hint }));
}

// resolveTemplate escolhe um layout: a escolha explicita vence, senao o
// primeiro cujo match() aceita o device, senao o generic.
export function resolveTemplate(device: HubDevice): DeviceModule {
  if (device?.template) {
    const explicit = deviceById(device.template);
    if (explicit) return explicit;
  }
  const matched = MODULES.find((m) => m.match?.(device));
  return matched ?? deviceById("generic") ?? MODULES[MODULES.length - 1];
}

export function allModules(): DeviceModule[] {
  return MODULES;
}
