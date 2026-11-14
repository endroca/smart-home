import { describe, expect, it } from "vitest";
import { VADDetector, type VADEvent } from "./vad";

// Alimenta o detector em quadros de 50 ms (a mesma cadencia do recorder).
function feed(vad: VADDetector, rms: number, from: number, to: number): VADEvent[] {
  const events: VADEvent[] = [];
  for (let t = from; t <= to; t += 50) events.push(...vad.push(rms, t));
  return events;
}

const opts = {
  calibrationMs: 200,
  silenceMs: 500,
  noSpeechMs: 1000,
  maxMs: 10000,
  minSpeechMs: 300,
  minThreshold: 0.01,
  maxThreshold: 0.12,
};

describe("VADDetector", () => {
  it("desiste quando so ha ruido, sem falar", () => {
    const vad = new VADDetector(opts);
    const events = feed(vad, 0.001, 0, 2000);
    expect(events).toEqual(["no-speech"]);
    expect(vad.speaking).toBe(false);
  });

  it("detecta fala e encerra depois do silencio", () => {
    const vad = new VADDetector(opts);
    feed(vad, 0.001, 0, 200); // calibracao
    const start = feed(vad, 0.05, 250, 1300); // ~1s falando
    const end = feed(vad, 0.001, 1350, 2500); // silencio
    expect(start).toContain("speech-start");
    expect(end).toContain("speech-end");
    expect(vad.speechMs).toBeGreaterThanOrEqual(1000);
  });

  it("trata um estalo curto como ausencia de fala", () => {
    const vad = new VADDetector(opts);
    feed(vad, 0.001, 0, 200);
    const events = [...feed(vad, 0.05, 250, 350), ...feed(vad, 0.001, 400, 1200)];
    expect(events).toContain("speech-start");
    expect(events).toContain("no-speech");
    expect(events).not.toContain("speech-end");
  });

  it("respeita o teto de duracao", () => {
    const vad = new VADDetector({ ...opts, maxMs: 1000 });
    feed(vad, 0.001, 0, 200);
    const events = feed(vad, 0.05, 250, 1500);
    expect(events).toContain("speech-start");
    expect(events).toContain("max-duration");
  });

  it("calibra o limiar acima do ruido de fundo", () => {
    const vad = new VADDetector(opts);
    feed(vad, 0.02, 0, 200);
    expect(vad.threshold).toBeGreaterThan(0.05);
    const events = feed(vad, 0.12, 250, 500);
    expect(events).toContain("speech-start");
  });

  it("nao emite eventos depois de encerrar", () => {
    const vad = new VADDetector({ ...opts, noSpeechMs: 500 });
    feed(vad, 0.001, 0, 600);
    expect(vad.push(0.05, 650)).toEqual([]);
  });
});