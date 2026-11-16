# hub

Hub Go para Raspberry Pi 2B+ (armv7). Fala **HTTP puro** com os ESP32: descobre
cada gateway pelo manifesto (`GET /api/manifest`), comanda a API local e recebe o
estado que o device empurra. Serve a UI web, mantém a réplica do estado no SQLite
e coordena OTA.

## Status

Implementado. O hub fala HTTP puro na LAN com os ESP32, e o acesso remoto é feito
por Tailscale.

## Estrutura

```
cmd/smart-home-hub/main.go   entrypoint
internal/config/             JSON + variáveis de ambiente
internal/model/              manifesto, status, estado e helpers
internal/db/                 SQLite: devices, cômodos, automações, specs Tuya/Midea e a réplica do estado
internal/store/              inventário concorrente em memória (cache quente)
internal/fallback/           cliente HTTP do device (Bearer token)
internal/ota/                diretório de .bin, SHA-256 e rollback
internal/hub/                orquestração: descoberta, comandos, ingest, presença e réplica
internal/httpapi/            API JSON + assets embutidos
internal/tuya/               cliente do protocolo local Tuya (55AA/6699) e nuvem
internal/tuya/service/       adaptador que serve a UI (scan/registro/listen/controle)
internal/dispatch/           automações (evento → ação lógica) desacopladas do transporte
internal/functions/          catálogo de funções por device (rótulos da voz + ações de "desligar tudo")
internal/voice/              comandos de voz (parser pt-BR + rótulos + fallback LLM) → SetState/Tuya/dispatch
web/app/                     front novo em Svelte 5 + TS + Vite + Tailwind v4
                             (src/lib com stores, ui, devices/ e pages; vite-plugin-pwa)
web/dist/                    build do front (gerado, fora do git) embutido no binário Go
web/embed.go, web/build.go   embed de dist/ e o fingerprint (BuildID)
```

O banco SQLite (`internal/db`) é a fonte de verdade dos devices (ESP32 *things* e
equipamentos Tuya), dos cômodos, das associações ventilador→cômodo e das
automações. Tokens LAN por thing e URLs saíram do `config.json` e passam a ser
cadastrados pela UI. O front é um app **Svelte 5 + TypeScript + Vite + Tailwind
v4** em `web/app/`; o build sai em `web/dist/` e é embutido no binário Go via
`web/embed.go`. O `dist/` **não** é versionado: `make web` (ou `make deploy`, ou
os alvos Go, que chamam `web-assets`) o gera antes de compilar o binário.

A paleta é definida em `web/app/src/styles/theme.css` como variáveis CSS
(formato `R G B`) e o `@theme` do Tailwind v4 expõe os utilitários como
`rgb(var(--sh-token))`, então o mesmo `bg-surface`/`text-on-surface` resolve
para o tema claro ou escuro. O tema (claro/escuro/sistema) é escolhido na UI e
aplicado pela classe `.dark` / `.light` no `<html>`; em "Sistema" acompanha o
`prefers-color-scheme` do aparelho/PWA. Veja **Ajustes → Aparência** ou o botão
de tema no header.


## Build

Requisito: Go 1.23+. O front usa Node/npm só para **rebuildar** o `dist/` (ele é
commitado, então o binário sai sem Node). O `Makefile` na raiz de `hub/` agrupa
os comandos; veja `make help`.

```bash
cd hub
make init     # cria config.json e firmware/ (não sobrescreve)
make web      # compila o front (web/app) para web/dist
make check    # gofmt + vet + go test + testes do front (vitest)
make run      # sobe com -config config.json (go run; recompila em cache)
```

Alvos disponíveis:

| Alvo | Uso |
|---|---|
| `make init` | prepara o ambiente local (`config.json`, `firmware/` e `go mod download`) |
| `make web-deps` | instala as dependências do front (`npm ci`) |
| `make web` | compila o front (Svelte/Vite) para `web/dist` (gerado; embutido no binário) |
| `make web-assets` | garante `web/dist` (compila o front só se faltar) |
| `make dev-web` | sobe o `vite dev` com proxy para o hub em `:8080` (HMR) |
| `make test-web` | roda os testes do front (vitest) |
| `make build` | compila `smart-home-hub` na raiz do hub (para rodar/empacotar) |
| `make build-arm` | compila `smart-home-hub-linux-armv7` para a Pi |
| `make run` | inicia o hub com `go run` (sem deixar binário na raiz) |
| `make test` / `make test-race` | testes Go |
| `make vet` / `make fmt` / `make fmt-check` | análise e formatação |
| `make tidy` | ajusta `go.mod`/`go.sum` |
| `make check` | gofmt + vet + testes Go + testes do front |
| `make clean` | remove os binários gerados |

Equivalente manual:

```bash
go build ./...
go test ./...
go vet ./...
cd web/app && npm ci && npm run build && npm test
```

O CI (`.github/workflows/hub.yml`) roda build, vet, testes Go, `npm ci` +
`npm run build` + `npm test` do front e o build armv7.

## Configuração

Copie o exemplo e ajuste os caminhos:

```bash
cp config.example.json config.json
# config.json é ignorado pelo git (contém credenciais Tuya/Midea)
```

O arquivo tem os campos:

| Campo | Uso |
|---|---|
| `http_addr` | endereço da UI/API (default `:8080`) |
| `db` | banco SQLite (devices, cômodos, automações, specs e `local_key`) |
| `tuya_access_id` | Access ID/Client ID da nuvem Tuya (opcional; habilita registro/specs/controle por nuvem) |
| `tuya_access_secret` | Access Secret/Client Secret da nuvem Tuya |
| `tuya_region` | data center Tuya (`us`, `eu`, `cn`, `in`, `sg`, `us-e`, `eu-w`; default `us` via env) |
| `tuya_auto_listen` | mantém um listener LAN aberto por device Tuya e atualiza a telemetria sozinho (default `true`) |
| `tuya_refresh_seconds` | cadência da releitura de telemetria, em segundos (default `5`) |
| `tuya_cloud_poll_seconds` | cadência da leitura pela nuvem dos devices `cloud_only` (fechaduras que dormem), em segundos (default `300`) |
| `tuya_event_poll_seconds` | cadência do poll de `/report-logs` que vira evento de automação para devices `cloud_only`, em segundos (default `60`) |
| `tuya_cloud_push` | liga o **push** do Message Service (Pulsar) para devices `cloud_only` (default `true`); só conecta se existir algum `cloud_only` |
| `tuya_cloud_push_test` | usa o canal de **teste** do Message Service (`out/event-test`) em vez de produção |
| `midea_username` / `midea_password` | conta SmartHome usada somente para buscar a credencial LAN do aparelho no cadastro |
| `midea_cloud` | perfil SmartHome (default `SmartHome`) |
| `automation_grace_seconds` | tolerância, em segundos, para uma automação de horário atrasada ainda rodar após um restart (default `120`) |

Tokens e URLs por thing, cômodos e automações **não** ficam no `config.json`:
são cadastrados na UI e persistidos no SQLite. `firmware_dir` e
`firmware_base_url` também não são configuráveis: o diretório é relativo à raiz
de execução e a URL é derivada do IP da LAN (há overrides por ambiente para
casos avançados).

