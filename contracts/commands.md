# Comandos

Os comandos são **chamadas HTTP** do hub à API local do device, com resposta
**síncrona**. Não há fila de comandos: se o device não responder, o comando
falhou e o usuário repete.

| Ação | Requisição |
|---|---|
| `state.correct` | `POST /api/correct` |
| `learn.start` | `POST /api/learn/start` |
| `learn.cancel` | `POST /api/learn/cancel` |
| `fan.add` | `POST /api/fan/add` |
| `fan.remove` | `POST /api/fan/remove` |
| `fan.rename` | — (renomeia só o nome de exibição no hub) |
| `config.get` | `GET /api/config` |
| `config.set` | `POST /api/config` |
| `reboot` | `POST /api/reboot` |
| OTA | `POST /api/update` (multipart) — ver [`ota.md`](ota.md) |
| Estado | `POST /api/state` — ver [`http-api.md`](http-api.md) |

O hub expõe as mesmas ações em `POST /api/things/<thing>/commands`
(`{action, id?, ...}`) e o device responde no formato abaixo.

## Envelope de resposta

Sucesso:

```json
{ "ok": true, "result": { } }
```

Erro:

```json
{ "ok": false, "error": { "code": "unknown_device", "message": "fan9 nao existe" } }
```

Regras:

- Antes de transmitir, o device compara o estado pedido com o seu estado
  conhecido. Sem diferença, **não há RF** (apenas a resposta).
- `state.correct` **nunca** transmite RF.
- `speed: 0` sem `power: false` explícito usa o `vel_0` aprendido, se existir.
- Direção (`vent`/`exhaust`) exige o comando correspondente aprendido; sem ele,
  responde `not_supported`. O fallback genérico `OUTRO` é o da luz, não da direção.
- O frame de direção (`vent`/`exhaust`) é **nativo** no ventilador: com o motor
  desligado ele liga na velocidade 1; com o motor ligado mantém a velocidade e
  só reverte a direção. O device espelha isso no estado que reporta
  (`power:true`, `speed:1`) — **não envie** `power`/`speed` junto com
  `direction`: além de redundantes, o device transmitiria um segundo frame RF e
  a direção se perderia.
- O comando de desligar (`power:false`) também é **nativo**: desliga motor e luz
  de uma vez. O device espelha a luz (`light:false`) no estado reportado; mande
  só `power:false` (sem `light`), senão o device transmite um frame de luz extra.
- Um comando pode **demorar**: o frame RF leva ~0,5 s e o aprendizado espera o
  botão do controle. O hub usa timeout curto por requisição e serializa comandos
  ao mesmo device.

## Catálogo

### `state.correct`

Corrige o estado conhecido sem acionar o ventilador. Usado quando o controle
físico foi usado e o RX do CC1101 não captou (ou captou errado), ou quando o
usuário sabe o estado real.

```json
{ "id": "fan0", "state": { "power": true, "speed": 2 } }
```

- `state` é parcial; campos ausentes permanecem.
- O device atualiza o estado e passa a reportar `source: "correction"`.

### `learn.start`

Inicia o aprendizado de um botão do controle físico.

```json
{ "id": "fan0", "command": "light" }
```

- `command` ∈ `power` | `light` | `vel_0` .. `vel_6` | `exhaust` | `vent`
  (os 11 comandos RF; os nomes são os mesmos do estado/config).
- Resposta imediata: `{ "ok": true, "result": { "state": "listening" } }`.
- O resultado sai por `GET /api/learn/status`: `result` é `listening`, `idle`,
  `ok` ou `error`, com `frame` (hex do frame captado) quando deu certo, ou
  `error = "rf_timeout"` após ~15 s. `learn.cancel` produz `error = "cancelled"`.
- O device também **empurra** um evento `learn.finished` ao hub, para a UI ficar
  sabendo sem polling.
- Um novo `learn.start` substitui o aprendizado anterior.
- O device precisa do CC1101 presente; sem rádio responde `rf_unavailable`.

### `learn.cancel`

Cancela qualquer aprendizado em andamento. Responde `ok: true`.

### `fan.add`

Cria o próximo slot livre (`fan0`..`fan7`), com nome padrão `Ventilador N`.

