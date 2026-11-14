// Voz no cliente. O push-to-talk grava audio e o hub transcreve (Whisper) quando
// o STT esta configurado; sem STT, cai para a Web Speech API do navegador.
// Em todos os casos o hub recebe texto e resolve/executa a intencao.

import { ApiError, api } from "$lib/api";
import { toast } from "$lib/stores/toast.svelte";
import { hub } from "$lib/stores/data.svelte";
import { audioExtension } from "$lib/actions/recorder";

export interface STTStatus {
  available: boolean;
  provider?: string;
  model?: string;
  language?: string;
}

interface VoiceResponse {
  needs_disambiguation?: boolean;
  candidates?: { label?: string; room?: string }[];
  speech?: string;
  plan?: { speech?: string };
}

export function voiceSupported(): boolean {
  const w = window as unknown as { SpeechRecognition?: unknown; webkitSpeechRecognition?: unknown };
  return !!(w.SpeechRecognition || w.webkitSpeechRecognition);
}

// isIOSStandalone: no PWA instalado o WebKit nao expoe SpeechRecognition. O
// caminho por gravacao + Whisper nao tem essa limitacao.
export function isIOSStandalone(): boolean {
  return (
    /iPad|iPhone|iPod/.test(navigator.userAgent) &&
    ((navigator as unknown as { standalone?: boolean }).standalone === true ||
      window.matchMedia("(display-mode: standalone)").matches)
  );
}

export function voiceUnavailableReason(): string {
  if (!window.isSecureContext) {
    return "A voz precisa de HTTPS, e este endereço é http://. Na Pi, rode `tailscale serve --bg http://127.0.0.1:80` e abra o endereço https://<pi>.<tailnet>.ts.net.";
  }
  if (isIOSStandalone()) {
    return "Este navegador não expõe reconhecimento de voz. Configure o STT (Whisper) no hub para usar a voz no iPhone.";
  }
  return "Este navegador não expõe reconhecimento de voz. Configure o STT (Whisper) no hub ou digite o comando.";
}

export function speak(text: string): void {
  if (!text || !("speechSynthesis" in window)) return;
  try {
    const u = new SpeechSynthesisUtterance(text);
    u.lang = "pt-BR";
    window.speechSynthesis.speak(u);
  } catch {
    /* ignore */
  }
}

// voiceConfig asks the hub whether it can transcribe audio itself.
export async function voiceConfig(): Promise<STTStatus> {
  try {
    const r = await api.get<{ stt?: STTStatus }>("/api/voice/config");
    return r.stt ?? { available: false };
  } catch {
    return { available: false };
  }
}

async function applyVoiceResponse(r: VoiceResponse, spoken: boolean): Promise<void> {
  if (r.needs_disambiguation && (r.candidates ?? []).length) {
    const names = (r.candidates ?? [])
      .slice(0, 5)
      .map((c) => (c.label ?? "") + (c.room ? ` (${c.room})` : ""))
      .join(", ");
    toast.show("Qual deles? " + names);
    if (spoken) speak("Há mais de um dispositivo. Especifique o cômodo ou o nome.");
    return;
  }
  const speech = r.speech || r.plan?.speech || "";
  if (speech) {
    toast.show(speech);
    if (spoken) speak(speech);
  } else {
    toast.show("Não entendi o comando");
    if (spoken) speak("Não entendi o comando.");
  }
  await hub.load();
}

export async function runVoice(text: string, spoken: boolean): Promise<void> {
  try {
    const r = await api.post<VoiceResponse>("/api/voice/command", { text });
    await applyVoiceResponse(r, spoken);
  } catch (e) {
    toast.show((e as Error).message, true);
  }
}

// transcribeAndRun uploads one recorded clip. The hub transcribes it and runs
// the resulting command in a single round trip.
export async function transcribeAndRun(blob: Blob, mime: string): Promise<void> {
  const form = new FormData();
  form.append("audio", blob, `comando.${audioExtension(mime)}`);
  form.append("execute", "true");
  try {
    const r = await api.post<VoiceResponse>("/api/voice/command", form);
    await applyVoiceResponse(r, true);
  } catch (e) {
    if (e instanceof ApiError && e.code === "no_speech") {
      toast.show("Não ouvi nada");
      return;
    }
    toast.show((e as Error).message, true);
  }
}