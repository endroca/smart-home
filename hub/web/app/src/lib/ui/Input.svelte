<script lang="ts">
  import type { FullAutoFill } from "svelte/elements";

  let {
    // Sem fallback: permite `bind:value` para um campo ainda nao inicializado
    // (ex.: o nome de um comodo recem-criado antes do primeiro $effect).
    value = $bindable(),
    placeholder = "",
    type = "text",
    autocomplete = "off",
    autocapitalize = undefined,
    spellcheck = undefined,
    maxlength = undefined,
    name = undefined,
    disabled = false,
    onenter,
    cls = "",
  }: {
    value?: string;
    placeholder?: string;
    type?: string;
    autocomplete?: FullAutoFill;
    autocapitalize?: "off" | "none" | "on" | "sentences" | "words" | "characters";
    spellcheck?: boolean;
    maxlength?: number;
    name?: string;
    disabled?: boolean;
    onenter?: () => void;
    cls?: string;
  } = $props();
</script>

<input
  bind:value
  {type}
  {placeholder}
  {autocomplete}
  {autocapitalize}
  {spellcheck}
  {maxlength}
  {name}
  {disabled}
  onkeydown={(e) => {
    if (e.key === "Enter" && onenter) {
      e.preventDefault();
      onenter();
    }
  }}
  class="w-full rounded-xl border-outline-variant bg-surface-container-low font-body-md text-body-md text-on-surface disabled:opacity-50 {cls}"
/>