Variáveis de ambiente sobrescrevem os campos principais:
`HUB_HTTP_ADDR`, `HUB_FIRMWARE_DIR`, `HUB_FIRMWARE_BASE_URL`, `HUB_DB`,
`TUYA_ACCESS_ID` / `TUYA_ACCESS_SECRET` / `TUYA_REGION`,
`TUYA_AUTO_LISTEN` / `TUYA_REFRESH_SECONDS` / `TUYA_CLOUD_POLL_SECONDS` /
`TUYA_EVENT_POLL_SECONDS` / `TUYA_CLOUD_PUSH` / `TUYA_CLOUD_PUSH_TEST` e
`HUB_CONFIG` (caminho do arquivo).

## Rodar

```bash
make run            # dev: go run, com cache de compilacao
# com um binario ja compilado por 'make build':
make build && ./smart-home-hub -config config.json
# ou
HUB_CONFIG=config.json ./smart-home-hub
```

A UI fica em `http://<pi>:8080/`, **sem autenticação** — o hub deve ficar atrás
da LAN ou de uma VPN. No deploy da Pi o `config.pi.example.json` usa
`http_addr: ":80"` (ver a seção de deploy), então a UI fica em `http://<pi>/`.

## Deploy na Raspberry Pi

O hub é um binário Go com os assets web embutidos, então o deploy é: compilar
armv7 na máquina de desenvolvimento, enviar o binário e reiniciar o serviço. Os
scripts ficam em `deploy/` e usam `deploy/deploy.env` (ignorado pelo git).

Pré-requisitos: `go`, `rsync` e `ssh` na máquina de desenvolvimento; `rsync` na
Pi; e `sudo` **sem senha** para o usuário de deploy (os scripts rodam `sudo`
remoto de forma não interativa). O `seed-db` também precisa do `sqlite3` local.

O acesso precisa ser **não interativo** (sem senha), então configure uma vez:

```bash
# 1) chave SSH
ssh-keygen -t ed25519 -N ''            # se ainda não tiver uma
ssh-copy-id user@hub.local

# 2) sudo sem senha na Pi
ssh user@hub.local \
  'echo "user ALL=(ALL) NOPASSWD:ALL" | sudo tee /etc/sudoers.d/010-user-nopasswd && \
   sudo chmod 440 /etc/sudoers.d/010-user-nopasswd'

# teste
ssh -o BatchMode=yes user@hub.local true
```

Se faltar qualquer um dos dois, os scripts param antes de enviar nada e mostram
esse mesmo passo a passo.

```bash
cd hub
cp deploy/deploy.env.example deploy/deploy.env   # ajuste PI_HOST, PI_USER, ...
make install-pi   # bootstrap: usuário, diretórios, unit systemd, enable, certs
make seed-db      # opcional: semeia o banco SQLite desta máquina na Pi
make deploy       # build do front + armv7 + envio + restart + health check + rollback
```

A autenticação com os devices é por token, cadastrado na UI. O `install-pi` cuida de usuário, diretórios, unit systemd e do config.

### Layout na Pi

| Caminho | Uso |
|---|---|
| `/opt/smart-home-hub/smart-home-hub` | binário (único arquivo trocado pelo deploy) |
| `/opt/smart-home-hub/firmware/` | `.bin` de OTA (persistente) |
| `/etc/smart-home-hub/config.json` | configuração e segredos (`root:smart-home 0640`) |
| `/var/lib/smart-home-hub/hub.db` | SQLite (`StateDirectory`) |

A unit `deploy/smart-home-hub.service` sobe no boot e **nunca desiste**
(`Restart=always`, `StartLimitIntervalSec=0`), recuperando de queda de energia e
de travamento. O hub **não** tem autenticação própria: mantenha-o apenas na LAN
ou atrás de uma VPN.

A unit roda como usuário `smart-home` e escuta a **porta 80**. Como portas
`<1024` são privilegiadas, a unit concede só o necessário via
`AmbientCapabilities=CAP_NET_BIND_SERVICE` + `CapabilityBoundingSet=CAP_NET_BIND_SERVICE`
(sem rodar como root, e sem `setcap` no binário — sobrevive a cada deploy). O
`config.pi.example.json` já traz `"http_addr": ":80"`.

### Deploy normal

`make deploy` roda um comando só: primeiro compila o **front** (Svelte/Vite →
`web/dist`, instalando as deps com `npm ci` na primeira vez se faltar
`node_modules`), depois cross-compila o binário armv7 com a revisão do git
injetada (`-X smart_home/hub/internal/hub.Version=...`), envia, instala
atomicamente (guardando o binário anterior como `.prev`), reinicia e confere
`/api/health` até a versão nova responder. Se não responder, restaura o `.prev`.

Com `BUILD_WEB=0 ./deploy/deploy.sh` o build do front é pulado e o `web/dist`
do repositório é usado como está (útil sem Node na máquina). O deploy **não**
toca em `config.json`, segredos, `firmware/` nem no banco.

### Semeando o banco (uma vez)

O banco é estado, não artefato: `make seed-db` roda separado e é opt-in, para
não sobrescrever o que a Pi já criou. Ele tira um snapshot consistente com
`VACUUM INTO` (seguro mesmo com o hub local rodando, sem `-wal`/`-shm`), para o
serviço na Pi, salva o banco atual com timestamp, instala o snapshot e sobe de
novo. Para sobrescrever um banco existente use `FORCE=1 make seed-db` (ou
`./deploy/seed-db.sh --force`). Depois do primeiro seed, a Pi é a fonte de verdade.

### Acesso remoto (Tailscale)

Na Pi:

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

Acesso por `http://<host>` pelo MagicDNS, de qualquer lugar, **sem abrir
porta**. O Tailscale é VPN L3, então clientes remotos chegam com IP `100.x`
(faixa CGNAT `100.64.0.0/10`). Para HTTPS, use
`tailscale serve https / http://127.0.0.1:80`.

## Descoberta e estado

O hub não assina nada: ele **sonda** cada gateway cadastrado.

- No boot e enquanto um gateway ainda não tem manifesto na memória, o hub chama
  `GET /api/manifest` a cada **5 s**; com o manifesto em cache, **revalida a cada
  60 s**. Cadastrar/atualizar um gateway pela UI dispara uma sondagem imediata, e
  o heartbeat do device também dispara uma releitura quando o `manifestVersion`
  muda.
- Nomes `*.local` que não resolvem falham rápido, em vez de travar a sondagem.

### mDNS: o gateway se anuncia

O firmware publica um serviço mDNS `_http._tcp` com o nome do host
(`smart-control-rf01.local`) e um TXT `thing=<thing>`. O resolver do Go **não**
faz mDNS sozinho, então o hub faz a busca ele mesmo (`github.com/hashicorp/mdns`),
a cada 30 s:

- o endereço anunciado (`http://<ip>:<porta>`) fica em memória e é usado para
  alcançar o gateway **antes** de existir URL, IP ou manifesto;
- quando o gateway anunciado já está cadastrado, o hub dispara a descoberta —
  cadastrar só o **id + token** basta;
- um anúncio nunca cadastra nada sozinho: registrar continua sendo decisão do
  operador.

Precedência de endereço: URL configurada (não-`.local`) → IP reportado no status
→ endereço anunciado por mDNS → URL configurada `.local` → nome do manifesto.

