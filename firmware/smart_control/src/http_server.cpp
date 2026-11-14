#include "http_server.h"

#include <ArduinoJson.h>
#include <Update.h>
#include <WiFi.h>
#include <WebServer.h>
#include <mbedtls/sha256.h>

#include "config.h"
#include "hub_client.h"
#include "manifest.h"

#include "ota.h"
#include "rf_ventisol.h"
#include "state.h"
#include "storage.h"
#include "system.h"
#include "web_ui.h"
#include "web_ui_css.h"
#include "wifi_provision.h"

static WebServer server(HTTP_PORT);

static bool uploadStarted = false;
static bool uploadAuthOk = false;
static bool uploadBusy = false;
static bool uploadSuccess = false;
static String uploadError;

static mbedtls_sha256_context uploadShaCtx;
static bool uploadShaActive = false;
static bool uploadShaExpected = false;
static String uploadExpectedSha;

static bool validSha(const String &sha) {
  if (sha.length() != 64) return false;
  for (size_t i = 0; i < sha.length(); i++) {
    if (!isxdigit((unsigned char)sha[i])) return false;
  }
  return true;
}

static bool authOk() {
  if (!provision_token_set()) return false;
  String header = server.header("Authorization");
  if (!header.startsWith("Bearer ")) return false;
  String token = header.substring(7);
  token.trim();
  return provision_check_token(token);
}

static void send401() {
  server.send(401, "application/json",
              "{\"ok\":false,\"error\":{\"code\":\"unauthorized\",\"message\":"
              "\"token invalido\"}}");
}

static bool requireAuth() {
  if (authOk()) return true;
  send401();
  return false;
}

static void sendJson(JsonDocument &doc) {
  String out;
  serializeJson(doc, out);
  server.send(200, "application/json", out);
}

static void sendError(int status, const String &code, const String &message) {
  JsonDocument doc;
  doc["ok"] = false;
  JsonObject error = doc["error"].to<JsonObject>();
  error["code"] = code;
  error["message"] = message.isEmpty() ? code : message;
  String out;
  serializeJson(doc, out);
  server.send(status, "application/json", out);
}

static int statusForError(const String &code) {
  if (code == "invalid_device" || code == "unknown_device") return 404;
  if (code == "busy") return 409;
  if (code == "invalid_payload" || code == "invalid_name" ||
      code == "invalid_config" || code == "invalid_fan_count") {
    return 400;
  }
  if (code == "fan_limit" || code == "last_fan") return 409;
  return 500;
}

static bool readBody(JsonDocument &doc) {
  String body = server.arg("plain");
  if (body.isEmpty()) return false;
  return !deserializeJson(doc, body);
}

static void fillState(JsonObject state, const FanState &st) {
  state["power"] = st.power;
  state["light"] = st.light;
  state["speed"] = st.speed;
  state["direction"] = state_directionName(st.direction);
}

static void fillFan(JsonObject fan, uint8_t index) {
  const FanConfig &cfg = state_config(index);
  const FanState &st = state_get(index);
  fan["id"] = state_deviceId(index);
  fan["name"] = cfg.name;
  JsonObject state = fan["state"].to<JsonObject>();
  fillState(state, st);
  fan["source"] = state_sourceName(st.source);
  fan["updatedAt"] = st.updatedAt;
  fan["counter"] = st.counter;
}

