# Ingest do hub (device → hub)

O device é a **fonte de verdade** do estado dos seus sub-devices. Ele empurra o
que acontece para o hub por HTTP:

```
POST http://<hub>/api/things/<thing>/events
Authorization: Bearer <sha256(token) em hex>
Content-Type: application/json
```

O hub espelha o que recebe: atualiza o cache em memória, a réplica durável
(`thing_states` / `thing_status`) e publica no SSE para a UI. **Nada** do que o
hub manda entra aqui — este canal é só device → hub.

## Autenticação

O device guarda apenas o **digest SHA-256** do token na NVS, então ele nunca
transmite o texto plano. Ele envia o digest em hex (64 caracteres), e o hub o
confere contra o digest do token que já mantém para aquele gateway.

Por conveniência de operação (testar com `curl`), o hub também aceita o token em
texto plano. Ambos são comparados em tempo constante.

Sem token cadastrado para o `thing`, ou com `thing` desconhecido, a resposta é
`401 unauthorized`.

## Corpos

### `state`

Sempre que um sub-device muda. O estado é **absoluto**: o campo `reported`
carrega o estado inteiro daquele sub-device, não um diff.

```json
{
  "type": "state",
  "id": "fan0",
  "reported": {
    "power": true,
    "light": false,
    "speed": 3,
    "direction": "forward",
    "source": "rf_remote",
    "updatedAt": 1762286400,
    "counter": 4
  }
}
```

`source` identifica quem originou a mudança (`boot`, `hub`, `http`,
`rf_remote`, `correction`). Valores antigos persistidos no device são
normalizados para `hub`. `updatedAt` é o epoch do device e pode vir `0`
quando ele não sincronizou o relógio — o hub não depende dele para ordenar.

### `status`

Heartbeat e diagnóstico. Receber um `status` prova que o device está alcançável,
então o hub **força** `online: true`; o corpo não decide presença.

```json
{
  "type": "status",
  "status": {
    "version": "1.0.0",
    "ts": 1762286400,
    "ip": "192.168.1.42",
    "rssi": -61,
    "heap": 142000,
    "uptime": 12345,
    "manifestVersion": 4
  }
}
```

O `manifestVersion` é a chave de invalidação da descoberta: quando ele difere do
que o hub tem em cache, o hub relê `GET /api/manifest` (com throttle) em vez de
revalidar num timer.

### `event`

Eventos assíncronos. São best-effort: se a fila do device encher, os mais antigos
são descartados.

```json
{
  "type": "event",
  "event": {
    "event": "learn.finished",
    "id": "opcional-para-correlacionar",
    "id": "fan0",
    "command": "vel_3",
    "ok": true,
    "frame": "1A2B3C4D"
  }
}
```

Eventos definidos: `learn.finished`, `config.changed`, `ota.progress`, `ota.done`
e `state.failed`.

## Respostas

| Código | Quando |
|---|---|
| `200 {"ok":true}` | aplicado |
| `400 invalid_payload` | `type` fora de `state`/`status`/`event`, ou corpo sem os campos obrigatórios |
| `401 unauthorized` | token ausente/inválido, ou gateway sem token cadastrado |

## Regras de consistência

- **Um escritor por dado.** O hub nunca inventa estado: `state` só muda o
  `reported` do device.
- **A réplica é do que o device reportou.** O hub grava no SQLite exatamente o
  que chegou, mais o carimbo de recebimento (confiável, do hub) ao lado do
  carimbo do device.
- **No boot, estado restaurado é marcado como `stale`** até o device confirmar.
- **Heartbeat não escreve sempre.** O hub só persiste o status quando algo
  relevante muda (online/offline, IP, versão, estado do OTA), para poupar o
  cartão SD do hub.