### O hub configura a URL de push no device

Depois de cada sondagem bem-sucedida, o hub chama `POST /api/hub` no gateway com
a própria URL (`http://<ip-da-lan>:<porta>`), para que o device saiba onde
empurrar estado, status e eventos **sem configuração manual**. A URL é um IP, não
um nome `.local`: o cliente HTTP do ESP32 não resolve mDNS. Se a Pi não for
alcançável pelo IP da LAN (proxy, outra VLAN), defina `public_url` no
`config.json` ou `HUB_PUBLIC_URL`. A chamada é idempotente e o device só
regrava a NVS quando a URL muda.

O estado vem do **push do device** em `POST /api/things/<thing>/events` (ver
[`../contracts/hub-ingest.md`](../contracts/hub-ingest.md)): `state` a cada
mudança, `status` a cada 30 s (heartbeat) e `event` quando algo assíncrono
acontece. O hub só espelha — ele não inventa estado.

**Presença:** o hub considera o gateway online enquanto os batimentos não
atrasarem mais de 90 s; um watchdog de 15 s marca offline quem
silenciar, mantendo os últimos diagnósticos (IP, versão, RSSI). Após um restart
do hub, a presença restaurada é sempre `offline` até o próximo batimento.

## Comandos na LAN

O hub fala com o device por HTTP com `Authorization: Bearer <token>`, cadastrado
por gateway na UI:

- estado: `POST /api/state` (`force: true` reenvia o RF mesmo sem mudança);
- correção sem RF: `POST /api/correct`;
- learn: `POST /api/learn/start` + polling em `/api/learn/status`;
- config, `fan.*`, `reboot`: endpoints equivalentes;
- OTA: upload multipart em `/api/update`.

Sem token/URL para o gateway, o comando falha com `fallback_unavailable`. Não há
retry automático: se o device está inalcançável, o comando falha e o usuário
repete (ou usa a correção sem RF).

## Réplica do estado (cache quente + persistência)

O estado reportado pelos sub-devices de um gateway (os ventiladores) vive em
dois lugares:

- **cache quente**: o inventário em memória (`internal/store`), que serve a UI e
  as decisões do hub;
- **réplica durável**: a tabela `thing_states`, escrita pelo mesmo caminho que
  atualiza a memória, para que o estado sobreviva a um restart.

No boot o hub chama `LoadState` **antes** da descoberta: ele carrega a réplica
para a memória, então a UI já renderiza o último estado conhecido sem esperar a
sondagem na LAN. Esse estado restaurado vem marcado como `stale: true` — é o
último valor conhecido, não um valor confirmado — até o device reportar de novo
(um relatório fresco sempre limpa a marca).

A réplica é escrita só pelo que o device reporta (o push, ou a resposta de um
comando); nada do que o hub pretende entra nela. Quando um ventilador é removido
(`fan.remove`, sub-device removido do manifesto ou gateway apagado), a linha
correspondente é apagada junto — inclusive em cascata ao deletar o gateway.

## Dispositivos, cômodos e automações

O hub guarda tudo que não é descoberto automaticamente no SQLite:

- **Gateways (ESP32)** — `kind: "thing"`: token LAN e URL opcional, cadastrados
  na UI (aba **Configurações → Dispositivos**). O `id` é a **identidade** do
  gateway (o campo `thing` do manifesto, ex.: `smart-control-rf01`); o `name` é
  só exibição. Token/URL são atrelados ao `id`.
- **Sub-devices do gateway** — `kind: "fan"`: os ventiladores são **descobertos
  pelo manifesto e **cadastrados automaticamente** (id `gateway/fan0`),
  cada um com o seu cômodo. Renomear/remover usa os comandos do firmware.
- **Equipamentos Tuya** — `kind: "tuya"`: `local_key`, `category` (categoria do
  produto) e specs e estado. `cloud_only` marca os que dormem e são lidos pela
  nuvem (fechaduras).
- **Cômodos**: tabela `rooms`. Cada fan (sub-device), cada gateway e cada device
  Tuya tem um `room_id`. A UI agrupa os cards por cômodo. A **ordem** dos devices
  dentro do cômodo fica em `devices.sort` (botão **Ordenar** no Início;
  `POST /api/devices/order`).
- **Funções do device**: `devices.functions_json` guarda os rótulos/apelidos que a
  **voz** reconhece e quais funções contam como **"desligar"** (com o valor
  enviado). Vazio = padrões derivados das capacidades; a UI edita em
  **detalhe do device → Funções e voz** (`/api/devices/{id}/functions`).
  Só as **saídas** viram funções: num interruptor multi-tecla, cada tecla
  `switch_N`; o backlight, a pulsação, a trava e os `countdown_*` configuram o
  aparelho e ficam de fora — inclusive do "desligar tudo".
  O **rótulo cadastrado** é a fonte única do nome da tecla: o card do
  dispositivo mostra o mesmo rótulo que a voz reconhece (renomear em
  **Funções e voz** muda o card), e o front não mantém um segundo dicionário.
- **Automações**: uma fonte (um evento de device Tuya/Midea, um disparo manual ou
  um **horário** — as voláteis, que disparam uma vez e somem) ligada a ações
  lógicas, com um período de horário opcional. **Não** está
  amarrada a um transporte: uma ação aponta para um ventilador
  (`thing`+`device_id`+estado) ou para um device Tuya/Midea (`device`+`dp`/`state`+valor);
  quem decide o transporte é o hub. Para evitar **loops** (o interruptor liga a
  luz, a luz liga o interruptor), a fonte aceita **`only_user: true`** — a regra
  ignora mudanças causadas por outra automação — e o dispatcher ainda usa um
  **mapa de eco** (a regra não re-dispara pelo próprio efeito) e um **limite de
  profundidade** como rede de segurança.

Quando o firmware reindexa os slots (`fan.remove`), o hub remapeia o ventilador
— e o cômodo — pelo **id do sub-device** (estável) primeiro; o nome só é usado para
casar um slot que sumiu, e apenas quando o nome é único no manifesto. Assim
renomear todos os ventiladores para o mesmo nome não move nem apaga nenhum.
Associações antigas de `placements` são migradas para as linhas de fan.

O nome de exibição do hub (`devices.name`) e o nome no firmware
(`devices.firmware_name`, vindo do manifesto) são **independentes**: renomear no
hub não chama o device, e renomear na UI do ESP32 só atualiza `firmware_name`. A
UI mostra "No dispositivo: …" quando os dois divergem.

Equipamentos **Tuya** também podem ser renomeados na UI (aba **Configurações →
Tuya** ou engrenagem do card): o `id` do Tuya é a identidade e o nome é só
exibição, então o rename não altera a chave, o cômodo nem o estado.


## Layouts (design system modular)

Cada dispositivo tem um **layout** (design) associado quando entra no hub. O
layout é um componente da UI, escolhido no **detalhe do dispositivo** e
persistido na coluna `template` da tabela `devices` (vazio = inferir
automaticamente).

- O núcleo (`web/app/src/lib/devices/registry.ts`) é **device-agnóstico**:
  mantém o registry (`registerDevice`/`resolveTemplate`) e o kit de componentes.
  Cada layout registra
  `{ id, label, kinds, hint, match(device), component, config }`.