static void addTransmitted(JsonObject root, uint16_t mask) {
  JsonArray transmitted = root["transmitted"].to<JsonArray>();
  for (uint8_t command = 0; command < COMMAND_COUNT; command++) {
    if (mask & (1u << command)) transmitted.add(state_commandName(command));
  }
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

static void handleRoot() {
  server.sendHeader("Cache-Control", "no-store, max-age=0");
  server.send_P(200, "text/html", INDEX_HTML);
}

static void handleCss() {
  server.sendHeader("Cache-Control", "no-cache");
  server.send_P(200, "text/css", INDEX_CSS);
}

static void handleHealth() {
  JsonDocument doc;
  doc["ok"] = true;
  doc["version"] = FIRMWARE_VERSION;
  doc["build"] = __DATE__ " " __TIME__;
  doc["thing"] = THING_NAME;
  doc["uptime"] = system_uptime_seconds();
  doc["heap"] = system_free_heap();
  doc["rssi"] = wifi_rssi();
  doc["hub"] = hub_client_configured()
                   ? (hub_client_reachable() ? "connected" : "pending")
                   : "disabled";
  doc["apMode"] = wifi_ap_active();
  doc["provisioned"] = wifi_config_set();
  doc["tokenSet"] = provision_token_set();
  doc["apSsid"] = wifi_ap_ssid();
  doc["ip"] = wifi_ip();
  doc["time"] = system_now_epoch();
  doc["rf"] = rf_radioOk();
  sendJson(doc);
}

static void handleManifest() {
  // O manifesto é o mesmo documento que o device empurra/expõe ao hub.
  if (!requireAuth()) return;
  JsonDocument doc;
  manifest_build(doc);
  sendJson(doc);
}

static void handleStateGet() {
  if (!requireAuth()) return;

  JsonDocument doc;
  doc["ok"] = true;
  doc["pendingSync"] = hub_client_pending();
  JsonArray fans = doc["fans"].to<JsonArray>();
  for (uint8_t i = 0; i < state_fanCount(); i++) {
    JsonObject fan = fans.add<JsonObject>();
    fillFan(fan, i);
  }
  sendJson(doc);
}

static void handleStatePost() {
  if (!requireAuth()) return;

  if (ota_busy() || rf_isBusy()) {
    sendError(409, "busy", "outra operacao esta em andamento");
    return;
  }

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  String deviceId = doc["id"] | "";
  uint8_t fan = state_findById(deviceId);
  if (fan == 255) {
    sendError(404, "unknown_device", "device nao existe");
    return;
  }

  JsonObject desired = doc["state"].as<JsonObject>();
  FanTarget target;
  if (desired.isNull() || !state_parseTarget(desired, target)) {
    sendError(400, "invalid_payload", "state invalido");
    return;
  }

  String error;
  uint16_t mask = 0;
  bool force = doc["force"] | false;
  if (!state_apply(fan, target, FanSource::HTTP, error, &mask, force)) {
    sendError(statusForError(error), error, error);
    return;
  }

  JsonDocument result;
  result["ok"] = true;
  JsonObject state = result["state"].to<JsonObject>();
  fillState(state, state_get(fan));
  addTransmitted(result.as<JsonObject>(), mask);
  sendJson(result);
}

static void handleCorrect() {
  if (!requireAuth()) return;
  if (ota_busy()) {
    sendError(409, "busy", "atualizacao em andamento");
    return;
  }

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  String deviceId = doc["id"] | "";
  uint8_t fan = state_findById(deviceId);
  if (fan == 255) {
    sendError(404, "unknown_device", "device nao existe");
    return;
  }

  JsonObject desired = doc["state"].as<JsonObject>();
  FanTarget target;
  if (desired.isNull() || !state_parseTarget(desired, target)) {
    sendError(400, "invalid_payload", "state invalido");
    return;
  }

  String error;
  if (!state_correct(fan, target, error)) {
    sendError(statusForError(error), error, error);
    return;
  }

  JsonDocument result;
  result["ok"] = true;
  JsonObject state = result["state"].to<JsonObject>();
  fillState(state, state_get(fan));
  sendJson(result);
}

static void handleLearnStart() {
  if (!requireAuth()) return;

  if (ota_busy()) {
    sendError(409, "busy", "outra operacao esta em andamento");
    return;
  }

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  String deviceId = doc["id"] | "";
  uint8_t fan = state_findById(deviceId);
  if (fan == 255) {
    sendError(404, "unknown_device", "device nao existe");
    return;
  }

  String commandName = doc["command"] | "";
  int command = state_commandFromName(commandName);
  if (command < 0) {
    sendError(400, "invalid_payload", "comando desconhecido");
    return;
  }

  String id = doc["id"] | "";
  if (id.isEmpty()) id = String("http-") + String(millis());

  String error;
  if (!rf_startLearn(fan, (uint8_t)command, id, error)) {
    sendError(statusForError(error), error, error);
    return;
  }

  JsonDocument result;
  result["ok"] = true;
  result["state"] = "listening";
  result["id"] = id;
  sendJson(result);
}

static void handleLearnStatus() {
  if (!requireAuth()) return;

  LearnStatus learn = rf_learnStatus();
  const char *result = "idle";
  if (learn.active) {
    result = "listening";
  } else if (learn.finished) {
    if (learn.success) {
      result = "ok";
    } else if (learn.cancelled) {
      result = "cancelled";
    } else {
      result = "error";
    }
  }

  JsonDocument doc;
  doc["ok"] = true;
  doc["active"] = learn.active;
  doc["result"] = result;
  doc["id"] = state_deviceId(learn.fan);
  doc["command"] = state_commandName(learn.command);
  doc["requestId"] = learn.requestId;
  doc["startedAt"] = learn.startedAt;
  if (learn.success) doc["frame"] = learn.frameHex;
  if (strcmp(result, "error") == 0) doc["error"] = "rf_timeout";
  if (strcmp(result, "cancelled") == 0) doc["error"] = "cancelled";
  sendJson(doc);
}

static void handleLearnCancel() {
  if (!requireAuth()) return;
  rf_cancelLearn();
  JsonDocument doc;
  doc["ok"] = true;
  sendJson(doc);
}

static void handleConfigGet() {
  if (!requireAuth()) return;
  JsonDocument doc;
  JsonObject root = doc.to<JsonObject>();
  if (server.hasArg("format") && server.arg("format") == "legacy") {
    state_configToLegacyJson(root);
  } else {
    state_configToJson(root);
  }
  // O push para o hub é configuração do device, não dos ventiladores.
  JsonObject hub = root["hub"].to<JsonObject>();
  hub["url"] = hub_client_url();
  hub["configured"] = hub_client_configured();
  hub["reachable"] = hub_client_reachable();
  hub["ok"] = hub_client_ok_count();
  hub["fail"] = hub_client_fail_count();
  if (!hub_client_last_error().isEmpty()) {
    hub["lastError"] = hub_client_last_error();
  }
  sendJson(doc);
}

static void handleConfigExport() {
  if (!requireAuth()) return;
  JsonDocument doc;
  JsonObject root = doc.to<JsonObject>();
  state_configToLegacyJson(root);
  server.sendHeader("Content-Disposition",
                    "attachment; filename=ventisol-config.json");
  sendJson(doc);
}

static void handleConfigPost() {
  if (!requireAuth()) return;

  if (ota_busy() || rf_isBusy()) {
    sendError(409, "busy", "outra operacao esta em andamento");
    return;
  }

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  JsonObjectConst config = doc["config"].is<JsonObject>()
                               ? doc["config"].as<JsonObjectConst>()
                               : doc.as<JsonObjectConst>();

  // O hub pode vir junto (fora do objeto de ventiladores) e é opcional.
  String hubUrl;
  if (doc["hub"].is<JsonObject>()) {
    hubUrl = doc["hub"]["url"].as<String>();
  } else if (doc["hub_url"].is<const char *>()) {
    hubUrl = doc["hub_url"].as<String>();
  }

  String error;
  if (!state_configFromJson(config, error)) {
    sendError(statusForError(error), error, error);
    return;
  }
  if (doc["hub"].is<JsonObject>() || doc["hub_url"].is<const char *>()) {
    hub_client_set_url(hubUrl);
  }

  manifest_bump();
  hub_client_queue_all_states();
  hub_client_queue_status();

  JsonDocument event;
  event["event"] = "config.changed";
  event["version"] = manifest_version();
  String payload;
  serializeJson(event, payload);
  hub_client_queue_event(payload);

  JsonDocument result;
  result["ok"] = true;
  sendJson(result);
}

static void handleHubPost() {
  if (!requireAuth()) return;

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  String url;
  if (doc["hub"].is<JsonObject>()) {
    url = doc["hub"]["url"].as<String>();
  } else {
    url = doc["url"].as<String>();
  }
  url.trim();
  if (!url.isEmpty() && !url.startsWith("http://") && !url.startsWith("https://")) {
    sendError(400, "invalid_payload", "url deve comecar com http:// ou https://");
    return;
  }
  while (url.endsWith("/")) url.remove(url.length() - 1);

  hub_client_set_url(url);
  // Reaplica a URL é barato e garante que o hub receba o estado e o status
  // atuais logo depois de (re)configurar o push — inclusive após um restart.
  hub_client_queue_all_states();
  hub_client_queue_status();

  JsonDocument result;
  result["ok"] = true;
  hub_client_fill_status(result["hub"].to<JsonObject>());
  sendJson(result);
}

static void handleDiag() {
  if (!requireAuth()) return;

  JsonDocument doc;
  doc["ok"] = true;

  JsonObject rf = doc["rf"].to<JsonObject>();
  rf["radio"] = rf_radioOk();
  rf["txCount"] = rf_txCount();
  rf["rxCount"] = rf_rxCount();
  rf["rxChecksumFail"] = rf_rxChecksumFail();
  rf["rxUnmatched"] = rf_rxUnmatched();
  rf["rxMatched"] = rf_rxMatched();
  rf["lastFrame"] = rf_lastFrameHex();
  rf["lastFrameAt"] = rf_lastFrameAt();
  rf["learning"] = rf_isLearning();
  rf["busy"] = rf_isBusy();
  rf["txGuard"] = rf_txGuardActive();

  JsonObject hub = doc["hub"].to<JsonObject>();
  hub_client_fill_status(hub);

  JsonObject nvs = doc["nvs"].to<JsonObject>();
  nvs["freeEntries"] = storage_free_entries();

  JsonObject system = doc["system"].to<JsonObject>();
  system["heap"] = system_free_heap();
  system["uptime"] = system_uptime_seconds();
  system["time"] = system_now_epoch();
  system["rssi"] = wifi_rssi();

  sendJson(doc);
}

static void handleWifi() {
  if (!requireAuth()) return;
  if (ota_busy() || rf_isBusy()) {
    sendError(409, "busy", "outra operacao esta em andamento");
    return;
  }

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  String ssid = doc["ssid"] | "";
  String password = doc["password"] | "";
  ssid.trim();
  if (ssid.isEmpty() || ssid.length() > 32) {
    sendError(400, "invalid_payload", "ssid invalido");
    return;
  }

  wifi_save(ssid, password);

  JsonDocument result;
  result["ok"] = true;
  sendJson(result);
  system_restart_in(1500);
}

static void handleFanAdd() {
  if (!requireAuth()) return;
  if (ota_busy()) {
    sendError(409, "busy", "atualizacao em andamento");
    return;
  }

  uint8_t newFan = 0;
  String error;
  if (!state_addFan(newFan, error)) {
    sendError(statusForError(error), error, error);
    return;
  }

  manifest_bump();
  hub_client_queue_state(newFan);
  hub_client_queue_status();

  JsonDocument result;
  result["ok"] = true;
  result["id"] = state_deviceId(newFan);
  sendJson(result);
}

static void handleFanRemove() {
  if (!requireAuth()) return;
  if (ota_busy() || rf_isBusy()) {
    sendError(409, "busy", "outra operacao esta em andamento");
    return;
  }

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  uint8_t fan = state_findById(doc["id"] | "");
  if (fan == 255) {
    sendError(404, "unknown_device", "device nao existe");
    return;
  }

  String error;
  if (!state_removeFan(fan, error)) {
    sendError(statusForError(error), error, error);
    return;
  }

  manifest_bump();
  hub_client_queue_all_states();
  hub_client_queue_status();

  JsonDocument result;
  result["ok"] = true;
  sendJson(result);
}

static void handleFanRename() {
  if (!requireAuth()) return;
  if (ota_busy()) {
    sendError(409, "busy", "atualizacao em andamento");
    return;
  }

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  uint8_t fan = state_findById(doc["id"] | "");
  if (fan == 255) {
    sendError(404, "unknown_device", "device nao existe");
    return;
  }

  String name = doc["name"] | "";
  String error;
  if (!state_renameFan(fan, name, error)) {
    sendError(statusForError(error), error, error);
    return;
  }

  manifest_bump();
  hub_client_queue_status();

  JsonDocument result;
  result["ok"] = true;
  sendJson(result);
}

static void handleReboot() {
  if (!requireAuth()) return;
  if (ota_busy() || rf_isBusy()) {
    sendError(409, "busy", "outra operacao esta em andamento");
    return;
  }
  JsonDocument result;
  result["ok"] = true;
  sendJson(result);
  system_restart_in(500);
}

static void handleScan() {
  if (!wifi_ap_active()) {
    sendError(409, "not_in_ap", "scan disponivel apenas em modo AP");
    return;
  }
  if (ota_busy()) {
    sendError(409, "busy", "atualizacao em andamento");
    return;
  }

  int count = WiFi.scanNetworks();
  JsonDocument doc;
  doc["ok"] = true;
  JsonArray networks = doc["networks"].to<JsonArray>();
  for (int i = 0; i < count && i < 24; i++) {
    JsonObject network = networks.add<JsonObject>();
    network["ssid"] = WiFi.SSID(i);
    network["rssi"] = WiFi.RSSI(i);
    network["secure"] = WiFi.encryptionType(i) != WIFI_AUTH_OPEN;
  }
  WiFi.scanDelete();
  sendJson(doc);
}

static void handleProvision() {
  if (!wifi_ap_active()) {
    sendError(409, "not_in_ap", "provisionamento disponivel apenas em modo AP");
    return;
  }
  if (ota_busy() || rf_isBusy()) {
    sendError(409, "busy", "outra operacao esta em andamento");
    return;
  }

  JsonDocument doc;
  if (!readBody(doc)) {
    sendError(400, "invalid_payload", "json invalido");
    return;
  }

  String ssid = doc["ssid"] | "";
  String password = doc["password"] | "";
  String token = doc["token"] | "";
  ssid.trim();
  token.trim();

  if (ssid.isEmpty() || ssid.length() > 32) {
    sendError(400, "invalid_payload", "ssid invalido");
    return;
  }
  if (token.length() < TOKEN_MIN_LEN) {
    sendError(400, "invalid_payload", "token muito curto");
    return;
  }

  wifi_save(ssid, password);
  provision_set_token(token);

  JsonDocument result;
  result["ok"] = true;
  sendJson(result);
  system_restart_in(1500);
}

static void handleProvisionStatus() {
  JsonDocument doc;
  doc["ok"] = true;
  doc["configured"] = wifi_config_set();
  doc["connecting"] = wifi_config_set() && !wifi_connected();
  doc["apMode"] = wifi_ap_active();
  doc["apSsid"] = wifi_ap_ssid();
  doc["ip"] = wifi_ip();
  doc["tokenSet"] = provision_token_set();
  sendJson(doc);
}

static void handleUpdateFinal() {
  if (!uploadStarted) {
    sendError(400, "invalid_payload", "envie o firmware no campo firmware");
    return;
  }
  uploadStarted = false;

  if (!uploadAuthOk) {
    send401();
    return;
  }
  if (uploadBusy) {
    sendError(409, "busy", "atualizacao em andamento");
    return;
  }
  if (!uploadSuccess) {
    if (uploadError == "invalid_sha256") {
      sendError(400, "invalid_sha256", "informe ?sha256=<64 hex>");
      return;
    }
    if (uploadError == "sha_mismatch") {
      sendError(400, "sha_mismatch", "sha256 do upload nao confere");
      return;
    }
    if (uploadError == "firmware_too_large") {
      sendError(413, "firmware_too_large", "firmware maior que a particao OTA");
      return;
    }
    String message = uploadError.isEmpty() ? "update_failed" : uploadError;
    if (Update.hasError()) {
      const char *detail = Update.errorString();
      if (detail && detail[0]) message = detail;
    }
    Update.abort();
    sendError(500, message, message);
    return;
  }

  JsonDocument result;
  result["ok"] = true;
  sendJson(result);
  system_restart_in(1000);
}

static void handleUpdateUpload() {
  HTTPUpload &upload = server.upload();

  if (upload.status == UPLOAD_FILE_START) {
    uploadStarted = true;
    uploadAuthOk = authOk();
    uploadBusy = false;
    uploadSuccess = false;
    uploadError = "";
    uploadShaActive = false;
    uploadShaExpected = false;
    uploadExpectedSha = "";

    if (!uploadAuthOk) {
      // Nao abortar um OTA remoto que esteja usando o Update neste momento.
      if (!ota_busy()) Update.abort();
      return;
    }
    if (ota_busy() || rf_isBusy()) {
      uploadBusy = true;
      // Nao chamar Update.abort(): abortaria o OTA remoto em andamento.
      return;
    }

    ota_local_begin();
    if (!Update.begin(UPDATE_SIZE_UNKNOWN)) {
      uploadError = "update_begin_failed";
      Update.abort();
      ota_local_end(false);
      return;
    }

    // O fallback local tambem exige SHA-256. A pagina calcula o hash no
    // navegador e envia em ?sha256=...; curl/hub devem fazer o mesmo.
    uploadExpectedSha = server.arg("sha256");
    uploadExpectedSha.trim();
    if (!validSha(uploadExpectedSha)) {
      uploadError = "invalid_sha256";
      Update.abort();
      ota_local_end(false);
      return;
    }
    uploadShaExpected = true;
    mbedtls_sha256_init(&uploadShaCtx);
    mbedtls_sha256_starts(&uploadShaCtx, 0);
    uploadShaActive = true;
    return;
  }

  if (upload.status == UPLOAD_FILE_WRITE) {
    if (!uploadAuthOk || uploadBusy) return;
    if (!uploadError.isEmpty()) return;
    if (Update.write(upload.buf, upload.currentSize) != upload.currentSize) {
      uploadError = (Update.getError() == UPDATE_ERROR_SPACE)
                        ? "firmware_too_large"
                        : "update_write_failed";
      Update.abort();
      return;
    }
    if (uploadShaActive) {
      mbedtls_sha256_update(&uploadShaCtx, upload.buf, upload.currentSize);
    }
    return;
  }

  if (upload.status == UPLOAD_FILE_END) {
    if (!uploadAuthOk || uploadBusy) return;

    if (uploadShaActive) {
      uint8_t digest[32];
      mbedtls_sha256_finish(&uploadShaCtx, digest);
      mbedtls_sha256_free(&uploadShaCtx);
      uploadShaActive = false;
      if (uploadShaExpected) {
        char hex[65];
        for (uint8_t i = 0; i < 32; i++) {
          snprintf(hex + i * 2, 3, "%02x", digest[i]);
        }
        hex[64] = '\0';
        if (strcasecmp(hex, uploadExpectedSha.c_str()) != 0) {
          uploadError = "sha_mismatch";
          Update.abort();
        }
      }
    }

    if (uploadError.isEmpty() && !Update.hasError() && Update.end(true)) {
      uploadSuccess = true;
    } else if (uploadError.isEmpty()) {
      uploadError = "update_end_failed";
    }
    ota_local_end(uploadSuccess);
    return;
  }

  if (upload.status == UPLOAD_FILE_ABORTED) {
    uploadStarted = false;
    if (uploadShaActive) {
      mbedtls_sha256_free(&uploadShaCtx);
      uploadShaActive = false;
    }
    if (!uploadBusy && !ota_busy()) Update.abort();
    if (uploadAuthOk && !uploadBusy) ota_local_end(false);
  }
}

static void handleNotFound() {
  if (wifi_ap_active()) {
    String url = String("http://") + WiFi.softAPIP().toString() + "/";
    server.sendHeader("Location", url);
    server.send(302, "text/plain", "");
    return;
  }
  sendError(404, "not_found", "rota inexistente");
}

void http_begin() {
  server.on("/", HTTP_GET, handleRoot);
  server.on("/style.css", HTTP_GET, handleCss);
  server.on("/api/health", HTTP_GET, handleHealth);
  server.on("/api/manifest", HTTP_GET, handleManifest);
  server.on("/api/state", HTTP_GET, handleStateGet);
  server.on("/api/state", HTTP_POST, handleStatePost);
  server.on("/api/correct", HTTP_POST, handleCorrect);
  server.on("/api/learn/start", HTTP_POST, handleLearnStart);
  server.on("/api/learn/status", HTTP_GET, handleLearnStatus);
  server.on("/api/learn/cancel", HTTP_POST, handleLearnCancel);
  server.on("/api/config", HTTP_GET, handleConfigGet);
  server.on("/api/config", HTTP_POST, handleConfigPost);
  server.on("/api/hub", HTTP_POST, handleHubPost);
  // Compatibilidade com o export/import do firmware legacy.
  server.on("/api/config/export", HTTP_GET, handleConfigExport);
  server.on("/api/config/import", HTTP_POST, handleConfigPost);
  server.on("/api/diag", HTTP_GET, handleDiag);
  server.on("/api/fan/add", HTTP_POST, handleFanAdd);
  server.on("/api/fan/remove", HTTP_POST, handleFanRemove);
  server.on("/api/fan/rename", HTTP_POST, handleFanRename);
  server.on("/api/wifi", HTTP_POST, handleWifi);
  server.on("/api/update", HTTP_POST, handleUpdateFinal, handleUpdateUpload);
  server.on("/api/reboot", HTTP_POST, handleReboot);
  server.on("/api/scan", HTTP_GET, handleScan);
  server.on("/api/provision", HTTP_POST, handleProvision);
  server.on("/api/provision/status", HTTP_GET, handleProvisionStatus);
  server.onNotFound(handleNotFound);
  server.begin();
  Serial.printf("[http] servidor em http://%s/\n", wifi_ip().c_str());
}

void http_loop() {
  server.handleClient();
}
