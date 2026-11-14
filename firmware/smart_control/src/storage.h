#pragma once

#include <Arduino.h>

bool storage_begin();

String storage_getString(const char *key, const String &fallback = String());
void storage_putString(const char *key, const String &value);

uint8_t storage_getUChar(const char *key, uint8_t fallback);
void storage_putUChar(const char *key, uint8_t value);

uint32_t storage_getUInt(const char *key, uint32_t fallback);
void storage_putUInt(const char *key, uint32_t value);

size_t storage_getBytes(const char *key, void *buffer, size_t length);
bool storage_putBytes(const char *key, const void *buffer, size_t length);
void storage_remove(const char *key);
size_t storage_free_entries();
void storage_factory_reset();

bool storage_token_set();
void storage_set_token(const String &token);
void storage_clear_token();
bool storage_check_token(const String &token);

// storage_token_hash_hex returns the SHA-256 of the configured token as a hex
// string ("" when no token is set). The device authenticates to the hub with
// this digest, so the plaintext token never has to be stored on the device.
String storage_token_hash_hex();
