// Regras de exibicao compartilhadas pelos cards.

import { hub } from "$lib/stores/data.svelte";
import { roomFilter } from "$lib/stores/nav.svelte";

// kindBadge e a etiqueta curta da familia do device, mostrada sob o nome:
// "Tuya", "Midea" ou "custom" (os devices de firmware proprio, como o
// ventilador do smart_control).
export function kindBadge(kind: string): string {
  switch (kind) {
    case "tuya":
      return "Tuya";
    case "midea":
      return "Midea";
    case "fan":
      return "custom";
    default:
      return kind;
  }
}

// roomChipName so mostra o comodo na aba "Todos": com um comodo filtrado ele ja
// esta implicito.
export function roomChipName(roomId?: string): string {
  if (!roomId || roomFilter.active) return "";
  return hub.rooms.find((r) => r.id === roomId)?.name ?? "";
}
