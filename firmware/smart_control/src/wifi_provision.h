#pragma once

#include <Arduino.h>

void wifi_begin();
void wifi_loop();
void provision_loop();

bool wifi_connected();
bool wifi_ap_active();
bool wifi_config_set();
bool wifi_manual_ap();
String wifi_ip();
int wifi_rssi();
String wifi_ap_ssid();
String wifi_mdns_name();

void wifi_save(const String &ssid, const String &password);
void wifi_clear();
void provision_start_ap(bool manual = false);
void provision_stop_ap();

void provision_set_token(const String &token);
bool provision_token_set();
bool provision_check_token(const String &token);
