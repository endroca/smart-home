<script lang="ts">
  // Bloco cinza pulsante para o primeiro carregamento.
  let { cls = "h-4 w-full" }: { cls?: string } = $props();
</script>

<div class="animate-pulse rounded-lg bg-surface-container-high {cls}"></div>