- Cada tipo de device tem sua pasta, dona do **layout + do comportamento**:
  `devices/fan` (ventilador), `devices/tuya` (lógica/DPs compartilhados),
  `devices/socket` (`tuya.socket`), `devices/gang` (`tuya.gang`, interruptor
  multi-tecla), `devices/switch` (`tuya.switch`),
  `devices/lock` (`tuya.lock`, fechaduras), `devices/midea` (`midea.ac`) e
  `devices/generic` (`generic`). Em cada uma,
  `logic.ts` é **puro** (testável sem DOM), `actions.ts` faz os efeitos e
  `Device.svelte`/`config/Config.svelte` apresentam.
- **Inferência**: o primeiro `match` que aceita o device vence — ventilador →
  `fan.ventisol`; Tuya com telemetria → `tuya.socket`; fechadura Tuya (pela
  categoria) → `tuya.lock`; Tuya com duas ou mais teclas `switch_N` → `tuya.gang`;
  Tuya com specs → `tuya.switch`; Midea → `midea.ac`; o resto → `generic`.
- **Ícones**: catálogo inline em `web/app/src/lib/ui/icons.ts`, todo em
  **contorno** (stroke) e renderizado pelo `Icon.svelte`; o `icons.test.ts` barra
  a volta de glifos preenchidos, que quebravam a coerência entre os cards.
- Para criar um novo design: crie `web/app/src/lib/devices/<nome>/` com o
  componente + as ações e chame `registerDevice({...})` em `index.ts` (o
  `devices/index.ts` importa todos). Cada layout tem `logic.test.ts` (puro) e
  `Device.test.ts` (DOM).

A UI usa os tokens de design (`surface-*`, `primary`, `tertiary`, `space-*`,
`label-sm`, ...) definidos em `web/app/src/styles/app.css` (bloco `@theme`) e as
cores em `theme.css`, então toda classe Tailwind usada nos componentes é
compilada no build do Vite. Prefira sempre os utilitários de token
(`bg-surface-container`, `text-on-surface-variant`, ...) a cores fixas: é o que
mantém o tema claro e o escuro coerentes.

Cada card tem uma **engrenagem** que abre o **detalhe do dispositivo**: ali
ficam os controles avançados (data points Tuya, brilho, modo, ...), as **ações**
(Ouvir/Atualizar/Renomear/Specs/Remover, Aprender/Renomear/Remover) e as
**informações de conexão** (IP, cômodo, fonte/último evento). O card fica só com
o essencial (ligar/desligar, telemetria e controles primários), sem poluir o
visual.

Os campos da configuração **não sofrem re-render por atualização alheia**. O
estado do hub é **reconciliado**, não substituído: o `load()` (30 s) alinha os
objetos existentes gravando só o que mudou, e um evento de DP grava **apenas
aquele DP** (`reconcile.ts`). Assim uma atualização de outro dispositivo — ou de
outro data point — não invalida a árvore de componentes, e o input que o
operador está editando não é tocado. Um `Enum` cujo valor reportado não está no
range declarado pela nuvem (ex.: `relay_status` = `off` num aparelho que declara
`power_off`/`power_on`/`last`) mostra esse valor como opção em vez de ficar em
branco, e quando o aparelho ignora a escrita o operador recebe um aviso — em vez
de a tela mentir sobre o aparelho.

A navegação é por um menu inferior (**Início, Cômodos, Automações, Ajustes**),
que troca de página sem modal; o cômodo selecionado é lembrado no
`localStorage` (`smart_home_room`) e restaurado no próximo carregamento.

Na tela **Início**, ao lado das abas de cômodo, o botão **Desligar tudo** age no
escopo da aba ativa: em **Todos** desliga a casa inteira; numa aba de cômodo,
só aquele cômodo. Ventiladores recebem motor + luz desligados e os dispositivos
Tuya têm suas saídas booleanas (`switch*`, `power`, `relay`) desligadas. Os
comandos saem **um dispositivo por vez**, com ~600 ms entre ventiladores, porque
o frame RF leva ~0,5 s — disparos em paralelo colidem no transmissor
(`busy`).

Nos controles do ventilador, **toque = envia RF** (com `force:true`, como a web
do firmware) e **segurar (~0,5 s) = corrige o estado conhecido sem RF**, então
não há botão "Corrigir". O card do ventilador tem **Desligar tudo** (motor +
luz) e **Luz**, como o firmware. O **Aprender** e o **Renomear** abrem modais
(via `config.get` + `learn/status` e `fan.rename`), em vez de `prompt`/`alert`.
O layout de **Ajustes** tem três seções: **Dispositivos** (inventário unificado
por origem — Gateways RF, Tuya e Midea — onde cada linha abre o detalhe do
dispositivo ou do gateway), **Aparência** (tema) e **Hub** (versão, build e
uptime). No detalhe do **gateway** ficam o **Diagnóstico** (LAN, espelhando o
`/api/diag` do firmware), o **Config**, o **firmware** (upload do `.bin` para o
hub, lista, instalar OTA e barra de progresso) e a gestão dos sub-devices
(adicionar/remover ventilador).

## Voz

O hub aceita comandos de voz como **texto** em `POST /api/voice/command`:

```json
{ "text": "liga o ventilador da sala", "execute": true }
```

A resposta traz a intenção interpretada (`plan`) e o resultado da execução. Em
caso de ambiguidade (a frase casa com mais de um device) vem
`needs_disambiguation` + `candidates` em vez de agir; `execute:false` só
interpreta (útil para depurar).

O **front-end de voz vive no cliente**, mas o STT pode ficar no hub: o botão de
microfone (**push-to-talk**) grava um clipe e o envia para o hub, que transcreve
com um endpoint compatível com OpenAI (`/audio/transcriptions`). Sem STT
configurado, a UI cai para a Web Speech API (`pt-BR`) e, sem ela, para o campo de
texto.

O microfone **se desliga sozinho**: um VAD por energia calibra o ruído de fundo
nos primeiros 250 ms e encerra a gravação após ~1,2 s de silêncio. Se ninguém
falar em ~5 s (só ruído), a captura é cancelada **sem** chamar a API. O teto é
20 s, e uma fala mínima de ~350 ms evita enviar um estalo.

### STT (Whisper)

`POST /api/voice/command` aceita JSON `{text, execute}` (como antes) e também
`multipart/form-data` com o campo `audio`: nesse caso o hub transcreve e executa
na mesma chamada. `GET /api/voice/config` informa à UI se o STT está disponível,
sem expor a chave.

| Campo | Uso |
|---|---|
| `voice_stt_provider` | `groq`, `openai`, `openrouter` ou `custom` |
| `voice_stt_url` | base do endpoint (default por provider; `custom` exige) |
| `voice_stt_key` | chave de API (`Authorization: Bearer`) |
| `voice_stt_model` | modelo (default por provider; `custom` exige) |
| `voice_stt_language` | idioma ISO-639-1 (default `pt`) |

Defaults: Groq → `https://api.groq.com/openai/v1` + `whisper-large-v3-turbo`;
OpenAI → `https://api.openai.com/v1` + `gpt-4o-mini-transcribe`; OpenRouter →
`https://openrouter.ai/api/v1` + `openai/whisper-large-v3-turbo`. Um
`voice_stt_url` sem provider é tratado como `custom`, o que cobre um whisper.cpp
local. Para o Groq:

