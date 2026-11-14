// Modal de renomear compartilhado (fans, Tuya e Midea). O contexto diz qual
// endpoint salvar; ver $lib/actions/rename.ts.

export type RenameKind = "fan" | "tuya" | "midea";

export interface RenameContext {
  kind: RenameKind;
  /** fan: thing + device; tuya/midea: id */
  thing?: string;
  device?: string;
  id?: string;
}

class RenameStore {
  open = $state(false);
  name = $state("");
  ctx = $state<RenameContext | null>(null);
  busy = $state(false);

  start(ctx: RenameContext, name: string) {
    this.ctx = ctx;
    this.name = name;
    this.open = true;
  }

  close() {
    this.open = false;
    this.ctx = null;
    this.busy = false;
  }
}

export const rename = new RenameStore();
