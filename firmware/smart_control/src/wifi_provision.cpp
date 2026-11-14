#include "wifi_provision.h"

#include <DNSServer.h>
#include <ESPmDNS.h>
#include <WiFi.h>

#include "config.h"
#include "storage.h"

static DNSServer dnsServer;
static bool apActive = false;
static bool manualAp = false;
static bool mdnsStarted = false;
static uint32_t lastAttemptAt = 0;
static uint32_t lastConnectedAt = 0;
static uint32_t lastMdnsTryAt = 0;
static uint32_t lastApAttemptAt = 0;
static uint32_t disconnectedSince = 0;

static String apSsid() {
  String ssid = String(THING_NAME);
  if (!ssid.startsWith("smart-control")) {
    ssid = String("smart-control-") + ssid;
  }
  if (ssid.length() > 32) ssid = ssid.substring(0, 32);
  return ssid;
}

String wifi_mdns_name() {
  String host = String(THING_NAME);
  host.toLowerCase();
  for (size_t i = 0; i < host.length(); i++) {
    char c = host[i];
    if (!isalnum((unsigned char)c) && c != '-') host[i] = '-';
  }
  while (host.length() > 0 && host[0] == '-') host.remove(0, 1);
  while (host.length() > 0 && host[host.length() - 1] == '-') {
    host.remove(host.length() - 1);
  }
  if (host.isEmpty()) host = "smart-control";
  if (host.length() > 63) host = host.substring(0, 63);
  while (host.length() > 0 && host[host.length() - 1] == '-') {
    host.remove(host.length() - 1);
  }
  return host;
}

static void startMdns() {
  if (mdnsStarted || WiFi.status() != WL_CONNECTED) return;
  String host = wifi_mdns_name();
  if (MDNS.begin(host.c_str())) {
    MDNS.addService("http", "tcp", HTTP_PORT);
    MDNS.addServiceTxt("http", "tcp", "thing", THING_NAME);
    mdnsStarted = true;
    Serial.printf("[wifi] mDNS: http://%s.local/\n", host.c_str());
  } else {
    Serial.println("[wifi] falha ao iniciar mDNS");
  }
}

static void onConnected() {
  lastConnectedAt = millis();
  Serial.printf("[wifi] conectado em %s (RSSI %d)\n",
                WiFi.localIP().toString().c_str(), WiFi.RSSI());
  startMdns();
  if (apActive && !manualAp) provision_stop_ap();
}

void provision_start_ap(bool manual) {
  if (apActive) {
    manualAp = manualAp || manual;
    return;
  }

  lastApAttemptAt = millis();
  manualAp = manual;
  WiFi.mode(WIFI_AP_STA);
  WiFi.softAPConfig(IPAddress(192, 168, 4, 1), IPAddress(192, 168, 4, 1),
                    IPAddress(255, 255, 255, 0));
  String ssid = apSsid();
  bool ok = WiFi.softAP(ssid.c_str());
  if (!ok) {
    Serial.println("[wifi] falha ao iniciar SoftAP");
    return;
  }
  dnsServer.start(53, "*", WiFi.softAPIP());
  apActive = true;
  Serial.printf("[wifi] SoftAP aberto: %s em http://%s/\n", ssid.c_str(),
                WiFi.softAPIP().toString().c_str());
}

void provision_stop_ap() {
  if (!apActive) return;
  dnsServer.stop();
  WiFi.softAPdisconnect(true);
  apActive = false;
  manualAp = false;
  if (wifi_config_set()) {
    WiFi.mode(WIFI_STA);
  }
  Serial.println("[wifi] SoftAP encerrado");
}

void wifi_begin() {
  WiFi.persistent(false);
  WiFi.setHostname(THING_NAME);
  WiFi.setSleep(false);
  WiFi.setTxPower(WIFI_POWER_17dBm);
  WiFi.setAutoReconnect(true);
  WiFi.mode(WIFI_STA);

  String ssid = storage_getString("wifi_ssid", "");
  String password = storage_getString("wifi_pass", "");

  if (ssid.isEmpty()) {
    Serial.println("[wifi] sem configuracao; iniciando provisionamento");
    provision_start_ap();
    return;
  }

  Serial.printf("[wifi] conectando em %s...\n", ssid.c_str());
  WiFi.begin(ssid.c_str(), password.c_str());
  uint32_t started = millis();
  while (WiFi.status() != WL_CONNECTED &&
         millis() - started < WIFI_CONNECT_TIMEOUT_MS) {
    delay(100);
  }
  if (WiFi.status() == WL_CONNECTED) {
    onConnected();
  } else {
    Serial.println("[wifi] sem conexao; SoftAP de recuperacao ativo");
    provision_start_ap();
  }
}

void wifi_loop() {
  uint32_t now = millis();

  if (WiFi.status() == WL_CONNECTED) {
    disconnectedSince = 0;
    if (lastConnectedAt == 0) {
      onConnected();
    } else if (apActive && !manualAp && wifi_config_set()) {
      provision_stop_ap();
    }
    if (!mdnsStarted && now - lastMdnsTryAt > 30000) {
      lastMdnsTryAt = now;
      startMdns();
    }
    return;
  }

  lastConnectedAt = 0;
  if (mdnsStarted) {
    MDNS.end();
    mdnsStarted = false;
  }
  if (disconnectedSince == 0) disconnectedSince = now;

  if (wifi_config_set() && now - lastAttemptAt > WIFI_RETRY_MS) {
    lastAttemptAt = now;
    String ssid = storage_getString("wifi_ssid", "");
    String password = storage_getString("wifi_pass", "");
    WiFi.begin(ssid.c_str(), password.c_str());
  }

  if (!apActive && now - lastApAttemptAt > 5000) {
    if (!wifi_config_set()) {
      provision_start_ap();
    } else if ((uint32_t)(now - disconnectedSince) > AP_RECOVERY_MS) {
      provision_start_ap();
    }
  }
}

void provision_loop() {
  if (apActive) dnsServer.processNextRequest();
}

bool wifi_connected() {
  return WiFi.status() == WL_CONNECTED;
}

bool wifi_ap_active() {
  return apActive;
}

bool wifi_manual_ap() {
  return manualAp;
}

bool wifi_config_set() {
  return !storage_getString("wifi_ssid", "").isEmpty();
}

String wifi_ip() {
  if (WiFi.status() == WL_CONNECTED) return WiFi.localIP().toString();
  if (apActive) return WiFi.softAPIP().toString();
  return "";
}

int wifi_rssi() {
  return WiFi.status() == WL_CONNECTED ? WiFi.RSSI() : 0;
}

String wifi_ap_ssid() {
  return apSsid();
}

void wifi_save(const String &ssid, const String &password) {
  storage_putString("wifi_ssid", ssid);
  storage_putString("wifi_pass", password);
}

void wifi_clear() {
  storage_remove("wifi_ssid");
  storage_remove("wifi_pass");
}

void provision_set_token(const String &token) {
  storage_set_token(token);
}

bool provision_token_set() {
  return storage_token_set();
}

bool provision_check_token(const String &token) {
  return storage_check_token(token);
}
