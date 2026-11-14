#pragma once

#include <Arduino.h>

// ---------------------------------------------------------------------------
// Identidade do device
//
// O thing name é a identidade do gateway para o hub. É um default de build,
// sobrescrevível com -DTHING_NAME=...
// ---------------------------------------------------------------------------
#ifndef THING_NAME
#define THING_NAME "smart-control-rf01"
#endif

#ifndef FIRMWARE_VERSION
#define FIRMWARE_VERSION "1.0.0"
#endif

#define MODEL_NAME "smart_control_esp32"
#define PROTOCOL_NAME "ventisol-18639"

// ---------------------------------------------------------------------------
// Hardware
// ---------------------------------------------------------------------------
#define PIN_SCK 18
#define PIN_MISO 19
#define PIN_MOSI 23
#define PIN_CSN 5
#define PIN_GDO0 12
#define PIN_GDO2 27
#define PIN_LED 2

// ---------------------------------------------------------------------------
// RF Ventisol
// ---------------------------------------------------------------------------
#define RF_FREQ_MHZ 433.92
#define RF_BAUD 4.0
#define RF_TX_ROUNDS 4
#define RF_TX_BUF_LEN 63
#define MAX_EDGES 130
#define SELF_TX_GUARD_MS 1500
#define LEARN_TIMEOUT_MS 15000
#define RF_RX_DEBOUNCE_MS 500

// Bytes genéricos do protocolo (fallback sem aprendizado).
#define RF_BYTE_POWER 0x74
#define RF_BYTE_PLUS 0x7B
#define RF_BYTE_MINUS 0x78
#define RF_BYTE_OUTRO 0x73

// ---------------------------------------------------------------------------
// Modelo de devices
// ---------------------------------------------------------------------------
#define MAX_FANS 8
#define FAN_NAME_LEN 33
#define COMMAND_COUNT 11

enum FanCommand : uint8_t {
  FAN_CMD_POWER = 0,
  FAN_CMD_LIGHT,
  FAN_CMD_VEL_0,
  FAN_CMD_VEL_1,
  FAN_CMD_VEL_2,
  FAN_CMD_VEL_3,
  FAN_CMD_VEL_4,
  FAN_CMD_VEL_5,
  FAN_CMD_VEL_6,
  FAN_CMD_EXHAUST,
  FAN_CMD_VENT,
};

enum class FanSource : uint8_t {
  BOOT = 0,
  HUB, // mudanca pedida pelo hub (API HTTP)
  HTTP,
  RF_REMOTE,
  CORRECTION,
};

enum class FanDirection : uint8_t {
  FORWARD = 0,
  REVERSE,
};

// ---------------------------------------------------------------------------
// Rede / HTTP
// ---------------------------------------------------------------------------
#define HTTP_PORT 80
#define WIFI_CONNECT_TIMEOUT_MS 20000
#define WIFI_RETRY_MS 5000
#define AP_RECOVERY_MS 60000
#define STATUS_INTERVAL_MS 60000
#define TOKEN_MIN_LEN 16

// ---------------------------------------------------------------------------
// Push para o hub (HTTP)
//
// O device empurra estado, status (heartbeat) e eventos para o hub. O POST sai
// de dentro do loop(), nunca do handler HTTP, com um timeout curto.
// ---------------------------------------------------------------------------
#define HUB_HEARTBEAT_MS 30000   // cadencia do status/heartbeat
#define HUB_PUSH_TIMEOUT_MS 4000 // timeout de cada POST
#define HUB_RETRY_MS 5000        // espera inicial apos uma falha
#define HUB_MAX_BACKOFF_MS 60000 // teto do backoff
#define HUB_EVENT_QUEUE 6        // tamanho da fila de eventos assincronos

// ---------------------------------------------------------------------------
// NVS
// ---------------------------------------------------------------------------
#define NVS_NAMESPACE "smart_control"
#define NVS_CONFIG_VERSION 1
