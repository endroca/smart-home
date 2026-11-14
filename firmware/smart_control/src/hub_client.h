#pragma once

#include <Arduino.h>
#include <ArduinoJson.h>

// Cliente HTTP que empurra o estado do device para o hub.
//
// O hub é quem manda comandos para o device (POST /api/state, /api/fan/*, ...).
// No sentido inverso, o device é a fonte de verdade: ele publica o que
// aconteceu (mudança de estado, status/heartbeat e eventos assíncronos) em
//
//   POST <hub_url>/api/things/<thing>/events
//   Authorization: Bearer <sha256(token)>   (hex)
//
// O token nunca sai do device em texto plano: enviamos o digest, e o hub o
// confere contra o hash do token que já guarda para este thing.
//
// O envio é feito no loop(), nunca dentro de um handler HTTP, e a fila é
// coalescida por ventilador: o estado é absoluto, então só o último importa.

void hub_client_begin();
void hub_client_loop();

// Configuração: URL do hub (persistida na NVS). Sem URL, o push fica desligado
// e o device segue funcionando só com a API local.
bool hub_client_configured();
String hub_client_url();
void hub_client_set_url(const String &url);
void hub_client_clear_url();

// Enfileira um push. Barato e não bloqueante.
void hub_client_queue_state(uint8_t fan);
void hub_client_queue_all_states();
void hub_client_queue_status();
void hub_client_queue_event(const String &eventJson);

// Diagnóstico.
bool hub_client_reachable();
bool hub_client_pending();
uint32_t hub_client_ok_count();
uint32_t hub_client_fail_count();
String hub_client_last_error();

// Monta o objeto de estado de um ventilador (mesmo formato do ingest).
void hub_client_fill_state(JsonObject target, uint8_t fan);

// Monta o bloco de diagnóstico do link com o hub (usado no /api/diag, no
// heartbeat e na resposta de POST /api/hub).
void hub_client_fill_status(JsonObject target);
