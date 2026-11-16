// API falsa para os testes de componente/logica: registra cada chamada e
// devolve respostas vazias (ou o que o teste configurar).
//
// Uso:
//   const { api, calls } = makeFakeApi();
//   setApi(api);
//   ...
//   expect(calls).toContainEqual({ method: "post", args: ["/api/tuya/control", "d1", "1", true] });

import type { Api } from "$lib/api";

export interface RecordedCall {
  method: string;
  args: unknown[];
}

export interface FakeApi {
  api: Api;
  calls: RecordedCall[];
}

export function makeFakeApi(responses: Record<string, unknown> = {}): FakeApi {
  const calls: RecordedCall[] = [];
  const handler: ProxyHandler<Record<string, unknown>> = {
    get(_target, prop: string) {
      if (prop === "then") return undefined;
      // Helpers puros que nao sao endpoints.
      if (prop === "enc") return (v: string) => encodeURIComponent(v);
      return (...args: unknown[]) => {
        calls.push({ method: prop, args });
        if (Object.prototype.hasOwnProperty.call(responses, prop)) {
          return Promise.resolve(responses[prop]);
        }
        return Promise.resolve({});
      };
    },
  };
  return { api: new Proxy({}, handler) as unknown as Api, calls };
}