```json
{ "voice_stt_provider": "groq", "voice_stt_key": "<token>" }
```

Variáveis de ambiente: `VOICE_STT_PROVIDER`, `VOICE_STT_URL`, `VOICE_STT_KEY`,
`VOICE_STT_MODEL`, `VOICE_STT_LANGUAGE`.

**Como conferir o provider:** no boot o hub loga
`voice: STT enabled (groq / whisper-large-v3-turbo, lang=pt)`;
`GET /api/voice/config` mostra `provider`, `model`, `url` e `available` (sem a
chave); e cada transcrição loga
`[voice] STT groq/whisper-large-v3-turbo: N bytes em Xms -> "..."`. Se aparecer
`custom` em vez de `groq`, a configuração não foi carregada — no serviço da Pi o
arquivo é `/etc/smart-home-hub/config.json`, não o `config.json` do repositório.

### Voz no iPhone

`getUserMedia`/`MediaRecorder` exigem **contexto seguro (HTTPS)**. Com o STT no
hub a voz deve funcionar também no **PWA da tela inicial**, ao contrário da Web
Speech API (limitação antiga do WebKit) — vale validar no aparelho.

Como o hub não embute TLS, a forma mais simples de ter HTTPS é o Tailscale:

```bash
# habilite HTTPS no admin do Tailscale (DNS → HTTPS Certificates) e, na Pi:
sudo tailscale serve --bg http://127.0.0.1:80
# abra https://<pi>.<tailnet>.ts.net no Safari
```

Atenção: Tailscale **não** é HTTPS. Acessar `http://<host>` (mesmo pela VPN) é
contexto inseguro e o microfone fica indisponível. Nesse caso a UI mostra o
motivo e cai para o campo de texto (dá para ditar pelo microfone do teclado do
iOS).

O parser é local e pt-BR: entende verbos (liga/desliga/acende/apaga), alvos
(ventilador/luz/tomada/ar), cômodos, velocidade (`velocidade 3`) e cenas (qualquer
automação manual, pelo nome). Antes de interpretar, a transcrição é normalizada
(pontuação removida) e passa por correções de confusões comuns do Whisper
(`Luiz` → `luz`, `ligado` → `ligar`), então uma transcrição imperfeita ainda
resolve. A execução reusa os mesmos caminhos do resto do hub (`SetState`,
controle Tuya e `dispatcher.Run`), então o hub continua decidindo o transporte.

Como o cômodo entra: os três ventiladores podem se chamar apenas "Ventilador" no
banco — o cômodo vem de `RoomID`. Por isso o prompt do STT e o do LLM listam
`Ventilador (Quarto)` e o resolvedor filtra por cômodo; **o nome do device não
precisa conter o cômodo**.

Como o alvo é escolhido:

- **Ventilador (ESP32):** casa por **cômodo + tipo** — "luz do escritório",
  "ventilador da sala". O nome cadastrado não precisa bater.
- **Tuya:** o hub não tem o schema de capacidades desses devices, então o
  **nome cadastrado** identifica o tipo — "tomada" acha o device cujo nome contém
  "tomada" (ex.: `Tomada-PC`), "ar" acha `Ar-condicionado`.
- Se ainda houver mais de um candidato, uma **palavra distintiva do nome** falada
  desempata; senão o hub devolve `candidates` e não adivinha.

> Dica: nomeie os devices Tuya com a palavra do tipo ("Tomada...", "Luz...",
> "Ar-condicionado..."). Ventiladores não precisam disso.

**Fallback opcional por LLM**: quando o parser local não resolve uma frase e
`voice_llm_enabled`/`voice_llm_url` estão configurados, o hub pede a um endpoint
compatível com `chat/completions` (function calling) a intenção e a resolve
contra os devices reais. Sem essa configuração o hub fica 100% local.

| Campo | Uso |
|---|---|
| `voice_llm_enabled` | liga o fallback por LLM (default `false`) |
| `voice_llm_url` | endpoint `chat/completions` (OpenAI-compatível) |
| `voice_llm_key` | chave de API (`Authorization: Bearer`) |
| `voice_llm_model` | modelo (default `gpt-4o-mini`) |

Variáveis de ambiente: `VOICE_LLM_ENABLED`, `VOICE_LLM_URL`, `VOICE_LLM_KEY`,
`VOICE_LLM_MODEL`.

> Como o restante da API, `/api/voice/command` não exige token: mantenha o hub
> na LAN ou atrás de uma VPN.

## Tempo real (SSE)

A UI **não faz polling**. O hub mantém um *event bus* interno e o expõe em
`GET /api/events` via **Server-Sent Events**: o navegador abre um `EventSource`
e recebe os updates assim que acontecem.

Eventos publicados:

| `type` | Origem | Conteúdo |
|---|---|---|
| `tuya` | DP recebido do device Tuya (LAN) | `device`, `kind`, `dps` |
| `state` | estado do ESP32 (ingest ou LAN) | `thing`, `id`, `reported` |
| `status` | status retained do ESP32 | `thing`, `status` |
| `manifest` | manifesto retained/descoberta do ESP32 | `thing`, `devices` |
| `event` | evento assíncrono do ESP32 | `thing`, `event` |

Os sub-devices de um gateway (fans) vêm do **banco** (`/api/devices`), então
aparecem assim que o hub responde — sem esperar o manifesto retained. O evento
`manifest` faz a UI re-renderizar na hora em que o gateway é (re)descoberto,
em vez de esperar o próximo `load()`.

O device Tuya já empurra os DPs pela conexão TCP local; o ESP32 empurra pelo
o push do device. O SSE só leva isso até o navegador. Um `load()` de reconciliação
lento (30 s) continua como rede de segurança.

O hub **só publica quando algum DP mudou**: os aparelhos reenviam o conjunto
completo a cada poucos segundos e reemitir o mesmo snapshot só faria a UI se
redesenhar à toa (e apagaria edições em curso na tela de configuração). Um
relatório parcial (só o DP que mudou) continua sendo publicado normalmente.

Do lado da UI, o estado é **reconciliado** (`web/app/src/lib/stores/reconcile.ts`):
o snapshot do `load()` alinha os objetos que já existem gravando só o que mudou,
e um evento de DP grava apenas aquele DP. Trocar os objetos por versões novas
(mesmo idênticas) invalidaria toda a árvore de componentes — inclusive um campo
de configuração sem relação com a atualização.

## Midea SmartHome

A integração aceita aparelhos de ar-condicionado Midea V3 encontrados na LAN. A aba **Midea** descobre os aparelhos, e o cadastro usa as credenciais `midea_username`/`midea_password` para obter token e chave individuais no SmartHome. O hub testa essa credencial diretamente no aparelho e grava os dados no SQLite. Depois do cadastro, leitura e comandos são enviados pela LAN; a conta não é consultada para controlar o aparelho. O hub redescobre o aparelho pelo ID Midea e atualiza o IP salvo quando ele muda.

Mudanças de estado recebidas pela conexão LAN autenticada são persistidas e enviadas à interface por SSE. Uma consulta a cada minuto fica como fallback de recuperação.

