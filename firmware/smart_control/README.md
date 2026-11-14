# firmware/smart_control

Firmware do ESP32 para o `smart_home`: um **controle universal RF 433 MHz**
(CC1101) que **aprende e reproduz** comandos de controles físicos. O primeiro
perfil é o ventilador de teto **Ventisol** (protocolo 18639) e o **suporte a IR
(infravermelho) vem em breve**.

## Escopo

- **Controle universal RF 433 MHz** via CC1101: aprende os comandos do controle
  físico e os reproduz. Perfil atual: ventilador de teto Ventisol (protocolo
  18639); **IR (infravermelho) em breve**.
- Estado absoluto por sub-device (`power`, `light`, `speed`, `direction` no perfil
  Ventisol).
- HTTP local sempre ligado (página web + API + OTA).
- **O device é a fonte de verdade**: ele empurra estado, status (heartbeat) e
  eventos para o hub por HTTP (`POST /api/things/<thing>/events`).
- SoftAP + portal cativo **só** para provisionamento/recuperação.
- OTA servido pelo hub (puxada via URL) e por upload local (`POST /api/update`).

O transporte é HTTP nas duas direções, na LAN.

## Estrutura

```
src/
  main.cpp              # setup/loop, NTP, comandos seriais, evento de aprendizado
  config.h              # pinos, constantes, versao, identidade do thing
  manifest.h/.cpp       # documento de descoberta (contrato com o hub)
  hub_client.h/.cpp     # push HTTP: estado, status (heartbeat) e eventos
  rf_ventisol.h/.cpp    # TX/RX, tabela de estados, aprendizado, contador
  state.h/.cpp          # lastKnownState, diff, config JSON, NVS
  http_server.h/.cpp    # pagina, API local, OTA
  wifi_provision.h/.cpp # SoftAP + captive portal + mDNS
  storage.h/.cpp        # NVS/Preferences + hash do token
  ota.h/.cpp            # download, SHA-256, Update
  web_ui.h              # pagina web (PROGMEM)
  web_ui_css.h          # CSS do Tailwind compilado (PROGMEM, gerado)
platformio.ini
partitions.csv          # app0/app1 de 0x1E0000 + otadata
```

## Dependencias

- `bblanchon/ArduinoJson` 7.x
- `lsatan/SmartRC-CC1101-Driver-Lib` 3.0.2
- `ESPmDNS`, `WebServer`, `HTTPClient`, `Update`, `Preferences` (core ESP32)

Core: `espressif32` 8.0.0 (Arduino ESP32 3.3.x), C++17.

## Build

Pré-requisitos:

- Python 3.8+
- PlatformIO Core: `pipx install platformio` (ou `pip install --user platformio`)

Há um `Makefile` nesta pasta com atalhos (roda o PlatformIO por baixo). De
dentro de `firmware/smart_control/`:

```bash
make help                   # lista os alvos
make build                  # compilar
make upload                 # gravar no ESP32 (PORT=/dev/ttyUSB0 para fixar)
make monitor                # monitor serial
make publish                # copia o .bin para hub/firmware/ (OTA pelo hub)
make clean                  # build limpo
make erase                  # apaga a flash inteira, INCLUSIVE a NVS
```

Da raiz do repositório, o equivalente é `make -C firmware/smart_control <alvo>`.

PlatformIO puro, a partir da raiz do repositório:

```bash
# compilar
pio run -d firmware/smart_control

# gravar no ESP32 (porta serial detectada automaticamente)
pio run -d firmware/smart_control -t upload

# monitor serial
pio device monitor -d firmware/smart_control -b 115200

# build limpo
pio run -d firmware/smart_control -t clean
```

A identidade do device (`THING_NAME`, default `smart-control-rf01`) é um default
de build, sobrescrevível por `build_flags = -DTHING_NAME=...`.

### UI e CSS (offline)

A interface usa Tailwind, mas o CSS é **compilado e embutido** em
`src/web_ui_css.h` e servido em `/style.css`. Não há CDN nem dependência de
internet no dispositivo.

Ao alterar `src/web_ui.h`, regenere o CSS (requer Node/npm só nesse momento):

```bash
./tools/gen-web-css.sh
```

O `web_ui_css.h` gerado é versionado; o firmware só precisa dele no build.

