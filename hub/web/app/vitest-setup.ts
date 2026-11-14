// Setup global do vitest: matchers de DOM e stubs que evitam rede real.
import "@testing-library/jest-dom/vitest";

// O SSE e aberto no onMount do shell; nos testes nao ha servidor, entao
// trocamos por um stub para nao gerar ECONNREFUSED.
class NoopEventSource {
  onmessage: unknown = null;
  onerror: unknown = null;
  onopen: unknown = null;
  readyState = 0;
  url: string;
  constructor(url: string) {
    this.url = url;
  }
  close() {}
  addEventListener() {}
  removeEventListener() {}
  dispatchEvent() {
    return false;
  }
}

globalThis.EventSource = NoopEventSource as unknown as typeof EventSource;