Configure as credenciais em `config.json` ou por `MIDEA_USERNAME` e `MIDEA_PASSWORD`. Sem elas ainda é possível fazer a descoberta local, mas não registrar um aparelho que ainda não tenha token/chave. O controle implementado inclui energia, temperatura, modo, ventilação e oscilação.

O aparelho é um kind de device (`midea`) como o Tuya: aparece no Início, nos cômodos, em Layouts, no "Desligar tudo", nas automações e na voz ("liga o ar da sala", "põe o ar em 22 graus").

Nas automações o ar-condicionado pode ser **gatilho** (`source: {"type":"midea_state","device":"1720...","dp":"power","value":true}`, onde `dp` é a chave de estado: `power`, `mode`, `target_temp`, `fan_speed`, ...) e **alvo** (`actions[].kind: "midea"` com `{device, state}`). O serviço lê cada aparelho a cada 10 s (`StatePoll`) além do push da conexão LAN; o "último valor" é semeado pelo estado salvo no boot para um restart não repetir disparo. O consumo vem do contador do próprio aparelho (`/api/midea/energy`) e aparece no painel recolhível do card e na engrenagem. O estado lógico é sempre o mesmo mapa (`power`, `mode`, `target_temp`, `fan_speed`, `swing_v`, ...), tanto na API quanto no SSE.

## Tuya

A aba **Tuya** escaneia a LAN, registra as `local_key` pela nuvem e lista os
DPs. Como um device que dorme (uma fechadura) **nunca aparece na varredura de
LAN**, use **"Listar da nuvem"** (aba **Ajustes → Dispositivos → Tuya**): o hub
lista os devices do app Tuya com a categoria e cadastra o escolhido por
`POST /api/tuya/register` (`{id}`). O hub agora também **controla**:
`POST /api/tuya/control` escreve um DP
(LAN primeiro, nuvem como fallback; a nuvem resolve o *code* a partir das specs)
e devolve o estado resultante.

O **estado atual** de cada device é acompanhado e persistido (tabela
`tuya_state`): ele é atualizado a cada evento enquanto o device está sendo
ouvido, aparece nos controles da UI e pode ser **relido sob demanda** com
`POST /api/tuya/state` (`{id}`) — LAN (`DP_QUERY`) primeiro, nuvem
(`/status`) como fallback. `GET /api/tuya/state?id=...` devolve apenas o último
valor conhecido, sem falar com o device.

Por padrão o hub mantém um **listener LAN aberto para cada device Tuya
registrado** (*auto-listen*) e **relê a telemetria periodicamente** na **mesma
conexão** (`tuya_refresh_seconds`, default 5 s) quando o device fica quieto.
Assim, qualquer device com telemetria (potência/tensão/corrente, temperatura,
umidade, ...) atualiza sozinho, sem o operador clicar em "Ouvir" — e sem abrir
uma segunda conexão, já que os devices Tuya costumam aceitar apenas uma conexão.
O botão **Parar** desliga o auto-listen só daquele device até um novo "Ouvir"
(ou reinício). Desligue tudo com `tuya_auto_listen: false`.

#### Devices que dormem (cloud-only)

Fechaduras de bateria (categorias `jtmspro`, `ms`, `mk`, ...) não ficam
conectadas o tempo todo: dormem em Wi‑Fi. O hub marca esses devices como
**`cloud_only`** quando os registra pela nuvem — não abre listener LAN para eles
e **lê o estado pela nuvem** (`tuya_cloud_poll_seconds`, default 300 s). O card
de uma fechadura usa o layout **`tuya.lock`** (inferido pela categoria) e mostra a
**bateria**, os alarmes, os últimos métodos de abertura e um painel recolhível de
**histórico de aberturas** (quem abriu, quando e como, via `door-lock/open-logs`
da Smart Lock Open Service — depende dessa assinatura no projeto Tuya).

No modo nuvem a leitura e o comando vão **à nuvem primeiro** — e, se a nuvem
**não responder**, o hub **cai para a LAN** (quando o device tem `local_key` e
endereço), então uma queda de internet não deixa um device preso. O flag é gravado
em `devices.cloud_only` e a `category` em `devices.category`.

#### Interruptores multi-tecla

Um interruptor de parede com duas ou mais teclas (categoria `kg` e afins) expõe
uma saída por tecla (`switch_1`, `switch_2`, ...). Quando há **duas ou mais**, a
inferência escolhe o layout **`tuya.gang`**: uma placa com uma tecla por saída,
o estado de cada uma e os atalhos **Ligar todas** / **Desligar todas** (só as
teclas). Cada tecla mostra o **rótulo cadastrado** em **Funções e voz** (sem
rótulo cadastrado, cai no nome da spec / dicionário curado). Os DPs que
configuram o aparelho — backlight, pulsação, trava, relé e os `countdown_*` —
não entram na placa nem no "desligar tudo"; ficam em **Ajustes do interruptor**,
na engrenagem do card.

O modo é **por device** e vale para qualquer Tuya, não só fechaduras: no detalhe
do dispositivo (engrenagem) há o controle **"Ler pela nuvem"** — ligado, o hub lê e
comanda pela nuvem (com a LAN só como reserva, se a nuvem falhar); desligado, volta
ao LAN automático (LAN primeiro, nuvem como fallback). O "Ouvir" some enquanto o
modo nuvem está ligado.

##### Eventos e automações (sem listener)

Um device `cloud_only` não tem listener, então os eventos vêm da **nuvem**. Há
dois caminhos, e eles **compartilham o mesmo cursor** por device
(`tuya_report_cursor`), então nunca disparam duas vezes o mesmo evento:

- **push (Message Service / Pulsar, preferido):** o hub assina o tópico
  `persistent://<access_id>/out/event` (assinatura `<access_id>-sub`, auth `auth1`,
  payload decifrado com **AES-GCM**) e entrega cada relato no instante em que a
  nuvem o recebe. Só conecta enquanto houver algum device `cloud_only`, para não
  consumir cota à toa. Ligado por `tuya_cloud_push` (default on). Depende de
  habilitar o **Message Service** e a **Messaging Rule** (`statusReport`, canal
  **Production**) no projeto Tuya.
- **poll (`/report-logs`, fallback):** a cada `tuya_event_poll_seconds` (default
  60 s); cobre o caso de o push não estar disponível.

O cursor é semeado no primeiro contato (olhando o histórico do device) e persiste,
então ligar uma automação **não repercute** aberturas antigas, nem depois de um
restart. Na automação (`source.type: "tuya_dp"`) `source.dp` é o **id** do data
point (`1` = `unlock_fingerprint`, `12` = `residual_electricity`); para uma
fechadura, `source.dps` pode listar todos os métodos de abertura e disparar em
qualquer um. Deixar `value` vazio dispara em qualquer abertura.

Uma automação também pode ter uma janela de horário opcional:

```json
"schedule": {"start": "18:00", "end": "03:00", "days": [1, 2, 3, 4, 5]}
```

`days` usa `0` = domingo … `6` = sábado; vazio significa todos os dias. Quando
`start` é maior que `end`, a janela atravessa a meia-noite e os dias se referem
ao dia em que ela começou (sexta 18:00 → sábado 03:00 pertence à sexta). A janela
usa o **fuso local do processo do hub** e é avaliada pelo horário do evento (não
pelo instante em que o poll o entregou). Cenas manuais ignoram o período e sempre
executam quando o botão é tocado.

