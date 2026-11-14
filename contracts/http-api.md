# API HTTP do device

O servidor HTTP fica **sempre ligado** no ESP32, em `http://<mdns>:80`
(ex.: `http://smart-control-rf01.local/`). Ele serve dois papéis:

1. A página web local (ajustes manuais, diagnóstico, OTA).
2. A API que o hub usa para **comandar** o device e **descobri-lo**.

O transporte é **HTTP nas duas direções**: o hub chama a API local, e o device
empurra o que acontece de volta (`POST /api/things/<thing>/events` no hub — ver
`contracts/hub-ingest.md`).

## Autenticação

- `Authorization: Bearer <device-token>` em todo `/api/*`, exceto:
  - `GET /api/health` (público, sem dados sensíveis);
  - em modo AP: `GET /api/scan`, `POST /api/provision`, `GET /api/provision/status`.
- O token é definido no provisionamento (portal cativo ou serial), guardado como hash
  no NVS. Sem token definido, `/api/*` responde `401` (exceto health/provisionamento).
- O **mesmo token** autentica o device no hub: ele envia o **digest SHA-256 em
  hex** (`Authorization: Bearer <hex>`), não o texto plano, que nunca sai do NVS.
- A página web pede o token uma vez e guarda em `localStorage`.

## Endpoints

### `GET /api/health`

```json
{
  "ok": true,
  "version": "1.0.0",
  "build": "Nov  4 2026 19:50:00",
  "thing": "smart-control-rf01",
  "uptime": 12345,
  "heap": 142000,
  "rssi": -61,
  "hub": "connected",
  "apMode": false,
  "provisioned": true,
  "tokenSet": true,
  "apSsid": "smart-control-rf01",
  "ip": "192.168.1.42",
  "time": 1762286400,
  "rf": true
}
```

`hub` vale `connected`, `pending` (configurado mas sem resposta recente) ou
`disabled` (sem URL configurada).

### `GET /api/manifest`

O documento de descoberta (mesmo schema de
[`manifest.schema.json`](manifest.schema.json)). O hub o usa para **descobrir os
devices pela LAN**. Autenticado (`Bearer`).

```json
{
  "schemaVersion": 2,
  "manifestVersion": 4,
  "thing": "smart-control-rf01",
  "model": "smart-control",
  "protocol": "rf433",
  "firmware": { "version": "1.0.0", "build": "Nov  4 2026 20:49:47" },
  "http": { "mdns": "smart-control-rf01.local", "port": 80 },
  "events": { "state": true, "status": true, "heartbeat": 30 },
  "actions": ["fan.add", "fan.remove", "config.get", "config.set", "reboot", "ota.install"],
  "devices": [
    {
      "id": "fan0",
      "type": "ceiling_fan",
      "name": "Ventilador Sala",
      "capabilities": {
        "power": { "kind": "switch" },
        "light": { "kind": "switch" },
        "speed": { "kind": "number", "min": 0, "max": 6, "step": 1 },
        "direction": { "kind": "enum", "values": ["forward", "reverse"] }
      },
      "actions": ["state.correct", "learn.start", "learn.cancel", "fan.rename"]
    }
  ]
}
```

### `GET /api/state`

```json
{
  "ok": true,
  "pendingSync": false,
  "fans": [
    {
      "id": "fan0",
      "name": "Ventilador Sala",
      "state": { "power": true, "light": false, "speed": 3, "direction": "forward" },
      "source": "rf_remote",
      "updatedAt": 1762286400,
      "counter": 4
    }
  ]
}
```

### `POST /api/state`

Aplica estado absoluto. Por padrão só transmite RF nos campos que mudaram em
relação a `lastKnownState`.

```json
{ "id": "fan0", "state": { "power": true, "speed": 4 } }
```

Com `"force": true`, o RF é **reenviado** mesmo sem mudança de estado (ex.:
tocar de novo na velocidade atual). Só reenvia o que tem comando aprendido,
ignora `force` em `state.correct` e é usado pela UI no toque simples (o toque
longo usa `/api/correct`, sem RF).

```json
{ "id": "fan0", "state": { "speed": 2 }, "force": true }
```

Resposta:

```json
{
  "ok": true,
  "state": { "power": true, "light": false, "speed": 4, "direction": "forward" },
  "transmitted": ["power", "vel_4"]
}
```

- `409` se já houver transmissão/OTA/aprendizado em andamento.
- `404` para `id` desconhecido.
- A mudança entra na fila de push (`reported`) mesmo offline; o hub reconcilia depois.

### `POST /api/correct`

Corrige o estado **sem RF** (mesma semântica de `state.correct`).

```json
{ "id": "fan0", "state": { "power": true, "speed": 2 } }
```

Responde `409 busy` se houver OTA em andamento.

### `POST /api/learn/start`

```json
{ "id": "fan0", "command": "vel_2" }
```

`command` ∈ `power` | `light` | `vel_0` .. `vel_6` | `exhaust` | `vent`.
Resposta `{ "ok": true, "state": "listening", "id": "http-…" }`. O resultado sai em
`GET /api/learn/status` e no evento `learn.finished` do ingest.

### `GET /api/learn/status`

