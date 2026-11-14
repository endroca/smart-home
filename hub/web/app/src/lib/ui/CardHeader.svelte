<script lang="ts">
  import type { Snippet } from "svelte";
  import Icon from "./Icon.svelte";
  import RoomChip from "./RoomChip.svelte";
  import { accentOf } from "./accent";

  let {
    name,
    icon,
    accent = "primary",
    online = false,
    roomName = "",
    spin = false,
    spinCls = "",
    badges,
    trailing,
  }: {
    name: string;
    icon: string;
    accent?: string;
    online?: boolean;
    roomName?: string;
    spin?: boolean;
    spinCls?: string;
    badges?: Snippet;
    trailing?: Snippet;
  } = $props();

  const a = $derived(accentOf(accent));
</script>

<div class="relative flex items-start justify-between gap-2">
  <div class="flex min-w-0 items-center gap-space-md">
    <div class="icon-badge relative flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl shadow-inner sm:h-14 sm:w-14">
      {#if spin}
        <span class="fan-rotor {spinCls} {a.text}"><Icon name={icon} cls="h-7 w-7 sm:h-8 sm:w-8" /></span>
      {:else}
        <Icon name={icon} cls="h-7 w-7 sm:h-8 sm:w-8 {a.text}" />
      {/if}
      <span class="absolute right-1 top-1">
        <span class="h-2.5 w-2.5 rounded-full {online ? 'bg-primary glow-dot' : 'bg-outline-variant'}"></span>
      </span>
    </div>
    <div class="min-w-0">
      <h3 class="break-words font-headline-sm text-headline-sm font-bold leading-tight tracking-tight text-on-surface">
        {name}
      </h3>
      {#if badges || roomName}
        <div class="mt-1 flex flex-wrap items-center gap-1.5">
          {#if badges}{@render badges()}{/if}
          {#if roomName}<RoomChip name={roomName} />{/if}
        </div>
      {/if}
    </div>
  </div>
  {#if trailing}
    <div class="flex shrink-0 items-center gap-2">{@render trailing()}</div>
  {/if}
</div>
