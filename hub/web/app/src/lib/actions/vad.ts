// Detector de fala (VAD) por energia RMS.
//
// O detector e puro: recebe a energia de cada janela e o instante, devolve
// eventos. Assim da para testar a maquina de estados sem microfone. A captura
// (Microfone/MediaRecorder) fica em recorder.ts.

export type VADEvent = "speech-start" | "speech-end" | "no-speech" | "max-duration";

export interface VADOptions {
  /** Janela inicial usada para estimar o ruido de fundo. */
  calibrationMs?: number;
  /** Silencio continuo que encerra a fala (o "auto desligar"). */
  silenceMs?: number;
  /** Tempo sem nenhuma fala que cancela a gravacao. */
  noSpeechMs?: number;
  /** Teto da gravacao. */
  maxMs?: number;
  /** Fala minima para valer um comando (evita ruido isolado). */
  minSpeechMs?: number;
  /** Piso absoluto do limiar de RMS. */
  minThreshold?: number;
  /** Teto do limiar, para nao ficar surdo em ambiente muito ruidoso. */
  maxThreshold?: number;
  /** limiar = pisoDeRuido * thresholdFactor. */
  thresholdFactor?: number;
}

const DEFAULTS: Required<VADOptions> = {
  calibrationMs: 250,
  silenceMs: 1200,
  noSpeechMs: 5000,
  maxMs: 20000,
  minSpeechMs: 350,
  minThreshold: 0.01,
  maxThreshold: 0.12,
  thresholdFactor: 3,
};

// Quadros consecutivos acima do limiar antes de declarar fala: um estalo
// isolado nao inicia a gravacao.
const SPEECH_START_FRAMES = 2;

export class VADDetector {
  readonly #opts: Required<VADOptions>;
  #threshold: number;
  #startedAt = -1;
  #lastAt = -1;
  #lastVoiceAt = 0;
  #speechMs = 0;
  #above = 0;
  #calSamples: number[] = [];
  #calibrated = false;
  #speaking = false;
  #ended = false;

  constructor(opts: VADOptions = {}) {
    this.#opts = { ...DEFAULTS, ...opts };
    this.#threshold = this.#opts.minThreshold;
  }

  get threshold(): number {
    return this.#threshold;
  }

  get speaking(): boolean {
    return this.#speaking;
  }

  get speechMs(): number {
    return this.#speechMs;
  }

  push(rms: number, nowMs: number): VADEvent[] {
    if (this.#ended) return [];
    const events: VADEvent[] = [];

    if (this.#startedAt < 0) {
      this.#startedAt = nowMs;
      this.#lastAt = nowMs;
      this.#lastVoiceAt = nowMs;
    }
    const dt = Math.max(0, nowMs - this.#lastAt);
    this.#lastAt = nowMs;

    // Calibra o piso de ruido nos primeiros quadros e congela o limiar.
    if (!this.#calibrated) {
      this.#calSamples.push(rms);
      this.#updateThreshold();
      if (nowMs - this.#startedAt < this.#opts.calibrationMs) return events;
      this.#calibrated = true;
      this.#calSamples = [];
    }

    // Teto duro: sempre encerra, com fala ou sem.
    if (nowMs - this.#startedAt >= this.#opts.maxMs) {
      this.#ended = true;
      if (this.#speechMs >= this.#opts.minSpeechMs) events.push("max-duration");
      else events.push("no-speech");
      return events;
    }

    const active = rms >= this.#threshold;
    if (active) {
      this.#lastVoiceAt = nowMs;
      this.#speechMs += dt;
      this.#above++;
      if (!this.#speaking && this.#above >= SPEECH_START_FRAMES) {
        this.#speaking = true;
        events.push("speech-start");
      }
      return events;
    }

    this.#above = 0;

    // Silencio depois da fala: encerra. Fala curta demais vira "no-speech".
    if (this.#speaking && nowMs - this.#lastVoiceAt >= this.#opts.silenceMs) {
      this.#ended = true;
      this.#speaking = false;
      events.push(this.#speechMs >= this.#opts.minSpeechMs ? "speech-end" : "no-speech");
      return events;
    }

    // Nunca falou: desiste sem gastar rede/API.
    if (!this.#speaking && nowMs - this.#startedAt >= this.#opts.noSpeechMs) {
      this.#ended = true;
      events.push("no-speech");
    }
    return events;
  }

  #updateThreshold(): void {
    const sorted = [...this.#calSamples].sort((a, b) => a - b);
    const floor = sorted[Math.floor(sorted.length * 0.2)] ?? 0;
    const threshold = floor * this.#opts.thresholdFactor;
    this.#threshold = Math.min(this.#opts.maxThreshold, Math.max(this.#opts.minThreshold, threshold));
  }
}