- Resposta: `{ "ok": true, "result": { "id": "fan3" } }`.
- Limite de 8 devices lógicos; acima disso responde `fan_limit`.
- O device persiste no NVS, incrementa o `manifestVersion` e o hub relê o
  manifesto.

### `fan.remove`

```json
{ "id": "fan3" }
```

- Remove o device lógico, persiste e incrementa o `manifestVersion`.
- Os slots seguintes são **reindexados**: remover `fan1` transforma `fan2` em
  `fan1`. O hub relê o manifesto e remapeia pelo `id` (estável).
- Não é permitido remover o último device (`last_fan`).
- O hub apaga o estado replicado daquele sub-device.

### `fan.rename`

```json
{ "id": "fan0", "name": "Ventilador Sala" }
```

- `name`: 1..32 caracteres (UTF-8), sem vazio.
- Renomeia **apenas o nome de exibição no hub**. Não chama o device e não
  incrementa o `manifestVersion`: o nome no firmware é independente e só muda
  pela UI local dele (`POST /api/fan/rename` no próprio device).
- O hub guarda o nome do firmware em `firmware_name` (vindo do manifesto) para
  exibição quando os dois divergirem.

### `config.get`

Resposta em `result.config`:

```json
{
  "version": 1,
  "fans": [
    {
      "id": "fan0",
      "name": "Ventilador Sala",
      "state": {
        "power": true, "light": false, "speed": 2, "lastSpeed": 2, "direction": "forward",
        "counter": 3, "updatedAt": 1762286400, "source": "hub"
      },
      "commands": {
        "power": { "data": [0, 10, 116, 3], "offset": 0 },
        "vel_2": { "data": [0, 10, 123, 18], "offset": 2 }
      }
    }
  ],
  "hub": { "url": "http://hub.local", "configured": true, "reachable": true }
}
```

- `commands` só contém os botões já aprendidos.
- `state.lastSpeed` é opcional no `config.set`; preserva a última velocidade
  não nula para religar via controle físico.
- O bloco `hub` informa (e, no `config.set`, ajusta) o push para o hub. Não
  inclui token, Wi-Fi nem segredos.

### `config.set`

Substitui a configuração inteira (não faz merge por seção), valida e persiste no
NVS.

- `state` é restaurado **sem RF** (equivalente a `state.correct`).
- Remove comandos aprendidos ausentes; `id` é posicional (`fan0`..`fanN-1`).
- Incrementa o `manifestVersion`, empurra todos os estados e emite
  `config.changed`.

### `reboot`

Reinicia o device em ~500 ms.

## Códigos de erro

| Código | Quando |
|---|---|
| `invalid_payload` | JSON inválido ou campo obrigatório ausente |
| `unknown_device` | `id` não existe no manifesto atual |
| `busy` | já existe OTA, aprendizado ou transmissão RF em andamento |
| `not_supported` | ação não declarada nas capabilities/actions |
| `invalid_config` | objeto `config` ausente ou sem `fans` |
| `invalid_fan_count` | `fans` vazio ou com mais de 8 itens |
| `invalid_sha256` | `POST /api/update` sem 64 dígitos hexadecimais |
| `rf_timeout` | nenhum frame captado no aprendizado |
| `rf_unavailable` | CC1101 ausente ou rádio indisponível |
| `cancelled` | aprendizado cancelado |
| `fan_limit` | já existem 8 ventiladores |
| `last_fan` | tentativa de remover o único ventilador |
| `invalid_name` | nome vazio ou maior que 32 caracteres |
| `ota_failed` | falha ao gravar a partição OTA |
| `persist_failed` | falha ao gravar no NVS |

## Eventos (device → hub)

Eventos assíncronos vão em `POST /api/things/<thing>/events` (ver
[`hub-ingest.md`](hub-ingest.md)); são best-effort.

| Evento | Campos |
|---|---|
| `learn.finished` | `id` (o do sub-device), `command`, `ok`, `frame?`, `error?` |
| `ota.progress` | `version?`, `percent` (0..100) |
| `ota.done` | `ok`, `version`, `error?` (`{code,message}`) |
| `config.changed` | `version` (manifestVersion) |
| `state.failed` | `id`, `error` (`{code,message}`) |
