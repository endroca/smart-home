// Reconciliacao de estado: alinha o que esta no store (reativo) com o que veio
// do hub gravando APENAS o que mudou.
//
// Por que existe: o `load()` reenvia o snapshot inteiro a cada 30 s e o SSE
// reenvia o estado de um device a cada poucos segundos. Trocar os objetos por
// versoes novas (mesmo com o mesmo conteudo) invalida a arvore de componentes
// toda — inclusive um input de OUTRO device que o operador esta editando e que
// nao tem nada a ver com a atualizacao. Aqui a identidade dos objetos e
// preservada e so o campo que mudou e gravado.

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** deepEqual compara valores JSON (objetos, arrays e escalares). */
export function deepEqual(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((x, i) => deepEqual(x, b[i]));
  }
  if (isPlainObject(a) && isPlainObject(b)) {
    const ka = Object.keys(a);
    const kb = Object.keys(b);
    return ka.length === kb.length && ka.every((k) => deepEqual(a[k], b[k]));
  }
  return false;
}

export interface ReconcileOptions {
  /**
   * Remove as chaves que nao vierem em `next`. Use quando o payload e COMPLETO
   * (o snapshot do `load()`); um evento incremental (um DP que mudou) nao pode
   * apagar os outros campos.
   */
  prune?: boolean;
}

/**
 * reconcileObject grava em `target` so o que difere de `next`, recursando em
 * objetos simples (`state`, `reported`, ...) para preservar a granularidade:
 * mudar um DP notifica apenas quem le aquele DP. Devolve true se algo mudou.
 */
export function reconcileObject(target: object, next: object, opts: ReconcileOptions = {}): boolean {
  const t = target as Record<string, unknown>;
  const n = next as Record<string, unknown>;
  let changed = false;
  for (const key of Object.keys(n)) {
    const a = t[key];
    const b = n[key];
    if (isPlainObject(a) && isPlainObject(b)) {
      if (reconcileObject(a, b, opts)) changed = true;
      continue;
    }
    if (!deepEqual(a, b)) {
      t[key] = b;
      changed = true;
    }
  }
  if (opts.prune) {
    for (const key of Object.keys(t)) {
      if (!(key in n)) {
        delete t[key];
        changed = true;
      }
    }
  }
  return changed;
}

/**
 * reconcileList alinha `target` com `next` pela chave `id`, reaproveitando os
 * objetos que ja existem. O array so e reescrito quando algo mudou — assim um
 * snapshot identico nao invalida nenhum componente.
 */
export function reconcileList<T extends object>(
  target: T[],
  next: T[],
  opts: ReconcileOptions & { idKey?: string } = {},
): boolean {
  const idKey = opts.idKey ?? "id";
  const idOf = (v: T) => String((v as Record<string, unknown>)[idKey]);
  const byId = new Map<string, T>();
  for (const t of target) byId.set(idOf(t), t);

  let changed = target.length !== next.length;
  const merged: T[] = [];
  for (let i = 0; i < next.length; i++) {
    const n = next[i];
    const cur = byId.get(idOf(n));
    if (!cur) {
      merged.push(n);
      changed = true;
      continue;
    }
    if (reconcileObject(cur, n, opts)) changed = true;
    if (target[i] !== cur) changed = true;
    merged.push(cur);
  }

  if (changed) {
    target.length = 0;
    for (const item of merged) target.push(item);
  }
  return changed;
}

/** reconcileField grava `next` em `target[key]` apenas quando mudou. */
export function reconcileField<T extends object>(target: T, key: keyof T & string, next: unknown): boolean {
  const t = target as Record<string, unknown>;
  if (deepEqual(t[key], next)) return false;
  t[key] = next;
  return true;
}
