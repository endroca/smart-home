#pragma once

#include <Arduino.h>

#include "config.h"

struct LearnStatus {
  bool active;
  bool finished;
  bool success;
  bool cancelled;
  uint8_t fan;
  uint8_t command;
  uint32_t serial;
  uint32_t startedAt;
  String requestId;
  String frameHex;
};

void rf_begin();
void rf_loop();

bool rf_radioOk();
bool rf_isBusy();
bool rf_isLearning();

bool rf_transmitCommand(uint8_t fan, uint8_t command, String &error);

bool rf_startLearn(uint8_t fan, uint8_t command, const String &requestId,
                   String &error);
void rf_cancelLearn();
LearnStatus rf_learnStatus();

uint32_t rf_txCount();
uint32_t rf_rxCount();
uint32_t rf_rxChecksumFail();
uint32_t rf_rxUnmatched();
uint32_t rf_rxMatched();
String rf_lastFrameHex();
uint32_t rf_lastFrameAt();
bool rf_txGuardActive();
