<script lang="ts">
  let { text, cls = "border-outline-variant text-on-surface-variant" }: { text: string; cls?: string } = $props();
</script>

<span class="rounded-full border px-2.5 py-0.5 font-label-sm text-label-sm {cls}">{text}</span>
