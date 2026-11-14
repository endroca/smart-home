#include "hub_client.h"

#include <HTTPClient.h>

#include "config.h"
#include "manifest.h"
#include "state.h"
#include "storage.h"
#include "system.h"
#include "wifi_provision.h"

static const char *HUB_URL_KEY = "hub_url";

static String hubUrl;
static uint32_t okCount = 0;
static uint32_t failCount = 0;
static uint32_t lastOkAt = 0;
static uint32_t nextAttemptAt = 0;
static uint32_t backoff = HUB_RETRY_MS;
static uint32_t lastHeartbeatAt = 0;
static String lastError;

// Estado pendente por ventilador: o estado é absoluto, então guardar um flag
// por slot já coalesce várias mudanças em um único POST.
static bool stateDirty[MAX_FANS];
static bool statusPending = false;
static String eventQueue[HUB_EVENT_QUEUE];
static uint8_t eventHead = 0;
static uint8_t eventCount = 0;
static bool wifiWasConnected = false;

static String endpoint() {
  String base = hubUrl;
  if (base.endsWith("/")) base.remove(base.length() - 1);
  return base + "/api/things/" + THING_NAME + "/events";
}

void hub_client_begin() {
  hubUrl = storage_getString(HUB_URL_KEY, "");
  for (uint8_t i = 0; i < MAX_FANS; i++) stateDirty[i] = false;
  statusPending = true; // o boot publica o estado assim que o Wi-Fi subir
  nextAttemptAt = 0;
  backoff = HUB_RETRY_MS;
  if (hub_client_configured()) {
    Serial.printf("[hub] push para %s\n", hubUrl.c_str());
    if (storage_token_hash_hex().isEmpty()) {
      Serial.println("[hub] sem token local: o hub vai recusar o push");
    }
  } else {
    Serial.println("[hub] push desligado (sem hub_url)");
  }
}

bool hub_client_configured() { return !hubUrl.isEmpty(); }

String hub_client_url() { return hubUrl; }

void hub_client_set_url(const String &url) {
  String clean = url;
  clean.trim();
  // Idempotente: o hub reaplica a URL periodicamente e isso não pode gerar
  // gravação na NVS nem reenfileirar o estado a cada tentativa.
  if (clean == hubUrl) return;
  hubUrl = clean;
  if (clean.isEmpty()) {
    storage_remove(HUB_URL_KEY);
  } else {
    storage_putString(HUB_URL_KEY, clean);
  }
  backoff = HUB_RETRY_MS;
  nextAttemptAt = 0;
  statusPending = true;
  Serial.printf("[hub] url=%s\n", hubUrl.c_str());
}

void hub_client_clear_url() { hub_client_set_url(""); }

bool hub_client_reachable() {
  return hub_client_configured() && lastOkAt != 0 &&
         (millis() - lastOkAt) < 3 * HUB_HEARTBEAT_MS;
}

// hub_client_pending reports whether there is anything waiting to be pushed.
bool hub_client_pending() {
  if (statusPending || eventCount > 0) return true;
  for (uint8_t i = 0; i < state_fanCount(); i++) {
    if (stateDirty[i]) return true;
  }
  return false;
}

uint32_t hub_client_ok_count() { return okCount; }
uint32_t hub_client_fail_count() { return failCount; }
String hub_client_last_error() { return lastError; }

void hub_client_fill_state(JsonObject target, uint8_t fan) {
  const FanState &st = state_get(fan);
  target["power"] = st.power;
  target["light"] = st.light;
  target["speed"] = st.speed;
  target["direction"] = state_directionName(st.direction);
  target["source"] = state_sourceName(st.source);
  target["updatedAt"] = st.updatedAt;
  target["counter"] = st.counter;
}

void hub_client_fill_status(JsonObject target) {
  target["configured"] = hub_client_configured();
  target["url"] = hubUrl;
  target["reachable"] = hub_client_reachable();
  target["pending"] = hub_client_pending();
  target["ok"] = okCount;
  target["fail"] = failCount;
  target["manifestVersion"] = manifest_version();
  if (!lastError.isEmpty()) target["lastError"] = lastError;
}

void hub_client_queue_state(uint8_t fan) {
  if (!state_validFan(fan)) return;
  stateDirty[fan] = true;
}

void hub_client_queue_all_states() {
  for (uint8_t i = 0; i < state_fanCount(); i++) stateDirty[i] = true;
}

void hub_client_queue_status() { statusPending = true; }

