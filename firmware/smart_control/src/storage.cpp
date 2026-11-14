#include "storage.h"

#include <Preferences.h>
#include <mbedtls/md.h>

#include "config.h"

static Preferences prefs;
static bool prefsReady = false;
static bool tokenSet = false;

static const char *TOKEN_HASH_KEY = "token_hash";
static const char *TOKEN_SET_KEY = "token_set";

static void sha256(const String &value, uint8_t out[32]) {
  const mbedtls_md_info_t *info = mbedtls_md_info_from_type(MBEDTLS_MD_SHA256);
  mbedtls_md(info, reinterpret_cast<const unsigned char *>(value.c_str()),
             value.length(), out);
}

static bool constantTimeEqual(const uint8_t *a, const uint8_t *b, size_t length) {
  uint8_t diff = 0;
  for (size_t i = 0; i < length; i++) {
    diff |= (uint8_t)(a[i] ^ b[i]);
  }
  return diff == 0;
}

bool storage_begin() {
  prefsReady = prefs.begin(NVS_NAMESPACE, false);
  if (!prefsReady) {
    Serial.println("[storage] falha ao abrir NVS");
    return false;
  }

  tokenSet = false;
  if (prefs.getBool(TOKEN_SET_KEY, false) &&
      prefs.getBytesLength(TOKEN_HASH_KEY) == 32) {
    tokenSet = true;
  }
  return true;
}

String storage_getString(const char *key, const String &fallback) {
  if (!prefsReady) return fallback;
  return prefs.getString(key, fallback);
}

void storage_putString(const char *key, const String &value) {
  if (!prefsReady) return;
  prefs.putString(key, value);
}

uint8_t storage_getUChar(const char *key, uint8_t fallback) {
  if (!prefsReady) return fallback;
  return prefs.getUChar(key, fallback);
}

void storage_putUChar(const char *key, uint8_t value) {
  if (!prefsReady) return;
  prefs.putUChar(key, value);
}

uint32_t storage_getUInt(const char *key, uint32_t fallback) {
  if (!prefsReady) return fallback;
  return prefs.getUInt(key, fallback);
}

void storage_putUInt(const char *key, uint32_t value) {
  if (!prefsReady) return;
  prefs.putUInt(key, value);
}

size_t storage_getBytes(const char *key, void *buffer, size_t length) {
  if (!prefsReady) return 0;
  return prefs.getBytes(key, buffer, length);
}

bool storage_putBytes(const char *key, const void *buffer, size_t length) {
  if (!prefsReady) return false;
  return prefs.putBytes(key, buffer, length) == length;
}

void storage_remove(const char *key) {
  if (!prefsReady) return;
  prefs.remove(key);
}

size_t storage_free_entries() {
  if (!prefsReady) return 0;
  return prefs.freeEntries();
}

void storage_factory_reset() {
  if (!prefsReady) return;
  prefs.clear();
  // O namespace do firmware legacy tambem e apagado: "reset" deve impedir que
  // a proxima inicializacao reimporte comandos antigos.
  Preferences legacy;
  if (legacy.begin("ventisol", false)) {
    legacy.clear();
    legacy.end();
  }
  tokenSet = false;
}

bool storage_token_set() {
  return tokenSet;
}

void storage_set_token(const String &token) {
  if (!prefsReady || token.isEmpty()) return;
  uint8_t hash[32];
  sha256(token, hash);
  if (prefs.putBytes(TOKEN_HASH_KEY, hash, sizeof(hash)) == sizeof(hash)) {
    prefs.putBool(TOKEN_SET_KEY, true);
    tokenSet = true;
  }
}

void storage_clear_token() {
  if (!prefsReady) return;
  prefs.remove(TOKEN_HASH_KEY);
  prefs.remove(TOKEN_SET_KEY);
  tokenSet = false;
}

bool storage_check_token(const String &token) {
  if (!tokenSet || token.isEmpty()) return false;
  uint8_t stored[32];
  if (prefs.getBytes(TOKEN_HASH_KEY, stored, sizeof(stored)) != sizeof(stored)) {
    return false;
  }
  uint8_t provided[32];
  sha256(token, provided);
  return constantTimeEqual(stored, provided, sizeof(stored));
}

String storage_token_hash_hex() {
  if (!prefsReady || !tokenSet) return String();
  uint8_t stored[32];
  if (prefs.getBytes(TOKEN_HASH_KEY, stored, sizeof(stored)) != sizeof(stored)) {
    return String();
  }
  char out[65];
  for (size_t i = 0; i < sizeof(stored); i++) {
    snprintf(out + i * 2, 3, "%02x", stored[i]);
  }
  out[64] = '\0';
  return String(out);
}
