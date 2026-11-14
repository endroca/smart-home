<script lang="ts">
  import type { HubDevice } from "$lib/types";
  import { resolveTemplate } from "./registry";
  // Garante que todos os layouts estejam registrados (efeito colateral) mesmo
  // quando o card e montado sem passar pelo shell do app.
  import "./index";

  let { device, onOpenConfig }: { device: HubDevice; onOpenConfig?: (id: string) => void } = $props();

  const module = $derived(resolveTemplate(device));
  const Card = $derived(module.component);
</script>

<Card {device} {onOpenConfig} />
