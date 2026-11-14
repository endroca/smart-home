// Logica do layout generico (fallback): mostra o que se sabe do device, sem
// assumir controles especificos.

import { fmtValue } from "$lib/format";
import type { HubDevice, TuyaSpec } from "$lib/types";
import { dpLabel } from "../tuya/logic";

export interface GenericView extends HubDevice {
  specs?: TuyaSpec[];
  state?: Record<string, unknown>;
  listening?: boolean;
  ip?: string;
  has_key?: boolean;
}

export interface GenericChip {
  label: string;
  value: string;
}

export function genericChips(d: GenericView): GenericChip[] {
  const specs = d.specs ?? [];
  const state = d.state ?? {};
  return Object.keys(state)
    .slice(0, 12)
    .map((dp) => {
      const spec = specs.find((s) => s.dp_id === dp);
      return { label: spec ? dpLabel(spec) : dp, value: fmtValue(state[dp]) };
    });
}

export function genericHasControls(d: GenericView): boolean {
  return (d.specs ?? []).some((s) => s.writable !== false);
}
