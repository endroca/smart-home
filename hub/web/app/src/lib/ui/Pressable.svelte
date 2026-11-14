<script lang="ts">
  import type { Snippet } from "svelte";

  // Botao com toque/longa-pressao (o padrao do ventilador: toque envia RF,
  // segurar ~550 ms corrige o estado conhecido sem RF). `onHold` e opcional.
  //
  // Regras (as mesmas do front antigo):
  // - o timer do hold NAO e cancelado se o dedo sair do controle: uma vez
  //   disparado, o hold acontece. So `pointercancel` (scroll/gesto do sistema)
  //   cancela.
  // - no `pointerup`, se o dedo saiu do controle (arrastar para fora) o toque
  //   simples e ignorado; se ficou dentro, dispara.
  let {
    onTap,
    onHold,
    cls = "",
    ariaLabel = "",
    ariaPressed = undefined,
    dataTone = undefined,
    dataActive = undefined,
    dataArmed = undefined,
    children,
  }: {
    onTap?: () => void;
    onHold?: () => void;
    cls?: string;
    ariaLabel?: string;
    ariaPressed?: boolean;
    dataTone?: string;
    dataActive?: boolean;
    dataArmed?: boolean;
    children: Snippet;
  } = $props();

  const HOLD_MS = 550;

  let el = $state<HTMLElement | undefined>(undefined);
  let timer: ReturnType<typeof setTimeout> | null = null;
  let down = false;
  let held = false;

  function clear() {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
  }

  function start(e: PointerEvent) {
    // Sem `if (down) return`: um press anterior que nunca completou nao pode
    // deixar o controle morto; cada pointerdown re-arma.
    down = true;
    held = false;
    clear();
    if (onHold) {
      timer = setTimeout(() => {
        timer = null;
        held = true;
        try {
          navigator.vibrate?.(15);
        } catch {
          /* ignore */
        }
        onHold?.();
      }, HOLD_MS);
    }
    // Captura o ponteiro para continuar recebendo eventos mesmo se o dedo
    // sair do botao durante o hold.
    try {
      (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
    } catch {
      /* alguns navegadores recusam; o hold continua via timer */
    }
  }

  function end(e: PointerEvent) {
    if (!down) return;
    down = false;
    clear();
    const wasHeld = held;
    held = false;
    if (wasHeld) return;
    // Ignora um release fora do controle (arrastar para fora).
    if (typeof e.clientX === "number" && typeof document !== "undefined" && document.elementFromPoint) {
      const under = document.elementFromPoint(e.clientX, e.clientY);
      if (under && el && !el.contains(under)) return;
    }
    onTap?.();
  }

  function cancel() {
    down = false;
    held = false;
    clear();
  }
</script>

<button
  bind:this={el}
  type="button"
  class={cls}
  aria-label={ariaLabel || undefined}
  aria-pressed={dataArmed ? undefined : ariaPressed != null ? (ariaPressed ? "true" : "false") : undefined}
  data-tone={dataTone}
  data-active={dataActive != null ? (dataActive ? "true" : "false") : undefined}
  data-armed={dataArmed ? "true" : undefined}
  onpointerdown={start}
  onpointerup={end}
  onpointercancel={cancel}
  oncontextmenu={(e) => e.preventDefault()}
>
  {@render children()}
</button>
