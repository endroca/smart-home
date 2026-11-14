#include "ota.h"

#include <ArduinoJson.h>
#include <HTTPClient.h>
#include <Update.h>
#include <WiFiClient.h>
#include <WiFiClientSecure.h>
#include <cstring>
#include <mbedtls/sha256.h>

#include "http_server.h"
#include "hub_client.h"
#include "system.h"

static bool pending = false;
static String pendingUrl;
static String pendingSha;
static String pendingVersion;
static String pendingCommandId;

static const char *otaState = "idle";
static String otaCurrentVersion;
static String otaError;
static bool localBusy = false;

static bool validSha(const String &sha) {
  if (sha.length() != 64) return false;
  for (size_t i = 0; i < sha.length(); i++) {
    char c = sha[i];
    if (!isxdigit((unsigned char)c)) return false;
  }
  return true;
}

static void publishProgress(int percent) {
  JsonDocument doc;
  doc["event"] = "ota.progress";
  doc["id"] = pendingCommandId;
  doc["version"] = pendingVersion;
  doc["percent"] = percent;
  String payload;
  serializeJson(doc, payload);
  hub_client_queue_event(payload);
}

static void publishDone(bool ok, const String &error) {
  JsonDocument doc;
  doc["event"] = "ota.done";
  doc["id"] = pendingCommandId;
  doc["version"] = pendingVersion.isEmpty() ? otaCurrentVersion : pendingVersion;
  doc["ok"] = ok;
  if (!ok) {
    JsonObject err = doc["error"].to<JsonObject>();
    err["code"] = error;
    err["message"] = error;
  }
  String payload;
  serializeJson(doc, payload);
  hub_client_queue_event(payload);
}

static void failOta(const String &error) {
  Update.abort();
  otaState = "error";
  otaError = error;
  publishDone(false, error);
  hub_client_queue_status();
  Serial.printf("[ota] falhou: %s\n", error.c_str());
}

bool ota_busy() {
  if (pending || localBusy) return true;
  return strcmp(otaState, "downloading") == 0 ||
         strcmp(otaState, "verifying") == 0 ||
         strcmp(otaState, "applying") == 0;
}

const char *ota_state() {
  return otaState;
}

const char *ota_version() {
  return otaCurrentVersion.c_str();
}

String ota_last_error() {
  return otaError;
}

void ota_local_begin() {
  if (ota_busy()) return;
  localBusy = true;
  otaState = "downloading";
  otaCurrentVersion = "";
  otaError = "";
  hub_client_queue_status();
}

void ota_local_end(bool ok) {
  localBusy = false;
  if (ok) {
    otaState = "applying";
  } else {
    otaState = "error";
    otaError = "local_update_failed";
  }
  hub_client_queue_status();
}

void ota_loop() {
  if (!pending) return;

  pending = false;
  otaState = "downloading";
  otaCurrentVersion = pendingVersion;
  otaError = "";
  hub_client_queue_status();

  Serial.printf("[ota] baixando %s (sha=%s)\n", pendingUrl.c_str(),
                pendingSha.c_str());

  WiFiClient plain;
  WiFiClientSecure secure;
  HTTPClient http;
  http.setTimeout(20000);

  bool https = pendingUrl.startsWith("https://");
  bool begun = false;
  if (https) {
    secure.setInsecure();
    begun = http.begin(secure, pendingUrl);
  } else {
    begun = http.begin(plain, pendingUrl);
  }
  if (!begun) {
    failOta("download_failed");
    return;
  }

  int code = http.GET();
  if (code != HTTP_CODE_OK) {
    http.end();
    failOta("download_failed");
    return;
  }

  int contentLength = http.getSize();
  if (contentLength <= 0) {
    // O servidor da Pi precisa enviar Content-Length; chunked nao e suportado.
    http.end();
    failOta("download_failed");
    return;
  }
  if (!Update.begin((size_t)contentLength)) {
    http.end();
    failOta("ota_failed");
    return;
  }

  mbedtls_sha256_context shaCtx;
  mbedtls_sha256_init(&shaCtx);
  mbedtls_sha256_starts(&shaCtx, 0);

  uint8_t buffer[1024];
  size_t written = 0;
  uint32_t lastPublishAt = 0;
  uint32_t lastDataAt = millis();
  int lastPercent = -1;
  bool writeError = false;
  bool readTimeout = false;

  WiFiClient *stream = http.getStreamPtr();
  while (http.connected() || stream->available() > 0) {
    if (written >= (size_t)contentLength) break;
    size_t available = stream->available();
    if (available == 0) {
      if (millis() - lastDataAt > 30000) {
        readTimeout = true;
        break;
      }
      delay(1);
      hub_client_loop();
      http_loop();
      continue;
    }
    size_t chunk = available > sizeof(buffer) ? sizeof(buffer) : available;
    size_t read = stream->readBytes(buffer, chunk);
    if (read == 0) break;
    lastDataAt = millis();

    if (Update.write(buffer, read) != read) {
      writeError = true;
      break;
    }
    mbedtls_sha256_update(&shaCtx, buffer, read);
    written += read;

    int percent = (int)((written * 100) / (size_t)contentLength);
    if (percent >= lastPercent + 5 ||
        (uint32_t)(millis() - lastPublishAt) > 2000) {
      lastPercent = percent;
      lastPublishAt = millis();
      publishProgress(percent);
      hub_client_queue_status();
    }
    hub_client_loop();
    http_loop();
    delay(0);
  }

  uint8_t digest[32];
  mbedtls_sha256_finish(&shaCtx, digest);
  mbedtls_sha256_free(&shaCtx);
  http.end();

  if (writeError) {
    failOta("ota_failed");
    return;
  }
  if (readTimeout || written != (size_t)contentLength) {
    failOta("download_failed");
    return;
  }

  char hex[65];
  for (uint8_t i = 0; i < 32; i++) {
    snprintf(hex + i * 2, 3, "%02x", digest[i]);
  }
  hex[64] = '\0';

  otaState = "verifying";
  hub_client_queue_status();

  if (strcasecmp(hex, pendingSha.c_str()) != 0) {
    failOta("sha_mismatch");
    return;
  }

  otaState = "applying";
  hub_client_queue_status();
  if (!Update.end(true)) {
    failOta("ota_failed");
    return;
  }

  Serial.println("[ota] atualizacao aplicada; reiniciando");
  publishDone(true, "");
  hub_client_queue_status();
  system_restart_in(1500);
}
