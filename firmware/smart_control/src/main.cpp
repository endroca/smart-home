#include <Arduino.h>
#include <ArduinoJson.h>
#include <time.h>

#include "config.h"
#include "http_server.h"
#include "hub_client.h"
#include "ota.h"
#include "rf_ventisol.h"
#include "state.h"
#include "storage.h"
#include "system.h"
#include "wifi_provision.h"

static void serialHelp() {
  Serial.println();
  Serial.println("Comandos seriais:");
  Serial.println("  status                          mostra o estado atual");
  Serial.println("  ap                              liga o SoftAP de provisionamento");
  Serial.println("  apoff                           desliga o SoftAP");
  Serial.println("  token <valor>                   grava o token local (>=16 chars)");
  Serial.println("  wifi <ssid> <senha>             grava o Wi-Fi e reinicia");
  Serial.println("  learn <id> <comando>            inicia aprendizado RF");
  Serial.println("  hub <url>                       configura o push para o hub");
  Serial.println("  huboff                          desliga o push para o hub");
  Serial.println("  reset                           apaga toda a NVS e reinicia");
  Serial.println("  reboot                          reinicia");
}

static void serialStatus() {
  Serial.printf("thing       : %s\n", THING_NAME);
  Serial.printf("firmware    : %s\n", FIRMWARE_VERSION);
  Serial.printf("wifi        : %s ip=%s rssi=%d\n",
                wifi_connected() ? "conectado" : "desconectado",
                wifi_ip().c_str(), wifi_rssi());
  Serial.printf("softap      : %s\n", wifi_ap_active() ? wifi_ap_ssid().c_str() : "off");
  Serial.printf("hub push    : %s url=%s pending=%s\n",
                hub_client_configured()
                    ? (hub_client_reachable() ? "conectado" : "pendente")
                    : "desligado",
                hub_client_url().c_str(),
                hub_client_pending() ? "sim" : "nao");
  Serial.printf("hub         : ok=%u fail=%u\n", (unsigned)hub_client_ok_count(),
                (unsigned)hub_client_fail_count());
  if (!hub_client_last_error().isEmpty()) {
    Serial.printf("hub erro    : %s\n", hub_client_last_error().c_str());
  }
  Serial.printf("rf          : %s tx=%u rx=%u\n", rf_radioOk() ? "ok" : "ausente",
                (unsigned)rf_txCount(), (unsigned)rf_rxCount());
  Serial.printf("fans        : %u\n", (unsigned)state_fanCount());
  for (uint8_t i = 0; i < state_fanCount(); i++) {
    const FanConfig &cfg = state_config(i);
    const FanState &st = state_get(i);
    Serial.printf("  %s %-16s power=%d light=%d speed=%u dir=%s counter=%u src=%s\n",
                  state_deviceId(i).c_str(), cfg.name, st.power ? 1 : 0,
                  st.light ? 1 : 0, (unsigned)st.speed,
                  state_directionName(st.direction), (unsigned)st.counter,
                  state_sourceName(st.source));
  }
  Serial.printf("heap        : %u\n", (unsigned)system_free_heap());
}

