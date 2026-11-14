#pragma once

#include <Arduino.h>

void ota_loop();

bool ota_busy();
const char *ota_state();
const char *ota_version();
String ota_last_error();

void ota_local_begin();
void ota_local_end(bool ok);
