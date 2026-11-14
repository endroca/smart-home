// Gravacao push-to-talk com VAD no cliente.
//
// O navegador grava o audio (MediaRecorder) enquanto um AnalyserNode mede a
// energia e o VADDetector decide quando a fala terminou. Assim o botao se
// "desliga sozinho" tanto quando o operador para de falar quanto quando ele
// apenas fez ruido sem falar.

import { VADDetector, type VADEvent, type VADOptions } from "./vad";

export interface Recording {
  blob: Blob;
  mime: string;
  durationMs: number;
}

export interface RecorderOptions {
  /** Nivel (RMS) de cada quadro, para animar o indicador. */
  onLevel?: (rms: number) => void;
  /** Entrou/saiu da fala (util para o estado visual). */
  onSpeak?: (speaking: boolean) => void;
  vad?: VADOptions;
}

export interface RecorderHandle {
  promise: Promise<Recording | null>;
  cancel(): void;
}

// Ordem de preferencia: Opus e o mais leve e o Whisper aceita webm/mp4/ogg.
// O Safari grava em audio/mp4 (m4a), que o Groq e o OpenRouter aceitam.
const MIME_CANDIDATES = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4", "audio/ogg;codecs=opus"];

type AudioContextCtor = typeof AudioContext;

function audioContextCtor(): AudioContextCtor | undefined {
  if (typeof window === "undefined") return undefined;
  return (
    window.AudioContext ??
    (window as unknown as { webkitAudioContext?: AudioContextCtor }).webkitAudioContext
  );
}

export function pickMime(): string | null {
  if (typeof MediaRecorder === "undefined" || typeof MediaRecorder.isTypeSupported !== "function") {
    return null;
  }
  return MIME_CANDIDATES.find((mime) => MediaRecorder.isTypeSupported(mime)) ?? null;
}

export function mediaRecorderSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    typeof navigator !== "undefined" &&
    !!navigator.mediaDevices?.getUserMedia &&
    typeof MediaRecorder !== "undefined"
  );
}

// audioExtension deriva a extensao do arquivo enviado; Groq e OpenRouter usam
// a extensao para escolher o decodificador.
export function audioExtension(mime: string): string {
  const type = (mime || "").toLowerCase();
  if (type.includes("webm")) return "webm";
  if (type.includes("mp4") || type.includes("m4a") || type.includes("aac")) return "m4a";
  if (type.includes("ogg")) return "ogg";
  if (type.includes("wav")) return "wav";
  if (type.includes("mpeg") || type.includes("mp3")) return "mp3";
  return "webm";
}

// startRecording opens the microphone and resolves with the recorded clip,
// or null when the VAD decided there was no usable speech (or it was
// cancelled). It always releases the microphone and the AudioContext.
export function startRecording(opts: RecorderOptions = {}): RecorderHandle {
  let cancelled = false;
  let abort: (() => void) | null = null;

  const promise = (async (): Promise<Recording | null> => {
    const Ctx = audioContextCtor();
    if (!mediaRecorderSupported() || !Ctx) {
      throw new Error("Este navegador não permite gravar áudio");
    }

    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        channelCount: 1,
      },
    });
    if (cancelled) {
      stream.getTracks().forEach((track) => track.stop());
      return null;
    }

    const mime = pickMime();
    const recorder = mime ? new MediaRecorder(stream, { mimeType: mime }) : new MediaRecorder(stream);
    const chunks: Blob[] = [];
    recorder.ondataavailable = (event) => {
      if (event.data && event.data.size > 0) chunks.push(event.data);
    };
    const stopped = new Promise<Blob>((resolve) => {
      recorder.onstop = () => resolve(new Blob(chunks, { type: recorder.mimeType || mime || "audio/webm" }));
    });

    const ctx = new Ctx();
    const analyser = ctx.createAnalyser();
    analyser.fftSize = 1024;
    ctx.createMediaStreamSource(stream).connect(analyser);
    const samples = new Float32Array(analyser.fftSize);
    const startedAt = performance.now();

    let timer: ReturnType<typeof setInterval> | null = null;
    let done = false;

    const decision = new Promise<VADEvent | "cancel">((resolve) => {
      const finish = (event: VADEvent | "cancel") => {
        if (done) return;
        done = true;
        if (timer !== null) {
          clearInterval(timer);
          timer = null;
        }
        resolve(event);
      };
      abort = () => finish("cancel");

      const vad = new VADDetector(opts.vad);
      recorder.start(250);
      timer = setInterval(() => {
        analyser.getFloatTimeDomainData(samples);
        let sum = 0;
        for (let i = 0; i < samples.length; i++) sum += samples[i] * samples[i];
        const rms = Math.sqrt(sum / samples.length);
        opts.onLevel?.(rms);
        for (const event of vad.push(rms, performance.now())) {
          if (event === "speech-start") {
            opts.onSpeak?.(true);
            continue;
          }
          if (event === "speech-end") opts.onSpeak?.(false);
          finish(event);
          return;
        }
      }, 50);
    });

    const outcome = await decision;
    if (recorder.state !== "inactive") recorder.stop();
    const blob = await stopped;

    try {
      await ctx.close();
    } catch {
      /* ignore */
    }
    stream.getTracks().forEach((track) => track.stop());
    abort = null;

    if (outcome === "cancel" || outcome === "no-speech" || blob.size === 0) return null;
    return {
      blob,
      mime: blob.type || mime || "audio/webm",
      durationMs: Math.round(performance.now() - startedAt),
    };
  })();

  return {
    promise,
    cancel: () => {
      cancelled = true;
      abort?.();
    },
  };
}