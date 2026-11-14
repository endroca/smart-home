// Acentos dos cards: mantem os nomes de classe literais para o scanner do
// Tailwind ve-los.

export type AccentName = "primary" | "secondary" | "tertiary" | "neutral" | "error";

export interface Accent {
  text: string;
  bg: string;
  border: string;
  soft: string;
}

export const ACCENT: Record<AccentName, Accent> = {
  primary: { text: "text-primary", bg: "bg-primary", border: "border-primary/40", soft: "bg-primary/10" },
  secondary: { text: "text-secondary", bg: "bg-secondary", border: "border-secondary/40", soft: "bg-secondary/10" },
  tertiary: { text: "text-tertiary", bg: "bg-tertiary", border: "border-tertiary/40", soft: "bg-tertiary/10" },
  error: { text: "text-error", bg: "bg-error", border: "border-error/40", soft: "bg-error/10" },
  neutral: {
    text: "text-on-surface-variant",
    bg: "bg-surface-container-highest",
    border: "border-outline-variant",
    soft: "bg-surface-container-high",
  },
};

export function accentOf(name?: string): Accent {
  return ACCENT[(name as AccentName) ?? "primary"] ?? ACCENT.primary;
}
