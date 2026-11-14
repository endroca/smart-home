import { describe, expect, it } from "vitest";
import { audioExtension } from "./recorder";

describe("audioExtension", () => {
  it("mapeia os mimetypes do MediaRecorder para a extensao do upload", () => {
    expect(audioExtension("audio/webm;codecs=opus")).toBe("webm");
    expect(audioExtension("audio/webm")).toBe("webm");
    expect(audioExtension("audio/mp4")).toBe("m4a");
    expect(audioExtension("audio/ogg;codecs=opus")).toBe("ogg");
    expect(audioExtension("audio/wav")).toBe("wav");
    expect(audioExtension("audio/mpeg")).toBe("mp3");
    expect(audioExtension("")).toBe("webm");
  });
});