```json
{
  "ok": true,
  "active": false,
  "id": "fan0",
  "command": "vel_2",
  "result": "ok",
  "id": "http-12345",
  "frame": "00 0A 7B 12",
  "startedAt": 12345
}
```

`result` ∈ `idle` | `listening` | `ok` | `error` | `cancelled`.

### `POST /api/learn/cancel`

`{ "ok": true }`.

### `GET /api/config`

Retorna a configuração persistida no formato:

```json
{
  "version": 1,
  "fans": [
    {
      "id": "fan0",
      "name": "Ventilador Sala",
      "state": { "power": true, "light": false, "speed": 2, "lastSpeed": 2, "direction": "forward",
                 "counter": 3, "updatedAt": 1762286400, "source": "hub" },
      "commands": {
        "power": { "data": [0, 10, 116, 3], "offset": 0 }
      }
    }
  ]
}
```

Não inclui chaves, Wi-Fi nem token. `state.lastSpeed` é opcional e preserva a
última velocidade não nula.

### `POST /api/config`

Substitui a configuração inteira, valida, restaura o estado **sem RF**, persiste no NVS
e republica o `manifest`. O body é o mesmo objeto retornado pelo `GET /api/config`
(também é aceito embrulhado em `{ "config": { … } }`).

### `GET /api/diag`

```json
{
  "ok": true,
  "rf": {
    "radio": true, "txCount": 42, "rxCount": 17, "lastFrame": "00 0A 74 03",
    "lastFrameAt": 123456, "learning": false, "busy": false, "txGuard": false
  },
  "hub": { "configured": true, "url": "http://192.168.1.10:8080",
           "reachable": true, "pending": false, "ok": 42, "fail": 0,
           "manifestVersion": 4 },
  "nvs": { "freeEntries": 412 },
  "system": { "heap": 142000, "uptime": 12345, "time": 1762286400, "rssi": -61 }
}
```

### `POST /api/wifi`

```json
{ "ssid": "casa", "password": "…" }
```

Salva e reinicia em ~1 s. Se a conexão falhar, o AP de recuperação volta a subir.

### `POST /api/update`

`multipart/form-data` com o campo `firmware` (`.bin`). Autenticado. Faz o mesmo
caminho do OTA descrito em [`ota.md`](ota.md): valida, grava na partição OTA e
reinicia.

O hash SHA-256 é obrigatório na query string:

```
POST /api/update?sha256=<64 hex>
```

O firmware calcula o SHA-256 durante o upload e aborta com `sha_mismatch` antes
de aplicar. Sem hash válido responde `400 invalid_sha256`. A página local calcula
o hash no navegador (WebCrypto ou implementação JS) e sempre o envia.

### `POST /api/reboot`

`{ "ok": true }` e reinicia. Responde `409 busy` durante OTA/RF/aprendizado.

### `POST /api/fan/add`

Cria o próximo device lógico (até 8). Resposta
`{ "ok": true, "id": "fan3" }`. Republica o `manifest`.

### `POST /api/fan/remove`

```json
{ "id": "fan3" }
```

Remove o device lógico. Os slots seguintes são reindexados; o hub deve reler o
`manifest`. Não remove o último (`409 last_fan`).

### `POST /api/fan/rename`

```json
{ "id": "fan0", "name": "Ventilador Sala" }
```

Nome de 1..32 caracteres, **do dispositivo**. Republica o `manifest` e o hub
guarda esse valor como `firmware_name`; o nome de exibição do hub é
independente e só muda por `fan.rename` no hub.

### `POST /api/hub`

Configura a URL para onde o device empurra estado, status e eventos. O hub
chama este endpoint automaticamente ao descobrir o gateway, usando o próprio
IP da LAN (o ESP32 não resolve nomes `.local`).

```json
{ "url": "http://192.168.1.10:8080" }
```

`url` vazia desliga o push. A resposta traz o bloco `hub` do `GET /api/diag`.
O device persiste a URL na NVS e reenfileira todo o estado e o status.

## Modo AP (provisionamento/recuperação)

O AP usa o nome do thing (ex.: `smart-control-rf01`) e só existe quando não há Wi-Fi
configurado ou quando o usuário entra em recuperação (serial `ap` ou falha de conexão
por mais de 60 s). Ao conectar, o AP é desligado com
`WiFi.softAPdisconnect(true)` + `WiFi.mode(WIFI_STA)`.

### `GET /api/scan`

```json
{ "networks": [ { "ssid": "casa", "rssi": -55, "secure": true } ] }
```

### `POST /api/provision`

```json
{ "ssid": "casa", "password": "…", "token": "<device-token>" }
```

Salva Wi-Fi + token, desliga o AP e reinicia.

### `GET /api/provision/status`

```json
{ "ok": true, "configured": true, "connecting": false, "apMode": false,
  "apSsid": "smart-control-rf01", "ip": "192.168.1.42", "tokenSet": true }
```

## Códigos

| Código | Uso |
|---|---|
| `200` | sucesso |
| `400` | JSON inválido / campo inválido |
| `401` | token ausente/errado |
| `404` | id/rota inexistente |
| `409` | ocupado (RF, OTA, aprendizado) |
| `413` | firmware grande demais |
| `500` | falha interna (NVS, OTA) |
