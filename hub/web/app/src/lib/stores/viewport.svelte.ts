// Breakpoint de desktop (768px). O Inicio usa duas colunas independentes; o
// layout precisa reagir ao cruzar o breakpoint.

class Viewport {
  desktop = $state(false);
  #mq: MediaQueryList | null = null;

  constructor() {
    if (typeof window !== "undefined" && window.matchMedia) {
      this.#mq = window.matchMedia("(min-width: 768px)");
      this.desktop = this.#mq.matches;
      this.#mq.addEventListener?.("change", () => {
        this.desktop = this.#mq?.matches ?? false;
      });
    }
  }
}

export const viewport = new Viewport();
