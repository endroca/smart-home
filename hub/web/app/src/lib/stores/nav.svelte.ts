// Navegacao (paginas + detalhe de device) e o filtro de comodo ativo.

export type Page = "home" | "rooms" | "automations" | "settings";

export const PAGE_LABELS: Record<Page, string> = {
  home: "Início",
  rooms: "Cômodos",
  automations: "Automações",
  settings: "Ajustes",
};

const ROOM_KEY = "smart_home_room";

export type Detail =
  | { type: "device"; id: string }
  | { type: "gateway"; id: string };

class NavStore {
  page = $state<Page>("home");
  /** quando definido, o detalhe (device ou gateway) cobre a pagina */
  detail = $state<Detail | null>(null);

  go(page: Page) {
    this.page = page;
    this.detail = null;
    this.#top();
  }

  openDevice(id: string) {
    this.detail = { type: "device", id };
    this.#top();
  }

  openGateway(id: string) {
    this.detail = { type: "gateway", id };
    this.#top();
  }

  closeDetail() {
    this.detail = null;
  }

  #top() {
    if (typeof window !== "undefined") window.scrollTo({ top: 0 });
  }
}

export const nav = new NavStore();

class RoomFilter {
  active = $state<string>("");

  constructor() {
    try {
      this.active = localStorage.getItem(ROOM_KEY) ?? "";
    } catch {
      this.active = "";
    }
  }

  set(id: string) {
    this.active = id;
    try {
      if (id) localStorage.setItem(ROOM_KEY, id);
      else localStorage.removeItem(ROOM_KEY);
    } catch {
      /* ignore */
    }
  }
}

export const roomFilter = new RoomFilter();
