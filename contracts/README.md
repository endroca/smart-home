# Contratos — smart_home

Este diretório é a **fonte de verdade** do contrato entre o firmware (`smart_control`),
o hub (Raspberry Pi) e qualquer adapter futuro (ex.: Tuya).

Nada de código de produção deve divergir daqui. Se o contrato mudar, muda primeiro
aqui e depois os dois lados.

## Transporte: HTTP nas duas direções

O device e o hub falam **HTTP puro** na LAN.

| Direção | Como |
|---|---|
| hub → device | o hub chama a API local do device ([`http-api.md`](http-api.md)) |
| device → hub | o device empurra estado/status/eventos ([`hub-ingest.md`](hub-ingest.md)) |

O device é a **fonte de verdade** do estado dos seus sub-devices. O hub espelha:
mantém um cache quente em memória e uma réplica durável no SQLite, e nunca
inventa estado.

### Descoberta

O hub lê `GET /api/manifest` do device ([`manifest.schema.json`](manifest.schema.json)).
Ele relê o documento quando o `manifestVersion` anunciado no heartbeat muda, na
sondagem periódica e ao cadastrar/atualizar o gateway na UI.

### Comandos e eventos

- Comandos one-shot (estado, learn, config, reboot, OTA) vão por HTTP e recebem
  resposta **síncrona** com o envelope `{ ok, result }` ou
  `{ ok: false, error: { code, message } }` — ver [`commands.md`](commands.md).
- Eventos assíncronos (`learn.finished`, `config.changed`, `ota.*`,
  `state.failed`) vão pelo ingest e são best-effort.

### Presença

O device manda um **heartbeat** de 30 s no ingest; o hub o considera online
enquanto os batimentos não atrasarem. Após um restart do hub, a
presença restaurada é sempre `offline` até o próximo batimento.

### Autenticação

- Um **token por gateway**, configurado no device (serial/portal) e cadastrado no
  hub pela UI.
- hub → device: `Authorization: Bearer <token>`.
- device → hub: `Authorization: Bearer <sha256(token) em hex>`. O device guarda
  só o digest na NVS, então nunca transmite o texto plano.

## Arquivos

| Arquivo | Conteúdo |
|---|---|
| `manifest.schema.json` | JSON Schema do manifesto de descoberta |
| `status.schema.json` | JSON Schema do status (liveness/diagnóstico) |
| `fan-state.schema.json` | JSON Schema do estado de um ventilador |
| `commands.md` | catálogo de comandos one-shot e eventos |
| `http-api.md` | API HTTP do device (comandos do hub + página local) |
| `hub-ingest.md` | ingest do hub: o device empurra estado/status/eventos |
| `ota.md` | OTA pela Pi e pelo formulário local |
| `examples/` | exemplos válidos dos payloads |

## Versionamento

- `manifest.schemaVersion` muda **somente** em breaking change.
- O hub deve suportar a versão atual e a anterior, e ignorar campos desconhecidos.
- Uma capability nova que o hub não conhece não pode quebrar a UI: ela é ignorada
  ou renderizada como genérica.
