#include "system.h"

#include <time.h>

static uint32_t restartAt = 0;

void system_restart_in(uint32_t ms) {
  restartAt = millis() + ms;
}

void system_loop() {
  if (restartAt != 0 && (int32_t)(millis() - restartAt) >= 0) {
    Serial.println("[system] reiniciando...");
    ESP.restart();
  }
}

uint32_t system_uptime_seconds() {
  return millis() / 1000;
}

uint32_t system_free_heap() {
  return ESP.getFreeHeap();
}

uint32_t system_now_epoch() {
  time_t now = time(nullptr);
  return now > 1700000000 ? (uint32_t)now : 0;
}

bool system_time_synced() {
  return system_now_epoch() != 0;
}