## Partições de flash

O `partitions.csv` define o mapa dos 4 MB de flash. Ele substitui a tabela padrão
do ESP32 porque o firmware precisa de **duas** partições de aplicação para OTA:

| Partição | Offset | Tamanho | Uso |
|---|---|---:|---|
| `nvs` | `0x9000` | `0x5000` (20 KB) | Preferences: config dos fans, contador RF, Wi-Fi, token e URL do hub |
| `otadata` | `0xE000` | `0x2000` (8 KB) | indica qual slot OTA (`app0`/`app1`) deve iniciar |
| `app0` | `0x10000` | `0x1E0000` (1.875 MiB) | firmware — slot OTA 0 |
| `app1` | `0x1F0000` | `0x1E0000` (1.875 MiB) | firmware — slot OTA 1 |
| `coredump` | `0x3D0000` | `0x10000` (64 KB) | dump de crash para diagnóstico |

- `app0`/`app1` têm o mesmo tamanho; o OTA grava no slot inativo e o bootloader
  alterna via `otadata`. Isso permite atualizar e voltar sem apagar o firmware atual.
- **Não há SPIFFS/LittleFS**: a página web fica no próprio firmware (`PROGMEM`),
  e o estado/config vão para o NVS. Por isso não é preciso uma partição de
  filesystem.
- Cada slot tem 1.966.080 bytes; o build atual usa ~1,49 MB (75,6%). Se o firmware
  crescer além disso, é preciso ajustar `partitions.csv` (ou usar flash maior),
  não apenas aumentar o binário.

## Provisionamento do token

O device se autentica **nas duas direções** com o mesmo token:

- o hub o envia em `Authorization: Bearer <token>` ao chamar a API local;
- o device o envia ao hub como `Authorization: Bearer <sha256(token) em hex>`
  — ele guarda apenas o digest na NVS, então nunca transmite o texto plano.

Configure o mesmo valor no device e no cadastro do gateway na UI do hub:

```
token <valor>        # serial, ou use o portal cativo
```

## Push para o hub

O device publica três tipos de mensagem em
`POST <hub_url>/api/things/<thing>/events`:

| `type` | Quando | Conteúdo |
|---|---|---|
| `state` | sempre que um ventilador muda | `id` + `reported` (power, light, speed, direction, source, updatedAt, counter) |
| `status` | heartbeat a cada 30 s e em mudanças de config/manifesto | versão, IP, RSSI, heap, uptime, `manifestVersion`, bloco `hub` |
| `event` | `learn.finished`, progresso/fim de OTA, `config.changed` | objeto do evento |

Regras de operação:

- O POST sai **do `loop()`**, nunca de dentro de um handler HTTP, com timeout
  curto — o handler apenas enfileira (`hub_client_queue_*`).
- A fila de estado é **coalescida por ventilador**: o estado é absoluto, então
  várias mudanças viram um único POST com o último valor.
- Eventos vão numa fila curta (6) e são descartados os mais antigos se encher.
- Falha → backoff exponencial de 5 s até 60 s. Sem hub configurado, o push fica
  desligado e o device funciona só com a API local.
- Ao conectar no Wi-Fi, o device empurra **todos** os estados e um status: o hub
  pode ter reiniciado sem ele.
- O `manifestVersion` no heartbeat permite ao hub reler o manifesto **quando muda**,
  em vez de revalidar num timer.

O hub configura esta URL **automaticamente** ao descobrir o gateway: ele chama
`POST /api/hub` com o próprio IP da LAN (o ESP32 não resolve nomes `.local`).
Não é preciso configurar nada manualmente no uso normal.

Configuração manual (serial, só para depuração):

```
hub http://192.168.1.10:8080   # liga o push
huboff                          # desliga
status                          # mostra url, alcance e contadores
```

Pela API local: `GET /api/diag` traz o bloco `hub` e `POST /api/hub` aceita
`{"url":"http://192.168.1.10:8080"}` (URL vazia desliga).

## Descoberta (manifesto)

`GET /api/manifest` devolve o mesmo documento que o device mantém, descrito em
[`contracts/manifest.schema.json`](../../contracts/manifest.schema.json). É o
contrato com o hub: descreve o modelo, o protocolo, o endpoint HTTP, os
sub-devices e suas capabilities. O hub o lê na descoberta e o relê quando o
`manifestVersion` muda.