static void serialLoop() {
  if (!Serial.available()) return;

  String line = Serial.readStringUntil('\n');
  line.trim();
  if (line.isEmpty()) return;

  if (line == "help") {
    serialHelp();
    return;
  }
  if (line == "status") {
    serialStatus();
    return;
  }
  if (line == "ap") {
    provision_start_ap(true);
    return;
  }
  if (line == "apoff") {
    provision_stop_ap();
    return;
  }
  if (line == "reboot") {
    Serial.println("[serial] reiniciando...");
    system_restart_in(200);
    return;
  }
  if (line == "reset") {
    Serial.println("[serial] apagando NVS e reiniciando...");
    storage_factory_reset();
    system_restart_in(500);
    return;
  }
  if (line.startsWith("token ")) {
    String token = line.substring(6);
    token.trim();
    if (token.length() < TOKEN_MIN_LEN) {
      Serial.printf("[serial] token precisa de ao menos %d caracteres\n",
                    TOKEN_MIN_LEN);
      return;
    }
    provision_set_token(token);
    Serial.println("[serial] token gravado");
    return;
  }
  if (line.startsWith("wifi ")) {
    int sep = line.indexOf(' ', 5);
    String ssid = sep < 0 ? line.substring(5) : line.substring(5, sep);
    String password = sep < 0 ? "" : line.substring(sep + 1);
    ssid.trim();
    if (ssid.isEmpty()) {
      Serial.println("[serial] uso: wifi <ssid> <senha>");
      return;
    }
    wifi_save(ssid, password);
    Serial.println("[serial] wifi gravado; reiniciando...");
    system_restart_in(500);
    return;
  }
  if (line.startsWith("learn ")) {
    int sep = line.indexOf(' ', 6);
    String deviceId = sep < 0 ? line.substring(6) : line.substring(6, sep);
    String commandName = sep < 0 ? "" : line.substring(sep + 1);
    deviceId.trim();
    commandName.trim();
    uint8_t fan = state_findById(deviceId);
    int command = state_commandFromName(commandName);
    if (fan == 255 || command < 0) {
      Serial.println("[serial] uso: learn <fan0..fan7> <power|light|vel_0..6|exhaust|vent>");
      return;
    }
    String error;
    if (!rf_startLearn(fan, (uint8_t)command, "serial", error)) {
      Serial.printf("[serial] falha: %s\n", error.c_str());
    } else {
      Serial.println("[serial] aguardando botao do controle...");
    }
    return;
  }
  if (line == "huboff") {
    hub_client_clear_url();
    Serial.println("[serial] push para o hub desligado");
    return;
  }
  if (line.startsWith("hub ")) {
    String url = line.substring(4);
    url.trim();
    if (url.isEmpty()) {
      Serial.println("[serial] uso: hub <url> (ex.: http://hub.local)");
      return;
    }
    hub_client_set_url(url);
    Serial.println("[serial] push para o hub configurado");
    return;
  }

  Serial.println("[serial] comando desconhecido; use 'help'");
}

void setup() {
  Serial.begin(115200);
  delay(200);

  Serial.println();
  Serial.println("==============================================");
  Serial.println(" smart_control - ESP32 + CC1101 + hub HTTP");
  Serial.println("==============================================");

  if (!storage_begin()) {
    Serial.println("[boot] NVS indisponivel; seguindo com defaults");
  }

  state_begin();
  state_setChangedHandler([](uint8_t fan, bool) {
    // O device é a fonte de verdade: avisa o hub do que acabou de acontecer.
    // O hub espelha o estado e reconcilia o que ele mesmo pediu.
    hub_client_queue_state(fan);
  });

  wifi_begin();
  configTime(0, 0, "pool.ntp.org", "time.nist.gov");

  rf_begin();
  hub_client_begin();
  http_begin();

  Serial.printf("[boot] heap inicial: %u bytes\n", (unsigned)system_free_heap());
  serialHelp();
}

// publishLearnFinished queues a learn.finished event once per finished session.
// The event is a device concern and travels through the hub push queue.
static void publishLearnFinished() {
  static uint32_t lastSerial = 0;
  LearnStatus learn = rf_learnStatus();
  if (!learn.finished || learn.serial == lastSerial) return;

  JsonDocument event;
  event["event"] = "learn.finished";
  event["id"] = state_deviceId(learn.fan);
  if (!learn.requestId.isEmpty()) event["requestId"] = learn.requestId;
  event["command"] = state_commandName(learn.command);
  event["ok"] = learn.success;
  if (learn.success) {
    event["frame"] = learn.frameHex;
  } else {
    JsonObject error = event["error"].to<JsonObject>();
    error["code"] = learn.cancelled ? "cancelled" : "rf_timeout";
    error["message"] =
        learn.cancelled ? "aprendizado cancelado" : "nenhum frame captado";
  }
  String payload;
  serializeJson(event, payload);
  hub_client_queue_event(payload);
  lastSerial = learn.serial;
}

void loop() {
  http_loop();
  wifi_loop();
  provision_loop();
  rf_loop();
  hub_client_loop();
  publishLearnFinished();
  ota_loop();
  serialLoop();
  system_loop();
  delay(1);
}
