#pragma once

const char INDEX_HTML[] PROGMEM = R"rawliteral(<!DOCTYPE html>
<html lang="pt-BR" class="dark">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, viewport-fit=cover">
<title>Smart Control</title>
<link rel="stylesheet" href="/style.css">
<style>
@keyframes spin-fan { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }
.animate-spin-fan { animation: spin-fan linear infinite; }
.fan-speed-0 { animation-play-state: paused !important; }
.fan-speed-1 { animation-duration: 2.8s; }
.fan-speed-2 { animation-duration: 2.1s; }
.fan-speed-3 { animation-duration: 1.5s; }
.fan-speed-4 { animation-duration: 1.0s; }
.fan-speed-5 { animation-duration: 0.65s; }
.fan-speed-6 { animation-duration: 0.4s; }
.reverse-spin { animation-direction: reverse !important; }
.no-scrollbar::-webkit-scrollbar { display: none; }
.no-scrollbar { -ms-overflow-style: none; scrollbar-width: none; }
.press-effect:active { transform: scale(0.97); }
.hidden { display: none !important; }
.toast-err { border-color: rgba(239,68,68,.55) !important; color: #fecaca !important; }
.modal-scroll { max-height: calc(100dvh - 2rem); overflow-y: auto; }
.learn-row { display:flex; align-items:center; justify-content:space-between; gap:8px; padding:8px 10px; border:1px solid #2E323A; border-radius:12px; background:#17191D; }
.learn-row button { padding:6px 10px; border-radius:8px; background:#10B981; color:#0B1210; font-size:11px; font-weight:700; }
.settings-row { display:flex; align-items:center; justify-content:space-between; gap:8px; padding:8px 10px; border:1px solid #2E323A; border-radius:12px; background:#17191D; }
.settings-row .name { font-size:12px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.settings-row button { padding:5px 8px; border-radius:8px; border:1px solid #2E323A; font-size:11px; }
.settings-row button.danger { color:#fecaca; border-color:rgba(239,68,68,.4); }
</style>
</head>
<body class="bg-[#121316] text-[#E3E2E6] font-sans min-h-screen antialiased flex justify-center selection:bg-emerald-500 selection:text-white">
<div class="w-full max-w-md min-h-screen flex flex-col justify-between p-4 sm:p-5 relative pb-8 overflow-x-hidden">

<header class="pt-2 pb-3">
  <div class="flex items-center justify-between mb-4">
    <div class="flex items-center space-x-3 min-w-0">
      <div class="w-10 h-10 rounded-2xl bg-gradient-to-tr from-emerald-600 to-emerald-400 p-[1px] shadow-md shadow-emerald-500/10 flex items-center justify-center">
        <div class="w-full h-full bg-[#1E2025] rounded-2xl flex items-center justify-center">
          <svg class="w-5 h-5 text-emerald-400" fill="currentColor" viewBox="0 0 24 24"><path d="M12 2L3.5 10.5L12 19L20.5 10.5L12 2ZM12 5.83L16.67 10.5L12 15.17L7.33 10.5L12 5.83Z"/></svg>
        </div>
      </div>
      <div class="min-w-0">
        <div class="flex items-center gap-2.5">
          <h1 class="text-base font-semibold tracking-tight text-white">Smart Control</h1>
          <span id="online-badge" class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[11px] font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/25">
            <span id="online-dot" class="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse"></span>
            <span id="online-text">Online</span>
          </span>
        </div>
        <p id="fan-source" class="text-[10px] text-slate-500 truncate mt-0.5">—</p>
      </div>
    </div>
    <button id="settings-btn" onclick="openSettings()" aria-label="Configurações do dispositivo" class="w-9 h-9 rounded-xl bg-[#1E2025] border border-[#2E323A] flex items-center justify-center text-slate-300 hover:text-white hover:bg-[#252830] transition press-effect" type="button">
      <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" stroke-linecap="round" stroke-linejoin="round"/><path d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" stroke-linecap="round" stroke-linejoin="round"/></svg>
    </button>
  </div>
  <nav id="fan-tabs" aria-label="Seletor de ambiente" class="flex space-x-2 overflow-x-auto no-scrollbar py-1"></nav>
</header>

<section id="ap-panel" class="hidden rounded-3xl bg-[#1E2025] border border-[#2E323A] p-5 shadow-xl my-2">
  <h2 class="text-base font-bold text-white">Provisionamento</h2>
  <p class="text-xs text-slate-400 mt-1">Conecte-se à rede <strong id="ap-ssid">—</strong> e informe o Wi-Fi da casa e um token de acesso local.</p>
  <div class="mt-4 space-y-3">
    <div class="flex gap-2">
      <select id="scan-list" onchange="pickNetwork()" class="flex-1 rounded-xl bg-[#121316] border border-[#2E323A] text-xs p-2.5"><option value="">clique em buscar</option></select>
      <button onclick="scan()" class="rounded-xl bg-[#121316] border border-[#2E323A] px-3 text-xs">Buscar</button>
    </div>
    <input id="prov-ssid" placeholder="SSID" autocomplete="off" class="w-full rounded-xl bg-[#121316] border border-[#2E323A] text-sm p-2.5">
    <input id="prov-pass" placeholder="Senha" type="password" autocomplete="new-password" class="w-full rounded-xl bg-[#121316] border border-[#2E323A] text-sm p-2.5">
    <div class="flex gap-2">
      <input id="prov-token" placeholder="Token (mínimo 16 caracteres)" autocomplete="off" class="flex-1 rounded-xl bg-[#121316] border border-[#2E323A] text-sm p-2.5">
      <button onclick="genToken()" class="rounded-xl bg-[#121316] border border-[#2E323A] px-3 text-xs">Gerar</button>
    </div>
    <button onclick="provision()" class="w-full rounded-xl bg-emerald-500 text-slate-950 font-bold text-sm py-3">Salvar e reiniciar</button>
  </div>
</section>

<main id="main-panel" class="space-y-4 my-2 flex-grow">
  <div class="grid grid-cols-2 gap-3">
    <button id="master-off-btn" aria-label="Desligar ventilador e luz com segurança" class="press-effect relative overflow-hidden rounded-2xl bg-[#1E2025] border border-red-950/70 p-4 text-left transition duration-200 hover:border-red-900/80 group" type="button">
      <div class="flex items-center justify-between mb-2">
        <span class="w-8 h-8 rounded-xl bg-red-950/40 border border-red-900/40 text-red-400 flex items-center justify-center group-hover:bg-red-950/60 transition">
          <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2.2" viewBox="0 0 24 24"><path d="M18.364 5.636a9 9 0 010 12.728m0 0l-2.829-2.828m2.829 2.828L21 21M15.536 8.464a5 5 0 010 7.072m0 0l-2.829-2.828m-4.243 4.243a9 9 0 01-6.364-2.636 8.964 8.964 0 010-12.728M8.464 15.536a5 5 0 01-1.414-3.536 4.98 4.98 0 011.414-3.536M12 3v6" stroke-linecap="round" stroke-linejoin="round"/></svg>
        </span>
        <span class="text-[10px] font-mono tracking-wider uppercase text-red-400/80 bg-red-950/50 px-1.5 py-0.5 rounded border border-red-900/40">Parada</span>
      </div>
      <div class="font-semibold text-sm text-[#E3E2E6] group-hover:text-red-300">Desligar Tudo</div>
      <div class="text-[11px] text-slate-400 mt-0.5">Zera motor &amp; luz</div>
    </button>

    <button id="light-toggle-btn" aria-label="Alternar luz" class="press-effect relative overflow-hidden rounded-2xl bg-[#1E2025] border border-amber-500/35 p-4 text-left transition duration-200 shadow-[0_0_20px_rgba(245,158,11,0.06)] group" type="button">
      <div class="flex items-center justify-between mb-2">
        <span id="light-icon-box" class="w-8 h-8 rounded-xl bg-amber-500/15 border border-amber-500/30 text-amber-300 flex items-center justify-center transition">
          <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M12 2C8.13 2 5 5.13 5 9C5 11.38 6.19 13.47 8 14.74V17C8 17.55 8.45 18 9 18H15C15.55 18 16 17.55 16 17V14.74C17.81 13.47 19 11.38 19 9C19 5.13 15.87 2 12 2ZM9 21C9 21.55 9.45 22 10 22H14C14.55 22 15 21.55 15 21V20H9V21Z"/></svg>
        </span>
        <span id="light-status-badge" class="text-[10px] font-bold tracking-wide uppercase text-amber-400 bg-amber-500/15 border border-amber-400/30 px-2 py-0.5 rounded-full">Ativa</span>
      </div>
      <div id="light-title" class="font-semibold text-sm text-amber-300">Luz ligada</div>
      <div id="light-subtext" class="text-[11px] text-slate-400 mt-0.5">Toque para desligar</div>
    </button>
  </div>

  <section class="rounded-3xl bg-[#1E2025] border border-[#2E323A] p-5 shadow-xl relative overflow-hidden">
    <div class="absolute -top-12 -right-12 w-48 h-48 bg-emerald-500/10 rounded-full blur-3xl pointer-events-none"></div>
    <div class="flex items-center justify-between mb-3">
      <div>
        <span class="text-xs uppercase tracking-wider text-slate-400 font-semibold">Ventilação</span>
        <h2 class="text-lg font-bold text-white tracking-tight flex items-center gap-1.5">
          <span>Velocidade</span>
          <span class="text-emerald-400 font-mono font-semibold" id="speed-label-hero">0</span>
          <span class="text-xs font-normal text-slate-400 font-sans">de 6</span>
        </h2>
      </div>
      <div class="text-right">
        <span id="flow-mode-badge" class="inline-flex items-center gap-1 text-[11px] font-medium px-2.5 py-1 rounded-full bg-emerald-950/60 border border-emerald-500/30 text-emerald-300">
          <svg class="w-3 h-3 text-emerald-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M19 14l-7 7m0 0l-7-7m7 7V3" stroke-linecap="round" stroke-linejoin="round"/></svg>
          Ventilar
        </span>
      </div>
    </div>

    <div class="flex justify-center my-3 relative items-center">
      <div class="relative w-36 h-36 flex items-center justify-center">
        <div class="absolute inset-0 rounded-full border border-emerald-500/20 border-dashed animate-pulse"></div>
        <div class="absolute inset-3 rounded-full border border-[#2E323A]"></div>
        <div id="fan-rotor" class="w-24 h-24 text-emerald-400 animate-spin-fan fan-speed-0 flex items-center justify-center">
          <svg class="w-full h-full" fill="currentColor" viewBox="0 0 24 24"><path d="M12 10.5C11.17 10.5 10.5 11.17 10.5 12C10.5 12.83 11.17 13.5 12 13.5C12.83 13.5 13.5 12.83 13.5 12C13.5 11.17 12.83 10.5 12 10.5Z"/><path d="M12 2C9.5 2 7.7 3.8 8.1 6.5C8.3 7.8 9 9 10 9.8C10.6 8.7 11.8 8 13.1 8C13.6 8 14 8.1 14.5 8.3C14.1 5.3 13.8 2 12 2Z"/><path d="M2.5 15.6C4.4 17.2 7 17.5 9.2 15.9C10.3 15.2 11.1 14.1 11.4 12.9C10.2 12.7 9.1 11.9 8.6 10.7C8.2 11 7.9 11.3 7.5 11.7C5.1 13.6 2.5 15.6 2.5 15.6Z"/><path d="M18.8 8.8C16.8 7.3 14.3 7.5 12.7 9.4C12 10.4 11.8 11.7 12 13C13.2 13.1 14.2 13.9 14.7 15.1C15.1 14.7 15.5 14.4 16 13.9C18.2 11.7 20.8 10.2 18.8 8.8Z"/></svg>
        </div>
        <div class="absolute w-5 h-5 rounded-full bg-[#121316] border-2 border-emerald-400 shadow-md"></div>
      </div>
    </div>

    <div class="mt-4">
      <div class="flex items-center justify-between text-xs text-slate-400 mb-2 px-1"><span>Parado (0)</span><span>Intensidade Máxima (6)</span></div>
      <div class="grid grid-cols-7 gap-1.5 bg-[#121316] p-1.5 rounded-2xl border border-[#2E323A]" id="speed-buttons-container">
        <button class="speed-btn py-2.5 rounded-xl text-xs font-semibold text-slate-400 hover:text-white transition flex flex-col items-center justify-center" data-speed="0" type="button">0</button>
        <button class="speed-btn py-2.5 rounded-xl text-xs font-bold bg-emerald-500 text-slate-950 shadow-md shadow-emerald-500/25 border border-emerald-400/50 transition flex flex-col items-center justify-center" data-speed="1" type="button">1</button>
        <button class="speed-btn py-2.5 rounded-xl text-xs font-semibold text-slate-400 hover:text-white transition flex flex-col items-center justify-center" data-speed="2" type="button">2</button>
        <button class="speed-btn py-2.5 rounded-xl text-xs font-semibold text-slate-400 hover:text-white transition flex flex-col items-center justify-center" data-speed="3" type="button">3</button>
        <button class="speed-btn py-2.5 rounded-xl text-xs font-semibold text-slate-400 hover:text-white transition flex flex-col items-center justify-center" data-speed="4" type="button">4</button>
        <button class="speed-btn py-2.5 rounded-xl text-xs font-semibold text-slate-400 hover:text-white transition flex flex-col items-center justify-center" data-speed="5" type="button">5</button>
        <button class="speed-btn py-2.5 rounded-xl text-xs font-semibold text-slate-400 hover:text-white transition flex flex-col items-center justify-center" data-speed="6" type="button">6</button>
      </div>
    </div>

    <div class="grid grid-cols-2 gap-2 mt-4 pt-3 border-t border-[#2E323A]">
      <button id="btn-mode-ventilar" class="press-effect py-2.5 px-3 rounded-xl text-xs font-semibold flex items-center justify-center gap-2 bg-emerald-950/60 border border-emerald-500/40 text-emerald-300 transition shadow-sm" type="button">
        <svg class="w-3.5 h-3.5 text-emerald-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M19 14l-7 7m0 0l-7-7m7 7V3" stroke-linecap="round" stroke-linejoin="round"/></svg>
        Ventilar (Abaixo)
      </button>
      <button id="btn-mode-exaustar" class="press-effect py-2.5 px-3 rounded-xl text-xs font-medium flex items-center justify-center gap-2 bg-[#121316] border border-[#2E323A] text-slate-400 hover:text-slate-200 transition" type="button">
        <svg class="w-3.5 h-3.5 text-slate-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M5 10l7-7m0 0l7 7m-7-7v18" stroke-linecap="round" stroke-linejoin="round"/></svg>
        Exaustar (Acima)
      </button>
    </div>
  </section>

  <button id="btn-learn-rf" class="press-effect w-full flex items-center justify-center space-x-2 py-3 px-3 rounded-2xl bg-[#1E2025] border border-[#2E323A] hover:border-slate-500 text-xs font-medium text-slate-300 transition" type="button">
    <svg class="w-4 h-4 text-emerald-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M13 10V3L4 14h7v7l9-11h-7z" stroke-linecap="round" stroke-linejoin="round"/></svg>
    <span>Aprender controle</span>
  </button>

  <p class="text-center text-[10px] text-slate-500">Toque = envia RF • Segure = atualiza o estado sem RF</p>

  <div class="rounded-2xl bg-[#1E2025] border border-[#2E323A] overflow-hidden transition-all duration-300">
    <button id="diagnostics-toggle-btn" class="w-full py-2.5 px-4 flex items-center justify-between text-xs text-slate-400 hover:text-slate-200 transition text-left" type="button">
      <span class="flex items-center gap-2">
        <svg id="diagnostics-arrow" class="w-3.5 h-3.5 transform transition-transform duration-200 text-slate-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M9 5l7 7-7 7" stroke-linecap="round" stroke-linejoin="round"/></svg>
        Diagnóstico do Sistema
      </span>
      <span id="timestamp-clock" class="text-[10px] text-slate-500 font-mono">—</span>
    </button>
    <div id="diagnostics-body" class="hidden px-4 pb-3 pt-1 border-t border-[#2E323A] text-[11px] font-mono text-slate-400 space-y-1.5">
      <div class="flex justify-between"><span>Estado:</span><span id="power-telemetry" class="text-slate-300">—</span></div>
      <div class="flex justify-between"><span>Sinal:</span><span id="diag-rssi" class="text-emerald-400">—</span></div>
      <pre id="diag-content" class="whitespace-pre-wrap break-words text-[10px] leading-relaxed mt-1">—</pre>
    </div>
  </div>
</main>

</div>

<div id="toast-message" class="fixed bottom-5 left-1/2 -translate-x-1/2 opacity-0 pointer-events-none transition-opacity duration-300 bg-[#1E2025]/95 border border-emerald-500/30 text-emerald-200 text-xs px-4 py-2 rounded-full shadow-2xl backdrop-blur-md z-50 flex items-center gap-2 max-w-[90vw]">
  <span class="w-2 h-2 rounded-full bg-emerald-400 shrink-0"></span>
  <span id="toast-text">Comando enviado</span>
</div>

<div id="token-modal" class="hidden fixed inset-0 bg-black/70 backdrop-blur-sm z-[90] grid place-items-center p-4">
  <div class="w-full max-w-sm bg-[#1E2025] border border-[#2E323A] rounded-2xl p-5 shadow-2xl">
    <h2 class="text-base font-semibold text-white">Token do dispositivo</h2>
    <p class="text-xs text-slate-400 mt-1">Informe o token definido no provisionamento. Ele fica salvo apenas neste navegador.</p>
    <input id="token-input" type="password" autocomplete="off" placeholder="Token" class="mt-4 w-full rounded-xl bg-[#121316] border border-[#2E323A] text-sm p-2.5">
    <button onclick="saveToken()" class="mt-3 w-full rounded-xl bg-emerald-500 text-slate-950 font-bold text-sm py-2.5">Entrar</button>
  </div>
</div>

<div id="settings-modal" class="hidden fixed inset-0 bg-black/70 backdrop-blur-sm z-[90] grid place-items-center p-4">
  <div class="modal-scroll w-full max-w-md bg-[#1E2025] border border-[#2E323A] rounded-2xl p-5 shadow-2xl text-sm">
    <div class="flex items-center justify-between">
      <h2 class="text-base font-semibold text-white">Configurações</h2>
      <button onclick="closeSettings()" class="w-8 h-8 rounded-lg border border-[#2E323A] text-slate-400">✕</button>
    </div>
    <p id="device-info" class="text-xs text-slate-500 mt-1">—</p>

    <h3 class="text-[11px] uppercase tracking-wider text-slate-500 mt-5 mb-2">Ventiladores</h3>
    <div id="settings-fans" class="space-y-2"></div>
    <button onclick="addFan()" class="mt-2 w-full rounded-xl border border-[#2E323A] py-2 text-xs text-slate-300">Adicionar ventilador</button>

    <h3 class="text-[11px] uppercase tracking-wider text-slate-500 mt-5 mb-2">Wi-Fi</h3>
    <input id="wifi-ssid" placeholder="SSID" autocomplete="off" class="w-full rounded-xl bg-[#121316] border border-[#2E323A] text-sm p-2.5 mb-2">
    <input id="wifi-pass" placeholder="Senha" type="password" autocomplete="new-password" class="w-full rounded-xl bg-[#121316] border border-[#2E323A] text-sm p-2.5">
    <button onclick="saveWifi()" class="mt-2 w-full rounded-xl border border-[#2E323A] py-2 text-xs text-slate-300">Salvar Wi-Fi e reiniciar</button>

    <h3 class="text-[11px] uppercase tracking-wider text-slate-500 mt-5 mb-2">OTA local</h3>
    <input id="ota-file" type="file" accept=".bin" class="w-full text-xs text-slate-400">
    <div class="h-2 bg-[#121316] rounded-full overflow-hidden mt-2"><div id="ota-progress" class="h-full w-0 bg-emerald-500 transition-all"></div></div>
    <button onclick="uploadOta()" class="mt-2 w-full rounded-xl bg-emerald-500 text-slate-950 font-bold py-2 text-xs">Enviar firmware</button>

    <h3 class="text-[11px] uppercase tracking-wider text-slate-500 mt-5 mb-2">Backup da configuração</h3>
    <div class="flex gap-2">
      <button onclick="exportConfig()" class="flex-1 rounded-xl border border-[#2E323A] py-2 text-xs text-slate-300">Exportar</button>
      <button onclick="document.getElementById('config-file').click()" class="flex-1 rounded-xl border border-[#2E323A] py-2 text-xs text-slate-300">Importar</button>
      <input id="config-file" type="file" accept=".json,application/json" class="hidden" onchange="importConfig(this)">
    </div>

    <h3 class="text-[11px] uppercase tracking-wider text-slate-500 mt-5 mb-2">Token local</h3>
    <div class="flex gap-2">
      <input id="local-token" placeholder="Token" class="flex-1 rounded-xl bg-[#121316] border border-[#2E323A] text-sm p-2.5">
      <button onclick="saveLocalToken()" class="rounded-xl border border-[#2E323A] px-3 text-xs text-slate-300">Salvar</button>
    </div>

    <h3 class="text-[11px] uppercase tracking-wider text-slate-500 mt-5 mb-2">Sistema</h3>
    <button onclick="reboot()" class="w-full rounded-xl border border-red-900/60 text-red-300 py-2 text-xs">Reiniciar dispositivo</button>
    <button onclick="closeSettings()" class="mt-3 w-full rounded-xl bg-[#121316] border border-[#2E323A] py-2 text-xs text-slate-300">Fechar</button>
  </div>
</div>

<div id="learn-modal" class="hidden fixed inset-0 bg-black/70 backdrop-blur-sm z-[90] grid place-items-center p-4">
  <div class="modal-scroll w-full max-w-md bg-[#1E2025] border border-[#2E323A] rounded-2xl p-5 shadow-2xl">
    <h2 class="text-base font-semibold text-white">Aprender controle</h2>
    <p class="text-xs text-slate-400 mt-1">Clique em aprender no comando desejado e pressione o botão correspondente no controle físico. O CC1101 escuta por 15 s.</p>
    <div id="learn-list" class="space-y-2 mt-4"></div>
    <div id="learn-status" class="text-xs text-slate-400 mt-3 min-h-[18px]"></div>
    <div class="flex gap-2 mt-4">
      <button onclick="cancelLearn()" class="flex-1 rounded-xl border border-[#2E323A] py-2.5 text-xs text-slate-300">Cancelar aprendizado</button>
      <button onclick="closeLearn()" class="flex-1 rounded-xl bg-[#121316] border border-[#2E323A] py-2.5 text-xs text-slate-300">Fechar</button>
    </div>
  </div>
</div>

<script>
const TOKEN_KEY='smart_control_token';
const LAST_FAN_KEY='smart_control_last_fan';
const COMMANDS=['power','light','vel_0','vel_1','vel_2','vel_3','vel_4','vel_5','vel_6','exhaust','vent'];
const COMMAND_LABELS={
  power:'Power (desligar)',light:'Luz',vel_0:'Velocidade 0',vel_1:'Velocidade 1',vel_2:'Velocidade 2',
  vel_3:'Velocidade 3',vel_4:'Velocidade 4',vel_5:'Velocidade 5',vel_6:'Velocidade 6',
  exhaust:'Exaustar',vent:'Ventilar'
};
let health=null, stateData=null, configData=null, selected=0, learnTimer=null, learnActive=false;

function token(){return localStorage.getItem(TOKEN_KEY)||''}
function esc(v){return String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function show(id){document.getElementById(id).classList.remove('hidden')}
function hide(id){document.getElementById(id).classList.add('hidden')}
function fmtTime(ts){if(!ts)return '—';try{return new Date(ts*1000).toLocaleString('pt-BR')}catch(e){return String(ts)}}
function toast(msg,err){
  const el=document.getElementById('toast-message'), txt=document.getElementById('toast-text');
  txt.textContent=msg;
  el.classList.remove('opacity-0','pointer-events-none');
  el.classList.toggle('toast-err',!!err);
  clearTimeout(el._t);
  el._t=setTimeout(()=>el.classList.add('opacity-0','pointer-events-none'),3800);
}

async function api(path,opts){
  opts=opts||{};
  const headers=Object.assign({},opts.headers||{});
  if(token())headers['Authorization']='Bearer '+token();
  if(opts.body && typeof opts.body==='string')headers['Content-Type']='application/json';
  const res=await fetch(path,Object.assign({},opts,{headers}));
  if(res.status===401){show('token-modal');throw new Error('token inválido')}
  let data={};try{data=await res.json()}catch(e){}
  if(!res.ok||data.ok===false){
    const e=data.error||{};
    throw new Error(e.message||e.code||('HTTP '+res.status));
  }
  return data;
}

function setOnline(on){
  const badge=document.getElementById('online-badge'), dot=document.getElementById('online-dot'), text=document.getElementById('online-text');
  if(on){
    badge.className='inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[11px] font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/25';
    dot.className='w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse';
    text.textContent='Online';
  }else{
    badge.className='inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[11px] font-medium bg-amber-500/10 text-amber-300 border border-amber-500/25';
    dot.className='w-1.5 h-1.5 rounded-full bg-amber-400';
    text.textContent='Offline';
  }
}

async function refreshHealth(){
  try{
    const r=await fetch('/api/health',{cache:'no-store'});
    health=await r.json();
    setOnline(!!health.ok);
  }catch(e){setOnline(false);return}
  document.getElementById('ap-ssid').textContent=health.apSsid||'—';
  if(health.apMode){
    show('ap-panel');hide('main-panel');
    if(!document.getElementById('prov-token').value)document.getElementById('prov-token').value=token();
  }else{
    hide('ap-panel');show('main-panel');
  }
  if(health.tokenSet && token()){
    hide('token-modal');
    if(!stateData)loadState();
  }else if(!health.apMode && health.tokenSet){
    show('token-modal');
  }
}

async function loadState(){
  try{
    const keep=current()?current().id:'';
    stateData=await api('/api/state');
    let idx=stateData.fans.findIndex(f=>f.id===keep);
    if(idx<0)idx=stateData.fans.findIndex(f=>f.id===rememberedFan());
    selected=idx>=0?idx:0;
    render();
    show('main-panel');
    hide('token-modal');
  }catch(e){if(String(e.message).indexOf('token')<0)toast(e.message,true)}
}

function rememberedFan(){try{return localStorage.getItem(LAST_FAN_KEY)||''}catch(e){return ''}}
function rememberFan(id){try{localStorage.setItem(LAST_FAN_KEY,id)}catch(e){}}

function current(){return stateData&&stateData.fans[selected]}

function setFanVisual(speed,direction){
  const rotor=document.getElementById('fan-rotor');
  document.getElementById('speed-label-hero').textContent=speed;
  rotor.className='w-24 h-24 text-emerald-400 animate-spin-fan flex items-center justify-center';
  if(direction==='reverse')rotor.classList.add('reverse-spin');
  rotor.classList.add('fan-speed-'+speed);
  if(speed===0){
    rotor.classList.remove('drop-shadow-[0_0_16px_rgba(16,185,129,0.35)]');
    rotor.classList.add('text-slate-600');
  }else{
    rotor.classList.add('drop-shadow-[0_0_16px_rgba(16,185,129,0.35)]');
  }
  const badge=document.getElementById('flow-mode-badge');
  if(direction==='reverse'){
    badge.innerHTML='<svg class="w-3 h-3 text-emerald-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 10l7-7m0 0l7 7m-7-7v18"/></svg> Exaustar';
  }else{
    badge.innerHTML='<svg class="w-3 h-3 text-emerald-400" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M19 14l-7 7m0 0l-7-7m7 7V3"/></svg> Ventilar';
  }
}

function setLightUI(on){
  const btn=document.getElementById('light-toggle-btn');
  const title=document.getElementById('light-title');
  const sub=document.getElementById('light-subtext');
  const badge=document.getElementById('light-status-badge');
  const icon=document.getElementById('light-icon-box');
  if(on){
    btn.className='press-effect relative overflow-hidden rounded-2xl bg-[#1E2025] border border-amber-500/35 p-4 text-left transition duration-200 shadow-[0_0_20px_rgba(245,158,11,0.06)] group';
    title.className='font-semibold text-sm text-amber-300';title.textContent='Luz ligada';
    sub.textContent='Toque para desligar';
    badge.className='text-[10px] font-bold tracking-wide uppercase text-amber-400 bg-amber-500/15 border border-amber-400/30 px-2 py-0.5 rounded-full';badge.textContent='Ativa';
    icon.className='w-8 h-8 rounded-xl bg-amber-500/15 border border-amber-500/30 text-amber-300 flex items-center justify-center transition';
  }else{
    btn.className='press-effect relative overflow-hidden rounded-2xl bg-[#1E2025] border border-[#2E323A] p-4 text-left transition duration-200 group';
    title.className='font-semibold text-sm text-slate-300';title.textContent='Luz apagada';
    sub.textContent='Toque para acender';
    badge.className='text-[10px] font-medium tracking-wide uppercase text-slate-400 bg-[#121316] border border-[#2E323A] px-2 py-0.5 rounded-full';badge.textContent='Desligada';
    icon.className='w-8 h-8 rounded-xl bg-[#121316] text-slate-500 border border-[#2E323A] flex items-center justify-center transition';
  }
}

function render(){
  if(!stateData)return;
  const tabs=document.getElementById('fan-tabs');
  tabs.innerHTML=stateData.fans.map((f,i)=>
    `<button type="button" onclick="selectFan(${i})" class="whitespace-nowrap px-4 py-1.5 rounded-full text-xs ${i===selected?'font-bold bg-emerald-500 text-slate-950 shadow-md shadow-emerald-500/20 border border-emerald-400':'font-medium bg-[#1E2025] text-slate-400 border border-[#2E323A] hover:text-slate-200 transition'}">${esc(f.name)}</button>`).join('');
  const f=current();if(!f)return;
  const src={boot:'boot',hub:'hub',http:'HTTP',rf_remote:'controle físico',correction:'correção'}[f.source]||f.source;
  document.getElementById('fan-source').textContent=src+' • '+fmtTime(f.updatedAt);
  setLightUI(!!f.state.light);
  setFanVisual(f.state.speed,f.state.direction);
  document.querySelectorAll('.speed-btn').forEach(btn=>{
    const n=parseInt(btn.getAttribute('data-speed'),10);
    btn.className=n===f.state.speed
      ? 'speed-btn py-2.5 rounded-xl text-xs font-bold bg-emerald-500 text-slate-950 shadow-md shadow-emerald-500/25 border border-emerald-400/50 transition flex flex-col items-center justify-center'
      : 'speed-btn py-2.5 rounded-xl text-xs font-semibold text-slate-400 hover:text-white transition flex flex-col items-center justify-center';
  });
  document.getElementById('btn-mode-ventilar').className=f.state.direction==='forward'
    ? 'press-effect py-2.5 px-3 rounded-xl text-xs font-semibold flex items-center justify-center gap-2 bg-emerald-950/60 border border-emerald-500/40 text-emerald-300 transition shadow-sm'
    : 'press-effect py-2.5 px-3 rounded-xl text-xs font-medium flex items-center justify-center gap-2 bg-[#121316] border border-[#2E323A] text-slate-400 hover:text-slate-200 transition';
  document.getElementById('btn-mode-exaustar').className=f.state.direction==='reverse'
    ? 'press-effect py-2.5 px-3 rounded-xl text-xs font-semibold flex items-center justify-center gap-2 bg-emerald-950/60 border border-emerald-500/40 text-emerald-300 transition shadow-sm'
    : 'press-effect py-2.5 px-3 rounded-xl text-xs font-medium flex items-center justify-center gap-2 bg-[#121316] border border-[#2E323A] text-slate-400 hover:text-slate-200 transition';
  document.getElementById('power-telemetry').textContent='Vel. '+f.state.speed+' • Luz '+(f.state.light?'ON':'OFF');
  if(health)document.getElementById('diag-rssi').textContent=(health.rssi||0)+' dBm';
  renderDiag(f);
}

async function renderDiag(f){
  let d={};
  try{d=await api('/api/diag')}catch(e){}
  const rf=d.rf||{};
  const nvs=d.nvs||{};
  const lines=[
    'device      : '+(health?health.thing:'—'),
    'firmware    : '+(health?health.version:'—'),
    'id          : '+f.id,
    'source      : '+f.source,
    'updatedAt   : '+fmtTime(f.updatedAt),
    'counter RF  : '+f.counter,
    'RF radio    : '+(rf.radio?'ok':'ausente'),
    'RF TX/RX    : '+(rf.txCount||0)+'/'+(rf.rxCount||0),
    'RF checksum : '+(rf.rxChecksumFail||0),
    'RF sem cmd  : '+(rf.rxUnmatched||0),
    'RF casados  : '+(rf.rxMatched||0),
    'ultimo RF   : '+(rf.lastFrame||'—'),
    'RF guard    : '+(rf.txGuard?'ativo':'livre'),
    'NVS livres  : '+(nvs.freeEntries!=null?nvs.freeEntries:'—'),
    'heap livre  : '+(health?health.heap:0)+' bytes',
    'uptime      : '+(health?health.uptime:0)+' s',
    'Hub         : '+(health?health.hub:'—'),
    'pendingSync : '+(stateData.pendingSync?'sim':'não')
  ];
  document.getElementById('diag-content').textContent=lines.join('\n');
}

function selectFan(i){selected=i;const f=current();if(f)rememberFan(f.id);render()}

async function postState(partial,force){
  const f=current();if(!f)return;
  try{
    const r=await api('/api/state',{method:'POST',body:JSON.stringify({id:f.id,state:partial,force:!!force})});
    rememberFan(f.id);
    toast('Aplicado'+(r.transmitted&&r.transmitted.length?' • RF: '+r.transmitted.join(', '):' • sem RF'));
    await loadState();
  }catch(e){toast(e.message,true);loadState().catch(()=>{})}
}
function powerOff(){postState({power:false,light:false},true)}
function toggleLight(){const f=current();if(f)postState({light:!f.state.light},true)}
function setSpeed(n){postState({speed:n},true)}
function setDirection(d){postState({direction:d},true)}

async function postCorrect(partial){
  const f=current();if(!f)return;
  try{
    await api('/api/correct',{method:'POST',body:JSON.stringify({id:f.id,state:partial})});
    rememberFan(f.id);
    toast('Estado atualizado (sem RF)');
    await loadState();
  }catch(e){toast(e.message,true)}
}

async function openLearn(){
  const f=current();if(!f)return;
  try{configData=await api('/api/config')}catch(e){configData=null}
  renderLearnList();
  document.getElementById('learn-status').textContent='';
  show('learn-modal');
}
function learnedSet(){
  const set=new Set();
  if(configData&&configData.fans){
    const fan=configData.fans.find(x=>x.id===current().id);
    if(fan&&fan.commands)Object.keys(fan.commands).forEach(k=>set.add(k));
  }
  return set;
}
function renderLearnList(){
  const learned=learnedSet();
  document.getElementById('learn-list').innerHTML=COMMANDS.map(c=>
    `<div class="learn-row"><div><div class="text-xs text-slate-200">${esc(COMMAND_LABELS[c])}</div><div class="text-[10px] ${learned.has(c)?'text-emerald-400':'text-slate-500'}">${learned.has(c)?'aprendido':'não aprendido'}</div></div><button onclick="startLearn('${c}')">Aprender</button></div>`).join('');
}
async function startLearn(command){
  const f=current();if(!f)return;
  try{
    await api('/api/learn/start',{method:'POST',body:JSON.stringify({id:f.id,command})});
    learnActive=true;
    document.getElementById('learn-status').textContent='Escutando… pressione o botão "'+COMMAND_LABELS[command]+'" no controle.';
    pollLearn();
  }catch(e){toast(e.message,true)}
}
async function pollLearn(){
  clearTimeout(learnTimer);
  try{
    const res=await fetch('/api/learn/status',{headers:token()?{'Authorization':'Bearer '+token()}:{}});
    if(res.status===401){show('token-modal');return}
    const s=await res.json();
    if(s.active){
      document.getElementById('learn-status').textContent='Escutando… '+(s.id||'')+' / '+(s.command||'');
      learnTimer=setTimeout(pollLearn,700);
      return;
    }
    if(s.result==='ok'){
      document.getElementById('learn-status').textContent='Aprendido: '+(s.command||'')+(s.frame?' • frame '+s.frame:'');
      learnActive=false;
      configData=await api('/api/config').catch(()=>configData);
      renderLearnList();
      return;
    }
    if(s.result==='error'||s.result==='cancelled'){
      document.getElementById('learn-status').textContent=s.result==='cancelled'?'Cancelado.':'Não captou nenhum frame. Tente novamente.';
      learnActive=false;
      return;
    }
  }catch(e){}
  learnTimer=setTimeout(pollLearn,900);
}
async function cancelLearn(){
  try{await api('/api/learn/cancel',{method:'POST',body:'{}'});learnActive=false;document.getElementById('learn-status').textContent='Cancelado.'}catch(e){toast(e.message,true)}
}
function closeLearn(){clearTimeout(learnTimer);hide('learn-modal')}

async function openSettings(){
  show('settings-modal');
  document.getElementById('local-token').value=token();
  if(health)document.getElementById('device-info').textContent=(health.thing||'—')+' • firmware '+(health.version||'—')+' • '+(health.ip||'sem IP');
  try{configData=await api('/api/config');renderSettingsFans()}catch(e){}
}
function closeSettings(){hide('settings-modal')}
function renderSettingsFans(){
  if(!configData||!configData.fans)return;
  document.getElementById('settings-fans').innerHTML=configData.fans.map(f=>
    `<div class="settings-row"><div class="name">${esc(f.name)} <span class="text-slate-500">(${esc(f.id)})</span></div><div class="flex gap-1.5"><button onclick="renameFan('${esc(f.id)}')">Renomear</button><button class="danger" onclick="removeFan('${esc(f.id)}')">Remover</button></div></div>`).join('');
}
async function addFan(){
  try{const r=await api('/api/fan/add',{method:'POST',body:'{}'});toast('Ventilador criado: '+r.id);configData=await api('/api/config');renderSettingsFans();await loadState()}
  catch(e){toast(e.message,true)}
}
async function removeFan(id){
  if(!confirm('Remover '+id+'?'))return;
  try{await api('/api/fan/remove',{method:'POST',body:JSON.stringify({id})});toast('Removido');configData=await api('/api/config');renderSettingsFans();await loadState()}
  catch(e){toast(e.message,true)}
}
async function renameFan(id){
  const name=prompt('Novo nome:');if(!name)return;
  try{await api('/api/fan/rename',{method:'POST',body:JSON.stringify({id,name})});toast('Nome atualizado');configData=await api('/api/config');renderSettingsFans();await loadState()}
  catch(e){toast(e.message,true)}
}
async function saveWifi(){
  const ssid=document.getElementById('wifi-ssid').value.trim();
  const password=document.getElementById('wifi-pass').value;
  if(!ssid){toast('Informe o SSID',true);return}
  try{await api('/api/wifi',{method:'POST',body:JSON.stringify({ssid,password})});toast('Salvo; reiniciando…');closeSettings()}
  catch(e){toast(e.message,true)}
}
function exportConfig(){
  api('/api/config').then(cfg=>{
    const blob=new Blob([JSON.stringify(cfg,null,2)],{type:'application/json'});
    const a=document.createElement('a');a.href=URL.createObjectURL(blob);
    a.download='smart_control_config.json';a.click();URL.revokeObjectURL(a.href);
  }).catch(e=>toast(e.message,true));
}
function importConfig(input){
  const file=input.files&&input.files[0];if(!file)return;
  const reader=new FileReader();
  reader.onload=async()=>{
    try{await api('/api/config',{method:'POST',body:reader.result});toast('Configuração importada');configData=await api('/api/config');renderSettingsFans();await loadState()}
    catch(e){toast(e.message,true)}
  };
  reader.readAsText(file);input.value='';
}
function saveLocalToken(){localStorage.setItem(TOKEN_KEY,document.getElementById('local-token').value.trim());toast('Token salvo neste navegador');hide('token-modal');loadState()}
function reboot(){
  if(!confirm('Reiniciar o dispositivo?'))return;
  api('/api/reboot',{method:'POST',body:'{}'}).then(()=>toast('Reiniciando…')).catch(e=>toast(e.message,true));
}
function rotr(x,n){return (x>>>n)|(x<<(32-n))}
function sha256Bytes(bytes){
  const K=[0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
    0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
    0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
    0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
    0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
    0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
    0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
    0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2];
  const H=[0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19];
  const len=bytes.length;
  const total=((len+9+63)>>6)<<6;
  const buf=new Uint8Array(total);
  buf.set(bytes);buf[len]=0x80;
  const dv=new DataView(buf.buffer);
  const bits=len*8;
  dv.setUint32(total-8,Math.floor(bits/0x100000000));
  dv.setUint32(total-4,bits>>>0);
  const w=new Uint32Array(64);
  for(let i=0;i<total;i+=64){
    for(let j=0;j<16;j++)w[j]=dv.getUint32(i+j*4);
    for(let j=16;j<64;j++){
      const s0=rotr(w[j-15],7)^rotr(w[j-15],18)^(w[j-15]>>>3);
      const s1=rotr(w[j-2],17)^rotr(w[j-2],19)^(w[j-2]>>>10);
      w[j]=(w[j-16]+s0+w[j-7]+s1)>>>0;
    }
    let a=H[0],b=H[1],c=H[2],d=H[3],e=H[4],f=H[5],g=H[6],h=H[7];
    for(let j=0;j<64;j++){
      const S1=rotr(e,6)^rotr(e,11)^rotr(e,25);
      const ch=(e&f)^(~e&g);
      const t1=(h+S1+ch+K[j]+w[j])>>>0;
      const S0=rotr(a,2)^rotr(a,13)^rotr(a,22);
      const maj=(a&b)^(a&c)^(b&c);
      const t2=(S0+maj)>>>0;
      h=g;g=f;f=e;e=(d+t1)>>>0;d=c;c=b;b=a;a=(t1+t2)>>>0;
    }
    H[0]=(H[0]+a)>>>0;H[1]=(H[1]+b)>>>0;H[2]=(H[2]+c)>>>0;H[3]=(H[3]+d)>>>0;
    H[4]=(H[4]+e)>>>0;H[5]=(H[5]+f)>>>0;H[6]=(H[6]+g)>>>0;H[7]=(H[7]+h)>>>0;
  }
  return H.map(x=>('00000000'+x.toString(16)).slice(-8)).join('');
}
function readArrayBuffer(file){
  if(file.arrayBuffer)return file.arrayBuffer();
  return new Promise((res,rej)=>{const r=new FileReader();r.onload=()=>res(r.result);r.onerror=()=>rej(r.error);r.readAsArrayBuffer(file)});
}
function sha256File(file){
  return readArrayBuffer(file).then(buf=>{
    if(window.crypto&&crypto.subtle)return crypto.subtle.digest('SHA-256',buf)
      .then(h=>[...new Uint8Array(h)].map(b=>b.toString(16).padStart(2,'0')).join(''))
      .catch(()=>sha256Bytes(new Uint8Array(buf)));
    return sha256Bytes(new Uint8Array(buf));
  });
}
function uploadOta(){
  const input=document.getElementById('ota-file');
  const file=input.files&&input.files[0];
  if(!file){toast('Selecione um .bin',true);return}
  const bar=document.getElementById('ota-progress');bar.style.width='0%';
  sha256File(file).then(hash=>{
    if(!hash){toast('Falha ao calcular SHA-256',true);return}
    const xhr=new XMLHttpRequest();
    xhr.open('POST','/api/update?sha256='+hash);
    if(token())xhr.setRequestHeader('Authorization','Bearer '+token());
    xhr.upload.onprogress=e=>{if(e.lengthComputable)bar.style.width=Math.round(e.loaded/e.total*100)+'%'};
    xhr.onload=()=>{
      if(xhr.status===200){bar.style.width='100%';toast('Firmware enviado; reiniciando…');closeSettings()}
      else{toast('Falha no OTA ('+xhr.status+')',true)}
    };
    xhr.onerror=()=>toast('Falha de rede no OTA',true);
    const fd=new FormData();fd.append('firmware',file,file.name);xhr.send(fd);
  }).catch(()=>toast('Falha ao calcular SHA-256',true));
}

function saveToken(){
  const v=document.getElementById('token-input').value.trim();
  if(!v){toast('Informe o token',true);return}
  localStorage.setItem(TOKEN_KEY,v);
  hide('token-modal');loadState();
}
function genToken(){
  const c=window.crypto||window.msCrypto;
  if(!c||!c.getRandomValues){toast('Navegador sem gerador seguro',true);return}
  const a=new Uint8Array(24);
  c.getRandomValues(a);
  const hex=[...a].map(b=>b.toString(16).padStart(2,'0')).join('');
  document.getElementById('prov-token').value=hex;
  toast('Token gerado — copie para o hub');
}
async function scan(){
  try{
    const r=await fetch('/api/scan').then(x=>x.json());
    const sel=document.getElementById('scan-list');
    sel.innerHTML='<option value="">selecione</option>'+(r.networks||[]).map(n=>
      `<option value="${esc(n.ssid)}">${esc(n.ssid)} (${n.rssi} dBm${n.secure?' 🔒':''})</option>`).join('');
  }catch(e){toast('Falha ao buscar redes',true)}
}
function pickNetwork(){
  const v=document.getElementById('scan-list').value;
  if(v)document.getElementById('prov-ssid').value=v;
}
async function provision(){
  const ssid=document.getElementById('prov-ssid').value.trim();
  const password=document.getElementById('prov-pass').value;
  const tokenValue=document.getElementById('prov-token').value.trim();
  if(!ssid){toast('Informe o SSID',true);return}
  if(tokenValue.length<16){toast('Token precisa de ao menos 16 caracteres',true);return}
  try{
    const res=await fetch('/api/provision',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({ssid,password,token:tokenValue})});
    const data=await res.json().catch(()=>({}));
    if(!res.ok||data.ok===false)throw new Error((data.error&&data.error.message)||'falha');
    localStorage.setItem(TOKEN_KEY,tokenValue);
    toast('Configurado; reiniciando…');
    setTimeout(()=>location.reload(),5000);
  }catch(e){toast(e.message,true)}
}

function updateClock(){
  const now=new Date(), pad=n=>String(n).padStart(2,'0');
  document.getElementById('timestamp-clock').textContent=
    `${pad(now.getDate())}/${pad(now.getMonth()+1)}/${now.getFullYear()}, ${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
}

function bindPress(el,onTap,onHold){
  if(!el)return;
  let timer=null,held=false;
  el.addEventListener('pointerdown',()=>{held=false;timer=setTimeout(()=>{held=true;onHold()},600)});
  el.addEventListener('pointerup',()=>{clearTimeout(timer);if(!held)onTap()});
  el.addEventListener('pointerleave',()=>{clearTimeout(timer)});
  el.addEventListener('pointercancel',()=>{clearTimeout(timer)});
  el.addEventListener('contextmenu',e=>e.preventDefault());
}
document.querySelectorAll('.speed-btn').forEach(btn=>{
  const n=parseInt(btn.getAttribute('data-speed'),10);
  bindPress(btn,()=>setSpeed(n),()=>postCorrect({speed:n}));
});
bindPress(document.getElementById('master-off-btn'),powerOff,()=>postCorrect({power:false,light:false}));
bindPress(document.getElementById('light-toggle-btn'),toggleLight,()=>{const f=current();if(f)postCorrect({light:!f.state.light})});
bindPress(document.getElementById('btn-mode-ventilar'),()=>setDirection('forward'),()=>postCorrect({direction:'forward'}));
bindPress(document.getElementById('btn-mode-exaustar'),()=>setDirection('reverse'),()=>postCorrect({direction:'reverse'}));
document.getElementById('btn-learn-rf').addEventListener('click',openLearn);
document.getElementById('diagnostics-toggle-btn').addEventListener('click',()=>{
  const body=document.getElementById('diagnostics-body');
  const arrow=document.getElementById('diagnostics-arrow');
  body.classList.toggle('hidden');
  arrow.classList.toggle('rotate-90');
});

updateClock();
setInterval(updateClock,1000);
refreshHealth();
setInterval(refreshHealth,15000);
setInterval(()=>{if(stateData&&token())loadState().catch(()=>{})},12000);
</script>
</body></html>
)rawliteral";
