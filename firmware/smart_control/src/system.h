#pragma once

#include <Arduino.h>

void system_restart_in(uint32_t ms);
void system_loop();
uint32_t system_uptime_seconds();
uint32_t system_free_heap();
uint32_t system_now_epoch();
bool system_time_synced();
