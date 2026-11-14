<script lang="ts">
  // Indicador de carregamento inline (secoes, listas, cards).
  let { label = "Carregando…", cls = "" }: { label?: string; cls?: string } = $props();
</script>

<div class="flex items-center justify-center gap-2 py-4 font-body-sm text-body-sm text-on-surface-variant {cls}">
  <span class="inline-block h-4 w-4 animate-spin rounded-full border-2 border-primary/30 border-t-primary"></span>
  {label}
</div>