##### Automações voláteis (gatilho de horário)

Uma automação com `source.type: "time"` não depende de evento: dispara **num
instante**. É o "desliga às 14" ou o "desliga daqui a 10 minutos". O hub resolve
o instante ao salvar e guarda `source.fire_at` (RFC3339 absoluto), então um
temporizador pendente **sobrevive a um restart**:

```json
"once": true,
"source": {"type": "time", "in_seconds": 600}
"source": {"type": "time", "at": "14:00"}                     // próximo HH:MM local
"source": {"type": "time", "at": "14:00", "date": "2026-11-13"}
"source": {"type": "time", "at": "18:00", "days": [1, 3, 5]}  // semanal, sem once
```

- `at` (+ `date` opcional) é horário de relógio local; sem `date`, é a **próxima
  ocorrência** (hoje se ainda não passou, senão amanhã). `in_seconds` é relativo e
  vira absoluto no save. Use **um ou outro**, nunca os dois.
- `days` (junto de `at`) transforma o gatilho em **semanal**: dispara nos dias
  marcados (`0` = domingo … `6` = sábado) e o hub **reagenda** a regra para a
  próxima ocorrência em vez de encerrá-la. Um gatilho semanal **não é volátil**
  (não aceita `once`) e não combina com `date`.
- `once: true` marca a automação como **volátil**: ao disparar (ou ao passar do
  horário), o hub **apaga** a regra. Sem `once` (e sem `days`), ela é apenas
  desabilitada. O gatilho de horário não usa `schedule` (o instante já é o
  horário) e uma janela nesse caso é rejeitada.
- Se o hub estava desligado no instante, ele dispara ao voltar desde que o atraso
  seja de no máximo `automation_grace_seconds` (default 120 s); passou disso, a
  volátil é descartada e a semanal pula para a próxima ocorrência, para não ligar
  algo horas atrasado.
- Na UI, os cards de device têm um botão **Temporizador** ("desligar em 30 min",
  "ligar em 1 h", "às HH:MM" ou o par "ligar às 10 / desligar às 14"), e o editor
  de automações ganha a opção **Horário / Agendamento**: escolhe-se o horário e,
  em **Repetir nos dias**, os dias da semana (vazio = disparo único). As voláteis
  aparecem na lista de Automações com o rótulo "Volátil · em 9 min"; as semanais,
  com "Seg, Qua · às 18:00". A mesma folha tem um
  **Ou personalize a ação**: em vez do ligar/desligar mestre, o operador escolhe
  o comando do aparelho (num interruptor de três teclas, qual chave) e o valor
  (ligar/desligar, velocidade, modo etc.).

Ventiladores e outros sub-dispositivos de um gateway também são gatilhos:

```json
"source": {"type": "thing_state", "device": "rf01/fan0", "dp": "light", "value": true}
```

O dispatcher assina os eventos `state` do barramento interno, então uma mudança
de luz, velocidade ou direção do ventilador dispara a automação do mesmo jeito
que um DP Tuya. Na UI, todas as famílias aparecem como **Dispositivo**: a escolha
do aparelho vem primeiro e as funções dele são carregadas em seguida.

No ventilador, o controle RF **não tem** um comando “ligar”: ligar é escolher uma
velocidade (`speed` 1..6); desligar é `speed: 0` ou o comando nativo
`power: false` (desligar tudo). Cada ação envia **um único campo** e o dispatcher
serializa ações de ventilador com um intervalo de 600 ms, porque o transmissor do
gateway não aceita dois frames ao mesmo tempo.

Cada device Tuya expõe `online` em `/api/tuya/devices`: é verdadeiro quando há
um **listener ativo** ou o device **respondeu nos últimos 90 s** (leitura,
comando ou evento). Assim, um device alcançável aparece **online** mesmo sem
estar transmitindo — o resumo "N dispositivos • M online" do Início usa esse
campo. Para um device **`cloud_only`**, a janela é o intervalo do poll da nuvem
(com folga), já que ele não tem link LAN. Para os ventiladores (ESP32), `online` vem do `status` retained quando o
o push do device (heartbeat) e de quão recentemente o hub os alcançou na LAN
(a descoberta sonda a LAN periodicamente).

A versão do protocolo é detectada **uma vez** e persistida (e também aproveitada
do broadcast de descoberta), então a primeira ação em um device novo é a mais
lenta (detecção) e as seguintes são locais e rápidas. Ler/escrever nunca bloqueia
na releitura: após um comando o hub atualiza o estado em segundo plano e publica
via SSE.

As *specifications* (controles da UI) vêm da **nuvem Tuya** e exigem as
credenciais (`tuya_access_id`/`tuya_access_secret`/`tuya_region` no `config.json`
**ou** `TUYA_ACCESS_ID`/`TUYA_ACCESS_SECRET`/`TUYA_REGION` no ambiente — o
ambiente tem precedência). Sem a nuvem:

- `GET /api/tuya/specs?id=...` devolve as specs **inferidas** dos DPs observados
  enquanto o device é ouvido (`Kind`/`Integer`/`String`), então dá para controlar
  via LAN mesmo sem nuvem;
- `POST /api/tuya/specs` (refresh pela nuvem) responde `503` com
  `tuya: cloud credentials are not configured`.

A nuvem expõe, por data point, um **nome curto** (`name`, ex.: "Child Lock") e
um `desc` (geralmente vazio). O hub passa a guardar os dois (`tuya_specs.name`/
`desc`, schema v7) e a UI mostra o **nome em vez da tag** (`code`), com a
descrição logo abaixo. Como a API **não** traz a descrição comportamental
(ex.: "desliga se ficar <3 W por 40 min"), há um **dicionário curado** pt-BR no
front (`DP_INFO` em `web/app/src/lib/devices/tuya/logic.ts`) para os códigos comuns
(`overcharge_switch`, `relay_status`, `switch_inching`, `cycle_time`, ...),
usado quando o device não informa nome/descrição.

O card da tomada tem uma seção **recolhível de consumo** (Hoje / 7 dias /
30 dias) alimentada pela **nuvem Tuya**
(`GET /api/tuya/energy?id=...&range=today|7d|30d`), nunca por um histórico local.
A fonte primária é o **log de relatos do dispositivo**
(`/v1.0/iot-03/devices/{id}/report-logs`, código `add_ele`): cada relato (~30 min)
traz um **incremento** em kWh que o hub soma por hora (hoje) ou por dia (7/30
dias), aplicando a escala do data point cacheada em `tuya_specs` (tipicamente
`scale: 3`, ou seja, milésimos de kWh). Isso funciona mesmo sem a tomada estar
vinculada a um espaço do Power Management. Se o log falhar ou vier vazio, o hub
cai para `statistics-trend`, que só tem dados com o aparelho vinculado a um
espaço. O cache em memória é curto (2 min para hoje, 30 min para 7 dias, 6 h
para 30 dias); os buracos são preenchidos com zero. A resposta sempre traz
`available` e `points`; sem permissão para as APIs de energia vem
`available:false` e `reason:"not_subscribed"`. O histórico começa no primeiro
relato do aparelho, então dias anteriores aparecem zerados.

