# OTA

Dois caminhos, ambos por HTTP na LAN:

1. **Pelo hub (primário):** o hub envia o `.bin` por upload multipart para
   `POST /api/update`.
2. **Pelo formulário local (resgate):** o mesmo endpoint, pela página do device.

O SHA-256 é obrigatório nos dois caminhos e verificado em streaming antes de
aplicar.

## Build

- Partition scheme com **duas** slots OTA (`app0`, `app1`) + `otadata`.
- O firmware publica `version` no `manifest` e no `status`.
- O `.bin` gerado é copiado para o diretório de firmware do hub, por exemplo
  `/opt/smart-home-hub/firmware/smart_control-1.1.0.bin`. O nome do arquivo
  carrega a versão (`<nome>-<versao>.bin`); é dela que o hub extrai a versão.
  Além da cópia manual, a UI envia o `.bin` para o hub por
  `POST /api/firmware` (multipart, campo `file`): o hub valida o nome
  (`^[A-Za-z0-9._-]+\.bin$`), limita o corpo (~8 MB) e grava de forma atômica
  (temporário + rename). `DELETE /api/firmware/<name>` remove um arquivo.

## Caminho 1 — hub

1. O hub lista os `*.bin` do diretório, escolhe o arquivo e calcula o `sha256`.
2. Faz `POST` multipart para o device:

```
POST http://<device>:80/api/update?sha256=9f2c…
Authorization: Bearer <token>
Content-Type: multipart/form-data

<form field "firmware" = bytes do .bin>
```

3. O device valida o header do binário, o `sha256` e o tamanho, grava na
   partição inativa e reinicia.
4. O hub confirma o sucesso quando a **nova versão aparece** no `status` (o
   heartbeat) ou no `manifest`. Se ela não aparecer em 10 min, o hub tenta
   reinstalar o binário anterior conhecido (rollback simples, uma tentativa).

Não há `ota.install` por URL nem comando assíncrono: o upload **é** a operação.
O hub usa um timeout longo para essa requisição (o `.bin` tem ~1,5 MB).

### Acompanhamento

O device também **empurra** eventos ao hub durante e depois do update
(`ota.progress`, `ota.done`), para a UI acompanhar sem polling:

```json
{ "event": "ota.progress", "version": "1.1.0", "percent": 42 }
{ "event": "ota.done", "ok": true, "version": "1.1.0" }
```

Se o update falhar, `ota.done` vem com `ok: false` e
`error: { "code": "…", "message": "…" }`, e o device continua na versão anterior.

| `error.code` | causa |
| --- | --- |
| `invalid_sha256` | `?sha256=` ausente ou não hexadecimal |
| `sha_mismatch` | SHA-256 do corpo recebido difere do esperado |
| `ota_failed` | falha ao gravar/validar a partição OTA (`Update`) |

## Caminho 2 — formulário local

Na página do device (`http://<mdns>/`), a aba **Firmware** envia o `.bin` para
`POST /api/update` (multipart, autenticado com o device token). A página calcula
o SHA-256 no navegador (WebCrypto ou implementação JS pura) e envia em
`?sha256=...`.

É o caminho de resgate quando o hub está fora ou sem o arquivo.

## Regras

- Uma atualização por vez (`409`/`busy` se já houver OTA em andamento).
- OTA e transmissão RF não rodam ao mesmo tempo; o device entra em modo seguro
  durante o update.
- Comandos de estado são rejeitados durante o OTA.
- O diretório de firmware do hub é servido apenas na LAN (`/firmware/<file>.bin`)
  e não é exposto fora dela.
- Não há rollout de frota, canary ou jobs: o hub instala em um thing por vez.
