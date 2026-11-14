<script lang="ts">
  let {
    value = $bindable(""),
    options,
    onchange,
    disabled = false,
    cls = "",
  }: {
    value?: string;
    options: { value: string; label: string }[];
    onchange?: (value: string) => void;
    disabled?: boolean;
    cls?: string;
  } = $props();
</script>

<select
  bind:value
  {disabled}
  onchange={(e) => onchange?.((e.currentTarget as HTMLSelectElement).value)}
  class="rounded-xl border-outline-variant bg-surface-container-low font-label-sm text-label-sm text-on-surface-variant disabled:opacity-50 {cls}"
>
  {#each options as opt (opt.value)}
    <option value={opt.value}>{opt.label}</option>
  {/each}
</select>