Para habilitar a nuvem, preencha `tuya_access_id`, `tuya_access_secret` e
`tuya_region` no `config.json` (que é ignorado pelo git) **ou** exporte as
variáveis de ambiente ao subir o hub:

```bash
# opção A: no config.json
#   "tuya_access_id": "...", "tuya_access_secret": "...", "tuya_region": "us"

# opção B: ambiente (precede o arquivo)
TUYA_ACCESS_ID=xxx TUYA_ACCESS_SECRET=yyy TUYA_REGION=us ./smart-home-hub -config config.json
# (ou 'make run' e exporte as variaveis no ambiente)
```

## OTA

O hub lista os `*.bin` do `firmware_dir`, calcula o SHA-256 e faz upload
multipart para `POST /api/update?sha256=...` no device. Não há comando por URL:
o upload **é** a operação.

O `/firmware/<arquivo>.bin` também é servido na LAN (o device não autentica no
hub) para o resgate manual.

Um OTA por thing; o hub aguarda a versão nova aparecer no `status` (heartbeat) ou
no `manifest`. Se não aparecer em 10 min, tenta reinstalar o binário anterior
conhecido (rollback simples, uma tentativa).

## API local

| Método | Rota | Uso |
|---|---|---|
| GET | `/api/health` | saúde pública do hub |
| POST | `/api/voice/command` | interpreta uma frase pt-BR e executa o comando (`{text, execute?}`); devolve `plan`/`result` ou `candidates` |
| GET | `/api/events` | stream SSE de updates para a UI (sem polling) |
| GET | `/api/things` | inventário |
| GET | `/api/things/{thing}` | um thing |
| POST | `/api/things/{thing}/devices/{id}/state` | `{state:{...}, force?}` (`force:true` reenvia o RF mesmo sem mudança, como a web do firmware) |
| POST | `/api/things/{thing}/devices/{id}/correct` | correção sem RF |
| POST | `/api/things/{thing}/commands` | `{action, id?, ...}` |
| GET/POST | `/api/things/{thing}/config` | `config.get`/`config.set` |
| POST | `/api/things/{thing}/learn` | `{id, command}` |
| POST | `/api/things/{thing}/learn/cancel` | cancela aprendizado |
| GET | `/api/things/{thing}/learn/status` | estado do aprendizado (`{active, command, result, frame}`) |
| POST | `/api/things/{thing}/reboot` | reboot |
| GET | `/api/things/{thing}/diag` | diagnóstico LAN do gateway (RF/NVS/heap/uptime/hub) |
| POST | `/api/things/{thing}/events` | ingest do gateway (`state`/`status`/`event`); token do device |
| GET | `/api/firmware` | lista `.bin` e OTAs pendentes |
| POST | `/api/firmware` | upload de um `.bin` (multipart, campo `file`); grava no diretório de firmware do hub |
| DELETE | `/api/firmware/{name}` | remove um `.bin` do hub |
| POST | `/api/things/{thing}/ota` | `{file}` |
| GET | `/firmware/{file}.bin` | binário para o ESP32 |
| GET/POST | `/api/devices` | lista/cadastra devices (`kind`: `thing`/`tuya`/`midea`, `template` opcional) |
| GET/POST/DELETE | `/api/devices/{id}` | token/URL/cômodo/**layout** (`template`) e `cloud_only` de um device |
| POST | `/api/devices/order` | `{ids:[…]}`: grava a ordem dos devices dentro de um cômodo (atômico, só um cômodo) |
| GET/PUT | `/api/devices/{id}/functions` | rótulos/funções do device: `{functions:[{key,label,aliases,voice,off,off_value}]}`; `GET` devolve o catálogo derivado; `PUT` com `{}` restaura os padrões |
| POST | `/api/off` | `{ids:[…]}`: desliga cada device pelas funções marcadas `off` (ventiladores espaçados ~600 ms); devolve `results` por device |
| GET/POST | `/api/rooms` | lista/cria cômodos |
| GET/POST/DELETE | `/api/rooms/{id}` | um cômodo |
| GET/POST | `/api/placements` | ventilador → cômodo |
| GET/POST | `/api/automations` | lista/cria automações (`source`, `schedule`, `once` e `actions`; `source.type` pode ser `time`) |
| GET/POST/DELETE | `/api/automations/{id}` | uma automação |
| POST | `/api/automations/{id}/run` | dispara uma automação manual |
| POST | `/api/tuya/control` | escreve um DP (`{id,dp,value}`) e devolve o estado |
| GET/POST | `/api/midea/devices` | lista os aparelhos (estado e funções em cache, online sem dial) |
| GET/POST | `/api/midea/scan` | descobre aparelhos na LAN (`?ip=...` para tentar um endereço) |
| POST | `/api/midea/register` | `{id,ip,name?}`: busca a chave no SmartHome e verifica no aparelho |
| POST | `/api/midea/forget` | `{id}`: remove o aparelho e o cache de funções/estado |
| GET | `/api/midea/specs?id=...` | funções suportadas pelo protocolo e recursos anunciados pelo aparelho |
| POST | `/api/midea/specs` | relê as funções no aparelho (`{id}`) |
| POST | `/api/midea/control` | aplica controles LAN (`{id,state:{...}}`) |
| GET | `/api/midea/state?id=...` | último estado salvo (sem tocar na rede) |
| POST | `/api/midea/state` | consulta o estado atual diretamente na LAN (`{id}`) |
| GET | `/api/midea/energy?id=...&range=run\|total` | consumo lido no aparelho (ciclo atual/acumulado) + potência agora |
| GET/POST | `/api/tuya/specs` | lista/atualiza as specs de DP |
| GET/POST | `/api/tuya/state` | lê (GET) ou atualiza do device (POST) os DPs atuais |
| GET | `/api/tuya/energy` | consumo (`?id=...&range=today\|7d\|30d`); responde 200 com `available`/`reason` |
| GET | `/api/tuya/records` | histórico de aberturas de uma fechadura (`?id=...&limit=...`); responde 200 com `available`/`reason` |
| GET | `/api/tuya/cloud` | lista os devices do app Tuya (id, nome, categoria, online, registrado), para cadastrar os que não aparecem na LAN |

As rotas `/api/*` **não exigem token** (o hub roda na LAN/VPN). O firmware é
público por necessidade do device na LAN.

## Limitações atuais

- Inventário de things em memória; no restart o hub o reconstrói pela LAN
  (ver "Descoberta e estado"). Devices, cômodos, automações, specs Tuya/Midea
  **e o estado dos ventiladores** (`thing_states`) ficam no SQLite; o estado é
  semeado da réplica no boot e marcado como `stale` até o device confirmar.
- Sem histórico local (o gráfico de consumo vem da nuvem Tuya).
- `fan.add/remove` e `config.set` só atualizam o inventário na próxima sondagem
  LAN (ou quando o heartbeat traz um `manifestVersion` novo). `fan.rename` é
  local ao hub e atualiza a UI na hora.
- OAuth/TLS na UI não estão embutidos; para expor fora da LAN use um reverse
  proxy com TLS (Caddy, nginx ou `tailscale serve`).
- OTA é por thing, sem rollout de frota.
