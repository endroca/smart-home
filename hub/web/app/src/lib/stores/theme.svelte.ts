// Tema (claro / escuro / auto). Le a mesma chave que o boot em index.html.

export type ThemeMode = "auto" | "light" | "dark";

const KEY = "smart_home_theme";
export const THEME_ORDER: ThemeMode[] = ["auto", "light", "dark"];
export const THEME_LABEL: Record<ThemeMode, string> = { auto: "Sistema", light: "Claro", dark: "Escuro" };
export const THEME_ICON: Record<ThemeMode, string> = { auto: "monitor", light: "sun", dark: "moon" };

function readStored(): ThemeMode {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "auto" || v === "light" || v === "dark") return v;
  } catch {
    /* localStorage indisponivel */
  }
  return "auto";
}

class ThemeStore {
  mode = $state<ThemeMode>(readStored());

  #systemDark: MediaQueryList | null =
    typeof window !== "undefined" && window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;

  constructor() {
    if (this.#systemDark?.addEventListener) {
      this.#systemDark.addEventListener("change", () => {
        if (this.mode === "auto") this.apply(false);
      });
    }
  }

  get dark(): boolean {
    if (this.mode === "dark") return true;
    if (this.mode === "light") return false;
    return this.#systemDark?.matches ?? false;
  }

  // apply pinta o tema no <html> e atualiza a cor do chrome do navegador.
  apply(fromUser: boolean) {
    if (typeof document === "undefined") return;
    if (fromUser) {
      try {
        localStorage.setItem(KEY, this.mode);
      } catch {
        /* ignore */
      }
    }
    const dark = this.dark;
    const root = document.documentElement;
    root.classList.toggle("dark", dark);
    root.classList.toggle("light", !dark);
    root.dataset.theme = this.mode;
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", dark ? "#0b1115" : "#f4f7f5");
  }

  cycle() {
    this.mode = THEME_ORDER[(THEME_ORDER.indexOf(this.mode) + 1) % THEME_ORDER.length];
    this.apply(true);
  }

  set(mode: ThemeMode) {
    this.mode = mode;
    this.apply(true);
  }
}

export const theme = new ThemeStore();