O manifesto é gerado por `manifest.cpp` e não depende de nenhum transporte.

O firmware também se anuncia por **mDNS** (`_http._tcp`, com o TXT
`thing=<thing>`), e é isso que permite ao hub encontrá-lo antes de qualquer URL
estar configurada.

## Comportamento

- No boot: carrega config/estado do NVS, conecta Wi-Fi, empurra `status` e todos
  os estados, e atende a sondagem de manifesto do hub.
- RX capta o controle físico → identifica fan/comando pelo frame aprendido,
  atualiza o estado e empurra `state` com `source: "rf_remote"`.
- TX tem guarda de ~1,5 s (`selfTxGuardUntil`) para o RX não captar a própria
  transmissão.
- Comandos comparam com o estado conhecido; sem diff, não há RF. Com
  `force: true` em `POST /api/state` o RF é **reenviado** mesmo sem diff (a UI
  usa isso no toque simples; só reenvia o que tem comando aprendido).
- O frame RF `POWER` só é transmitido em um desligamento explícito
  (`power: false`). Para ligar ou mudar de velocidade, o firmware usa o botão
  de velocidade aprendido (`vel_N`), nunca manda `POWER` junto.
- O botão **Desligar** da UI não tem estado próprio: ele envia
  `{ "power": false, "light": false }`, ou seja, zera o motor e apaga a luz.
- Em mudanças locais, todo o RF é transmitido primeiro; a persistência do
  estado/contador na NVS e o push só acontecem depois.
- Logs são apenas de diagnóstico no serial (`Serial.printf`); nada de log é
  gravado na NVS ou em filesystem.
- Correção manual (`POST /api/correct`) **nunca** transmite RF e empurra
  `source: "correction"`.
- Na UI local: **toque** nos controles envia RF; **toque longo** (~0,6 s) atualiza
  o estado **sem RF** (usa `/api/correct`). Não há o botão/modal "Corrigir estado".
- Contador RF é persistido por ventilador (`counter` 0..7) e usado tanto no
  `transmitLearned` quanto no fallback `transmitFifo`.
- Velocidades absolutas exigem o `vel_N` aprendido; sem ele o comando responde
  `not_supported`. `speed: 0` usa `vel_0` aprendido (se existir) ou `power`.
- Power e luz têm fallback genérico. Direção (`vent`/`exhaust`) **exige**
  aprendizado; o código `OUTRO` é o da luz e não é usado como fallback de direção.

## API local

Página em `http://<thing>.local/` (ou IP). Token via `Authorization: Bearer`.
Rotas principais:

- `GET /api/health` (público), `GET /api/manifest`, `GET/POST /api/state`,
  `POST /api/correct`
- `POST /api/learn/start`, `GET /api/learn/status`, `POST /api/learn/cancel`
- `GET/POST /api/config`, `POST /api/fan/add|remove|rename`
- `GET /api/diag`, `POST /api/reboot`, `POST /api/wifi`
- `POST /api/update` (multipart; `?sha256=` obrigatório)
- Em modo AP: `GET /api/scan`, `POST /api/provision`, `GET /api/provision/status`

Contrato completo em [`contracts/http-api.md`](../../contracts/http-api.md).

## OTA

- Duas partições OTA (`app0`/`app1`) + `otadata`.
- O hub informa a URL do `.bin`, o SHA-256 e a versão; o device baixa, confere o
  hash em streaming, grava na partição inativa e reinicia.
- Upload local: `POST /api/update` (multipart) com `?sha256=` obrigatório. A
  página calcula o hash no navegador (WebCrypto ou implementação JS pura) e
  envia; sem hash válido o device responde `400 invalid_sha256`.
- `busy`/`409` enquanto uma atualização está em andamento; comandos de estado
  são rejeitados durante o OTA.

## Serial

`help`, `status`, `ap`, `apoff`, `token <valor>`, `wifi <ssid> <senha>`,
`hub <url>`, `huboff`, `learn <id> <comando>`, `reset`, `reboot`
(115200 baud).

## Status

O transporte é HTTP puro; o hub descobre o device pelo manifesto e recebe o
estado por push. Testes end-to-end dependem do hardware com CC1101 e do hub.
