<script lang="ts">
  import { onMount } from "svelte";
  import { hub } from "$lib/stores/data.svelte";
  import { PAGE_LABELS, nav, roomFilter } from "$lib/stores/nav.svelte";
  import { theme, THEME_ICON, THEME_LABEL } from "$lib/stores/theme.svelte";
  import { toast } from "$lib/stores/toast.svelte";
  import { voice } from "$lib/stores/voice.svelte";
  import Icon from "$lib/ui/Icon.svelte";
  import Home from "$lib/pages/Home.svelte";
  import Rooms from "$lib/pages/Rooms.svelte";
  import Automations from "$lib/pages/Automations.svelte";
  import Settings from "$lib/pages/Settings.svelte";
  import DeviceDetail from "$lib/pages/DeviceDetail.svelte";
  import VoiceSheet from "$lib/components/VoiceSheet.svelte";
  import RenameSheet from "$lib/components/RenameSheet.svelte";
  import { allDevices } from "$lib/devices/lists";
  // Registra todos os layouts de device (efeito colateral).
  import "$lib/devices/index";

  const NAV = [
    { page: "home", label: "Início", icon: "home" },
    { page: "rooms", label: "Cômodos", icon: "grid" },
    { page: "automations", label: "Automações", icon: "sparkle" },
    { page: "settings", label: "Ajustes", icon: "settings" },
  ] as const;

  const crumb = $derived.by(() => {
    if (nav.detail) {
      if (nav.detail.type === "gateway") return nav.detail.id;
      const d = allDevices().find((x) => x.id === nav.detail?.id);
      return d?.name || "Dispositivo";
    }
    if (nav.page === "home" && roomFilter.active) {
      return hub.rooms.find((r) => r.id === roomFilter.active)?.name ?? "Início";
    }
    return PAGE_LABELS[nav.page];
  });

  onMount(() => {
    theme.apply(false);
    void hub.load();
    hub.connectEvents();
    hub.startPolling();
    // Resolve o modo de voz (Whisper no hub × Web Speech × texto) antes do
    // primeiro toque, para o getUserMedia acontecer dentro do gesto do usuario.
    void voice.prepare();
    return () => hub.stop();
  });

  // Carrega as specs dos devices Tuya que ainda nao tem (uma vez por device).
  $effect(() => {
    if (hub.tuya.available && hub.tuya.devices.some((d) => !hub.specs[d.id])) void hub.loadSpecsForDevices();
  });
</script>

<header class="app-header pt-safe">
  <div class="mx-auto w-full max-w-lg px-space-md pb-2 pt-2 md:max-w-3xl lg:max-w-5xl">
    <div class="flex items-center justify-between gap-3">
      <div class="flex min-w-0 items-center gap-3">
        <div class="logo-badge">
          <Icon name="home" cls="h-6 w-6" />
        </div>
        <div class="min-w-0">
          <h1 class="brand-title">smart <span class="text-primary">home</span></h1>
          <div class="brand-sub"><span>{crumb}</span></div>
        </div>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <button
          id="voice-btn"
          type="button"
          aria-label="Comando de voz"
          title={voice.transcribing ? "Transcrevendo…" : voice.listening ? "Ouvindo…" : "Comando de voz"}
          class="icon-btn press {voice.transcribing ? 'opacity-60' : ''}"
          style={voice.listening ? "color: rgb(var(--sh-primary))" : ""}
          onclick={() => void voice.toggle()}
        >
          {#if voice.transcribing}
            <span class="inline-block h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"></span>
          {:else}
            <Icon name="mic" cls="h-5 w-5" />
          {/if}
        </button>
        <button
          id="refresh"
          type="button"
          aria-label="Atualizar"
          class="icon-btn press {hub.loading ? 'opacity-60' : ''}"
          onclick={() => hub.load(true)}
        >
          {#if hub.loading}
            <span class="inline-block h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"></span>
          {:else}
            <Icon name="refresh" cls="h-5 w-5" />
          {/if}
        </button>
        <button
          id="settings-btn"
          type="button"
          aria-label="Configurações"
          class="icon-btn press"
          onclick={() => nav.go("settings")}
        >
          <Icon name="settings" cls="h-5 w-5" />
        </button>
        <button
          id="theme-btn"
          type="button"
          aria-label="Tema: {THEME_LABEL[theme.mode]}"
          title="Tema: {THEME_LABEL[theme.mode]}"
          class="theme-btn press"
          onclick={() => theme.cycle()}
        >
          <Icon name={THEME_ICON[theme.mode]} cls="h-5 w-5" />
        </button>
      </div>
    </div>
  </div>
</header>

<main class="mx-auto w-full max-w-lg px-space-md pb-nav md:max-w-3xl lg:max-w-5xl">
  {#if nav.detail}
    <section class="flex flex-col gap-space-lg pt-3">
      <DeviceDetail />
    </section>
  {:else if nav.page === "home"}
    <section class="flex flex-col gap-space-lg">
      <Home />
    </section>
  {:else if nav.page === "rooms"}
    <section class="flex flex-col gap-space-lg pt-3">
      <Rooms />
    </section>
  {:else if nav.page === "automations"}
    <section class="flex flex-col gap-space-lg pt-3">
      <Automations />
    </section>
  {:else}
    <section class="flex flex-col gap-space-lg pt-3">
      <Settings />
    </section>
  {/if}
</main>

<nav class="bottom-nav" aria-label="Navegação principal">
  <div class="bottom-nav-inner">
    {#each NAV as item (item.page)}
      {@const active = !nav.detail && nav.page === item.page}
      <button
        type="button"
        class="nav-item press {active ? 'text-primary font-semibold' : 'text-on-surface-variant'}"
        onclick={() => nav.go(item.page)}
      >
        <Icon name={item.icon} cls="h-[22px] w-[22px]" />
        <span class="font-label-sm text-label-sm tracking-tight">{item.label}</span>
      </button>
    {/each}
  </div>
</nav>

{#if toast.visible}
  <div
    class="fixed bottom-28 left-1/2 z-50 flex -translate-x-1/2 items-center gap-2 rounded-full border px-4 py-2 font-label-md text-label-md shadow-2xl backdrop-blur {toast.error
      ? 'border-error/50 bg-surface-container/95 text-on-error-container'
      : 'border-primary/30 bg-surface-container/95 text-primary'}"
  >
    {toast.message}
  </div>
{/if}

<VoiceSheet />
<RenameSheet />