void hub_client_queue_event(const String &eventJson) {
  if (eventJson.isEmpty()) return;
  if (!hub_client_configured()) return;
  if (eventCount >= HUB_EVENT_QUEUE) {
    // Fila cheia: descarta o mais antigo. Eventos são best-effort; o estado
    // absoluto e o heartbeat são o que realmente importa.
    eventHead = (uint8_t)((eventHead + 1) % HUB_EVENT_QUEUE);
    eventCount--;
  }
  uint8_t slot = (uint8_t)((eventHead + eventCount) % HUB_EVENT_QUEUE);
  eventQueue[slot] = eventJson;
  eventCount++;
}

void hub_client_loop() {
  if (!hub_client_configured()) return;
  if (!wifi_connected()) {
    wifiWasConnected = false;
    return;
  }
  if (!wifiWasConnected) {
    // Primeira conexão (ou reconexão): o hub pode ter reiniciado sem nós.
    wifiWasConnected = true;
    hub_client_queue_all_states();
    statusPending = true;
    nextAttemptAt = 0;
    backoff = HUB_RETRY_MS;
  }

  uint32_t now = millis();
  if (lastHeartbeatAt == 0) lastHeartbeatAt = now;
  if (now - lastHeartbeatAt >= HUB_HEARTBEAT_MS) {
    lastHeartbeatAt = now;
    statusPending = true;
  }

  // Nada a enviar?
  bool anyState = false;
  for (uint8_t i = 0; i < state_fanCount(); i++) {
    if (stateDirty[i]) {
      anyState = true;
      break;
    }
  }
  if (!anyState && !statusPending && eventCount == 0) return;
  if ((int32_t)(now - nextAttemptAt) < 0) return;

  JsonDocument doc;
  uint8_t fanToClear = 255;
  bool sendingStatus = false;
  bool sendingEvent = false;

  if (anyState) {
    for (uint8_t i = 0; i < state_fanCount(); i++) {
      if (!stateDirty[i]) continue;
      doc["type"] = "state";
      doc["id"] = state_deviceId(i);
      JsonObject reported = doc["reported"].to<JsonObject>();
      hub_client_fill_state(reported, i);
      fanToClear = i;
      break;
    }
  } else if (eventCount > 0) {
    JsonDocument event;
    if (deserializeJson(event, eventQueue[eventHead]) != DeserializationError::Ok) {
      // Evento corrompido: descarta e tenta o próximo no ciclo seguinte.
      eventHead = (uint8_t)((eventHead + 1) % HUB_EVENT_QUEUE);
      eventCount--;
      return;
    }
    doc["type"] = "event";
    doc["event"].set(event.as<JsonVariantConst>());
    sendingEvent = true;
  } else {
    doc["type"] = "status";
    JsonObject status = doc["status"].to<JsonObject>();
    status["thing"] = THING_NAME;
    status["version"] = FIRMWARE_VERSION;
    status["ts"] = system_now_epoch();
    status["ip"] = wifi_ip();
    status["rssi"] = wifi_rssi();
    status["heap"] = system_free_heap();
    status["uptime"] = system_uptime_seconds();
    status["manifestVersion"] = manifest_version();
    hub_client_fill_status(status["hub"].to<JsonObject>());
    sendingStatus = true;
  }

  String payload;
  serializeJson(doc, payload);

  HTTPClient http;
  http.setConnectTimeout(HUB_PUSH_TIMEOUT_MS);
  http.setTimeout(HUB_PUSH_TIMEOUT_MS);
  http.setReuse(false);
  if (!http.begin(endpoint())) {
    lastError = "begin_failed";
    failCount++;
    backoff = backoff < HUB_MAX_BACKOFF_MS ? backoff * 2 : HUB_MAX_BACKOFF_MS;
    nextAttemptAt = now + backoff;
    return;
  }
  http.addHeader("Content-Type", "application/json");
  String auth = storage_token_hash_hex();
  if (!auth.isEmpty()) http.addHeader("Authorization", "Bearer " + auth);

  int code = http.POST(payload);
  http.end();

  if (code >= 200 && code < 300) {
    okCount++;
    lastOkAt = millis();
    backoff = HUB_RETRY_MS;
    nextAttemptAt = 0;
    lastError = "";
    if (fanToClear != 255) stateDirty[fanToClear] = false;
    if (sendingStatus) statusPending = false;
    if (sendingEvent) {
      eventHead = (uint8_t)((eventHead + 1) % HUB_EVENT_QUEUE);
      eventCount--;
    }
    return;
  }

  failCount++;
  lastError = String("http ") + String(code);
  backoff = backoff < HUB_MAX_BACKOFF_MS ? backoff * 2 : HUB_MAX_BACKOFF_MS;
  nextAttemptAt = millis() + backoff;
  Serial.printf("[hub] push falhou: %s (backoff %ums)\n", lastError.c_str(),
                (unsigned)backoff);
}
