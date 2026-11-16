<script lang="ts">
  // Renderiza um icone do catalogo. Todos sao de CONTORNO (stroke): o padrao do
  // hub e nao misturar glifos preenchidos com vazados.
  import { ICONS } from "./icons";

  let { name, cls = "h-5 w-5" }: { name: string; cls?: string } = $props();
  const path = $derived(ICONS[name] ?? ICONS.dot);
</script>

<svg
  class="shrink-0 {cls}"
  viewBox="0 0 24 24"
  aria-hidden="true"
  fill="none"
  stroke="currentColor"
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
>
  <!-- eslint-disable-next-line svelte/no-at-html-tags -->
  {@html path}
</svg>
