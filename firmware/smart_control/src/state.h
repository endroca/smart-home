#pragma once

#include <Arduino.h>
#include <ArduinoJson.h>

#include "config.h"

struct FanConfig {
  char name[FAN_NAME_LEN];
  uint8_t hasCommand[COMMAND_COUNT];
  uint8_t stateOffset[COMMAND_COUNT];
  uint8_t command[COMMAND_COUNT][4];
};

struct FanState {
  bool power;
  bool light;
  uint8_t speed;     // 0..6
  uint8_t lastSpeed; // ultima velocidade nao nula, usada ao religar
  FanDirection direction;
  uint8_t counter; // 0..7
  uint32_t updatedAt;
  FanSource source;
};

struct FanTarget {
  bool hasPower;
  bool power;
  bool hasLight;
  bool light;
  bool hasSpeed;
  uint8_t speed;
  bool hasDirection;
  FanDirection direction;
};

typedef void (*StateChangedHandler)(uint8_t fan, bool transmittedRf);

void state_begin();
void state_setChangedHandler(StateChangedHandler handler);
uint8_t state_fanCount();

bool state_validFan(uint8_t fan);
const FanConfig &state_config(uint8_t fan);
const FanState &state_get(uint8_t fan);

String state_deviceId(uint8_t fan);
uint8_t state_findById(const String &deviceId);

const char *state_commandName(uint8_t command);
int state_commandFromName(const String &name);
const char *state_sourceName(FanSource source);
bool state_sourceFromName(const String &name, FanSource &source);
const char *state_directionName(FanDirection direction);
bool state_directionFromName(const String &name, FanDirection &direction);

bool state_parseTarget(JsonObjectConst obj, FanTarget &target);

bool state_apply(uint8_t fan, const FanTarget &target, FanSource source,
                 String &error, uint16_t *sentMask = nullptr,
                 bool force = false);
bool state_correct(uint8_t fan, const FanTarget &target, String &error);
void state_onRfCommand(uint8_t fan, uint8_t command, uint8_t counter);

bool state_addFan(uint8_t &newFan, String &error);
bool state_removeFan(uint8_t fan, String &error);
bool state_renameFan(uint8_t fan, const String &name, String &error);

void state_setCounter(uint8_t fan, uint8_t counter, bool persist = true);
void state_setCommand(uint8_t fan, uint8_t command, const uint8_t data[4],
                      uint8_t stateOffset);
void state_saveConfig(uint8_t fan);
void state_saveState(uint8_t fan);
void state_saveAll();
void state_reset();

void state_configToJson(JsonObject target);
void state_configToLegacyJson(JsonObject target);
bool state_configFromJson(JsonObjectConst source, String &error);
