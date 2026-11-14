// Formatadores compartilhados pelos cards e telas (portados do ui.js antigo).

export function fmtValue(v: unknown): string {
  if (v === true) return "on";
  if (v === false) return "off";
  if (v === null || v === undefined || v === "") return "—";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

// fmtNum escala um valor bruto e formata com precisao fixa.
export function fmtNum(v: unknown, scale = 1, digits = 0): string {
  if (v === null || v === undefined || v === "") return "—";
  const n = Number(v);
  if (!isFinite(n)) return String(v);
  return (n * scale).toFixed(digits);
}

// relTime gera um "ha X" compacto a partir de epoch segundos/millis.
export function relTime(ts: unknown): string {
  if (!ts) return "";
  const n = Number(ts);
  if (!isFinite(n) || n === 0) return "";
  const ms = n > 1e12 ? n : n * 1000;
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000));
  if (s < 60) return "agora";
  if (s < 3600) return Math.round(s / 60) + " min";
  if (s < 86400) return Math.round(s / 3600) + " h";
  return Math.round(s / 86400) + " d";
}

// pct limita um valor a uma porcentagem 0..100 para medidores.
export function pct(v: unknown, min = 0, max = 100): number {
  const n = Number(v);
  if (!isFinite(n)) return 0;
  if (max <= min) return 0;
  return Math.max(0, Math.min(100, ((n - min) / (max - min)) * 100));
}

export function fmtUptime(sec: unknown): string {
  const s = Math.max(0, Math.floor(Number(sec) || 0));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d} d ${h} h`;
  if (h > 0) return `${h} h ${m} min`;
  return `${m} min`;
}

export function fmtBytes(n: unknown): string {
  const b = Number(n);
  if (!isFinite(b) || b <= 0) return "";
  if (b < 1024) return `${b} B`;
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`;
  return `${(b / (1024 * 1024)).toFixed(1)} MB`;
}
