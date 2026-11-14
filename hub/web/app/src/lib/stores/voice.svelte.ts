// Estado do comando de voz (push-to-talk) + o fallback por texto.
//
// Ordem de preferencia: o hub transcreve o audio (Whisper) → Web Speech API do
// navegador → campo de texto. O microfone sempre se desliga sozinho: pelo VAD
// quando o operador para de falar, ou por timeout quando nao ha fala.

import {
  runVoice,
  transcribeAndRun,
  voiceConfig,
  voiceSupported,
  voiceUnavailableReason,
} from "$lib/actions/voice";
import { mediaRecorderSupported, startRecording, type RecorderHandle } from "$lib/actions/recorder";
import { toast } from "$lib/stores/toast.svelte";

type SpeechCtor = new () => SpeechRecognitionLike;
type Mode = "stt" | "browser" | "none";
export type VoicePhase = "idle" | "listening" | "speaking" | "transcribing";

interface SpeechRecognitionLike {
  lang: string;
  interimResults: boolean;
  maxAlternatives: number;
  onresult: ((e: { results: { 0: { 0: { transcript: string } } } }) => void) | null;
  onerror: ((e: { error: string }) => void) | null;
  onend: (() => void) | null;
  start(): void;
  stop(): void;
}

class VoiceStore {
  open = $state(false);
  hint = $state("");
  phase = $state<VoicePhase>("idle");
  #rec: SpeechRecognitionLike | null = null;
  #handle: RecorderHandle | null = null;
  #mode: Mode | null = null;
  #pendingMode: Promise<Mode> | null = null;
  #cancelled = false;

  /** Gravando ou falando (o botao fica destacado). */
  get listening(): boolean {
    return this.phase === "listening" || this.phase === "speaking";
  }

  get transcribing(): boolean {
    return this.phase === "transcribing";
  }

  openSheet(message?: string) {
    this.hint =
      message ??
      "Digite o comando (ex.: ligar luz do escritório). No iPhone, use o microfone do teclado para ditar.";
    this.open = true;
  }

  async toggle(): Promise<void> {
    if (this.listening) {
      this.#cancelled = true;
      this.#handle?.cancel();
      this.#rec?.stop();
      return;
    }
    if (this.transcribing) return;

    // With the mode already known the path stays synchronous, so getUserMedia
    // runs inside the click gesture (Safari requires user activation).
    const mode = this.#mode ?? (await this.#resolveMode());
    if (mode === "stt") {
      await this.#record();
      return;
    }
    if (mode === "browser") {
      this.#listen();
      return;
    }
    this.openSheet(voiceUnavailableReason() + " Enquanto isso, digite o comando.");
  }

  // prepare resolves the STT/browser decision ahead of the first tap.
  prepare(): Promise<Mode> {
    return this.#resolveMode();
  }

  submit(text: string) {
    const trimmed = text.trim();
    if (!trimmed) return;
    this.open = false;
    void runVoice(trimmed, false);
  }

  // #resolveMode decides once per session. The hub is the authority: if it has
  // a transcription backend, use it even when the browser offers Web Speech
  // (Whisper handles names and pt-BR better, and works in the iOS PWA).
  #resolveMode(): Promise<Mode> {
    if (this.#mode) return Promise.resolve(this.#mode);
    if (!this.#pendingMode) {
      this.#pendingMode = (async () => {
        const stt = mediaRecorderSupported() && (await voiceConfig()).available;
        this.#mode = stt ? "stt" : voiceSupported() ? "browser" : "none";
        return this.#mode;
      })();
    }
    return this.#pendingMode;
  }

  // #record is the Whisper path: capture audio, let the VAD decide when the
  // command ended, then upload one clip.
  async #record(): Promise<void> {
    if (this.phase !== "idle") return;
    this.#cancelled = false;
    this.phase = "listening";
    const handle = startRecording({
      onSpeak: (speaking) => {
        if (this.phase === "listening" || this.phase === "speaking") {
          this.phase = speaking ? "speaking" : "listening";
        }
      },
    });
    this.#handle = handle;
    try {
      const recording = await handle.promise;
      this.#handle = null;
      if (!recording) {
        if (!this.#cancelled) toast.show("Não ouvi nada");
        return;
      }
      this.phase = "transcribing";
      await transcribeAndRun(recording.blob, recording.mime);
    } catch (e) {
      const message = (e as Error).message || "Falha ao gravar áudio";
      if (/permission|denied|not allowed/i.test(message)) {
        this.openSheet(
          "Microfone bloqueado. Libere o microfone nas permissões do navegador ou digite o comando.",
        );
      } else {
        toast.show(message, true);
      }
    } finally {
      this.#handle = null;
      this.phase = "idle";
    }
  }

  // #listen is the legacy Web Speech path, kept as a fallback when the hub has
  // no STT configured.
  #listen(): void {
    const w = window as unknown as { SpeechRecognition?: SpeechCtor; webkitSpeechRecognition?: SpeechCtor };
    const Ctor = w.SpeechRecognition || w.webkitSpeechRecognition;
    if (!Ctor) return;
    const rec = new Ctor();
    rec.lang = "pt-BR";
    rec.interimResults = false;
    rec.maxAlternatives = 1;
    rec.onresult = (e) => {
      this.phase = "transcribing";
      void runVoice(e.results[0][0].transcript, true).finally(() => {
        this.phase = "idle";
      });
    };
    rec.onerror = (e) => {
      this.phase = "idle";
      if (e.error === "not-allowed" || e.error === "service-not-allowed") {
        this.openSheet("Microfone indisponível. Digite o comando — no iPhone dá para ditar pelo microfone do teclado.");
      } else if (e.error !== "aborted" && e.error !== "no-speech") {
        this.hint = "Não entendi";
      }
    };
    rec.onend = () => {
      if (this.phase === "listening") this.phase = "idle";
    };
    this.#rec = rec;
    this.phase = "listening";
    try {
      rec.start();
    } catch {
      this.phase = "idle";
      this.openSheet();
    }
  }
}

export const voice = new VoiceStore();