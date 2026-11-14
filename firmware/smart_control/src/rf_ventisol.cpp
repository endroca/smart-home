#include "rf_ventisol.h"

#include <SPI.h>
#include <SmartRC_CC1101.h>

#include "state.h"

static SmartRC_CC1101 radio;
static bool radioReady = false;
static bool txInProgress = false;

static const uint8_t STATE_TABLE[4][8] = {
    {0x03, 0x12, 0x21, 0x30, 0x47, 0x56, 0x65, 0x74},  // POWER
    {0x0C, 0x1D, 0x2E, 0x3F, 0x48, 0x59, 0x6A, 0x7B},  // PLUS
    {0x0F, 0x1E, 0x2D, 0x3C, 0x4B, 0x5A, 0x69, 0x78},  // MINUS
    {0x73, 0x04, 0x15, 0x26, 0x37, 0x40, 0x51, 0x62},  // OUTRO
};

static volatile uint32_t durations[MAX_EDGES];
static volatile uint8_t levels[MAX_EDGES];
static volatile uint32_t edgeCount = 0;
static volatile int lastState = 0;
static volatile uint32_t lastEdge = 0;
static volatile bool capturing = false;
static volatile bool frameReady = false;

static uint32_t savedDurations[MAX_EDGES];
static uint8_t savedLevels[MAX_EDGES];
static uint32_t savedEdgeCount = 0;
static uint8_t savedBytes[4] = {0, 0, 0, 0};

static uint32_t selfTxGuardUntil = 0;

static bool learning = false;
static bool learnFinished = false;
static bool learnSuccess = false;
static bool learnCancelled = false;
static uint8_t learnFan = 0;
static uint8_t learnCommand = 0;
static uint32_t learnDeadline = 0;
static uint32_t learnStartedAt = 0;
static uint32_t learnSerial = 0;
static String learnRequestId;
static String learnFrameHex;

static uint32_t txCount = 0;
static uint32_t rxCount = 0;
static uint32_t rxChecksumFail = 0;
static uint32_t rxUnmatched = 0;
static uint32_t rxMatched = 0;
static String lastFrameHex;
static uint32_t lastFrameAt = 0;

static uint8_t lastDebounceFan = 255;
static uint8_t lastDebounceCommand = 255;
static uint32_t lastDebounceAt = 0;

static int genericIndex(uint8_t command) {
  switch (command) {
    case RF_BYTE_POWER:
      return 0;
    case RF_BYTE_PLUS:
      return 1;
    case RF_BYTE_MINUS:
      return 2;
    case RF_BYTE_OUTRO:
      return 3;
    default:
      return -1;
  }
}

static void enterRx();
void onRadioEdge();

static void buildFifoBuffer(uint8_t b0, uint8_t b1, uint8_t b2, uint8_t b3,
                            uint8_t *buf) {
  uint8_t frame[4] = {b0, b1, b2, b3};
  int pos = 0;
  for (int rep = 0; rep < 3; rep++) {
    for (int b = 0; b < 4; b++) {
      uint8_t proto = frame[b];
      for (int pair = 0; pair < 4; pair++) {
        uint8_t bitHi = (proto >> (7 - pair * 2)) & 1;
        uint8_t bitLo = (proto >> (6 - pair * 2)) & 1;
        uint8_t hiNibble = bitHi ? 0xE0 : 0x80;
        uint8_t loNibble = bitLo ? 0x0E : 0x08;
        buf[pos++] = hiNibble | loNibble;
      }
    }
    buf[pos++] = 0x80;
    buf[pos++] = 0x00;
    buf[pos++] = 0x00;
    buf[pos++] = 0x00;
    buf[pos++] = 0x00;
  }
}

static bool txBuffer(const uint8_t *buf) {
  if (!radioReady) return false;

  detachInterrupt(digitalPinToInterrupt(PIN_GDO0));
  radio.setSidle();
  pinMode(PIN_GDO0, INPUT);
  radio.setDRate(RF_BAUD);
  radio.setPA(10);
  radio.SpiWriteReg(CC1101_FSCTRL0, 0x00);
  radio.SpiWriteReg(CC1101_PKTCTRL0, 0x00);
  radio.SpiWriteReg(CC1101_PKTLEN, RF_TX_BUF_LEN);

  bool completed = true;

  for (uint8_t round = 0; round < RF_TX_ROUNDS; round++) {
    radio.SpiStrobe(CC1101_SFTX);
    radio.SpiWriteBurstReg(CC1101_TXFIFO, (uint8_t *)buf, RF_TX_BUF_LEN);
    radio.SpiStrobe(CC1101_STX);

    bool roundDone = false;
    uint32_t start = millis();
    while (millis() - start < 300) {
      if ((radio.SpiReadStatus(CC1101_MARCSTATE) & 0x1F) == 0x01) {
        roundDone = true;
        break;
      }
      delayMicroseconds(200);
    }
    if (!roundDone) {
      completed = false;
      break;
    }
  }

  if (!completed) Serial.println("[rf] timeout aguardando TX terminar");
  txCount++;
  selfTxGuardUntil = millis() + SELF_TX_GUARD_MS;
  enterRx();
  return completed;
}

static bool transmitFifo(uint8_t fan, uint8_t command) {
  if (!state_validFan(fan)) return false;
  int index = genericIndex(command);
  if (index < 0) return false;

  uint8_t counter = state_get(fan).counter & 0x07;
  uint8_t state = STATE_TABLE[index][counter];
  uint8_t buf[RF_TX_BUF_LEN];
  buildFifoBuffer(0x00, 0x0A, command, state, buf);

  Serial.printf("[rf] FIFO TX fan=%u 00 0A %02X %02X (counter=%u)\n",
                (unsigned)fan, command, state, (unsigned)counter);
  if (!txBuffer(buf)) return false;

  // O contador e por ventilador. O fallback usa o mesmo contador do fan para
  // nao dessincronizar os proximos comandos aprendidos. A persistencia fica
  // para depois de todos os RFs (state_apply).
  state_setCounter(fan, (counter + 1) & 0x07, false);
  return true;
}

static bool transmitLearned(uint8_t fan, uint8_t command) {
  if (!state_validFan(fan) || command >= COMMAND_COUNT) return false;
  const FanConfig &cfg = state_config(fan);
  if (!cfg.hasCommand[command]) return false;

  uint8_t outgoing[4];
  memcpy(outgoing, cfg.command[command], sizeof(outgoing));

  uint8_t counter = state_get(fan).counter & 0x07;
  uint8_t state = (counter + cfg.stateOffset[command]) & 0x07;
  uint8_t encodedState = (outgoing[3] & 0x80) | (state << 4);
  uint8_t stateNibble = encodedState >> 4;
  outgoing[3] = encodedState | (stateNibble ^ ((outgoing[2] & 0x0F) ^ 0x07));

  uint8_t buf[RF_TX_BUF_LEN];
  buildFifoBuffer(outgoing[0], outgoing[1], outgoing[2], outgoing[3], buf);

  Serial.printf("[rf] learned TX fan=%u cmd=%s bytes=%02X %02X %02X %02X (counter=%u)\n",
                (unsigned)fan, state_commandName(command), outgoing[0], outgoing[1],
                outgoing[2], outgoing[3], (unsigned)counter);
  if (!txBuffer(buf)) return false;

  state_setCounter(fan, (counter + 1) & 0x07, false);
  return true;
}

static void enterRx() {
  if (!radioReady) return;

  detachInterrupt(digitalPinToInterrupt(PIN_GDO0));
  radio.setSidle();
  radio.setMHZ(RF_FREQ_MHZ);
  radio.SpiWriteReg(CC1101_FSCTRL0, 0x00);
  radio.setModulation(2);
  radio.setCCMode(false);
  radio.setDRate(RF_BAUD);
  radio.setPA(10);
  radio.SetRx();

  pinMode(PIN_GDO0, INPUT);
  lastState = digitalRead(PIN_GDO0);
  lastEdge = micros();
  noInterrupts();
  edgeCount = 0;
  capturing = false;
  frameReady = false;
  interrupts();
  attachInterrupt(digitalPinToInterrupt(PIN_GDO0), onRadioEdge, CHANGE);
}

void IRAM_ATTR onRadioEdge() {
  uint32_t now = micros();
  int state = digitalRead(PIN_GDO0);
  uint32_t duration = now - lastEdge;
  int previousLevel = lastState;
  lastState = state;
  lastEdge = now;

  if (duration > 5000) {
    if (capturing && edgeCount > 10) {
      frameReady = true;
      capturing = false;
    } else if (!frameReady) {
      edgeCount = 0;
      capturing = true;
    }
    return;
  }
  if (duration < 80 || !capturing || frameReady) return;
  if (edgeCount < MAX_EDGES) {
    durations[edgeCount] = duration;
    levels[edgeCount] = previousLevel;
    edgeCount = edgeCount + 1;
  }
}

static void processCapturedFrame() {
  if ((int32_t)(millis() - selfTxGuardUntil) < 0) {
    return;  // eco da propria transmissao
  }

  savedEdgeCount = edgeCount;
  noInterrupts();
  for (uint32_t i = 0; i < savedEdgeCount; i++) {
    savedDurations[i] = durations[i];
    savedLevels[i] = levels[i];
  }
  interrupts();

  if (savedEdgeCount < 10) return;

  uint32_t dataStart = 0;
  for (uint32_t i = 0; i < savedEdgeCount; i++) {
    if (savedDurations[i] > 3000) {
      dataStart = i + 1;
      break;
    }
  }

  uint8_t decoded[4] = {0, 0, 0, 0};
  uint8_t byteIndex = 0;
  uint8_t bitIndex = 0;
  uint8_t currentByte = 0;

  for (uint32_t i = dataStart; i + 1 < savedEdgeCount; i += 2) {
    uint32_t p1 = savedDurations[i];
    uint32_t p2 = savedDurations[i + 1];
    int bit = -1;
    if (p1 < 450 && p2 >= 450) {
      bit = 0;
    } else if (p1 >= 450 && p2 < 450) {
      bit = 1;
    }
    if (bit < 0) continue;

    currentByte = (currentByte << 1) | bit;
    bitIndex++;
    if (bitIndex == 8) {
      if (byteIndex < 4) {
        decoded[byteIndex] = currentByte;
        savedBytes[byteIndex] = currentByte;
        byteIndex++;
      }
      currentByte = 0;
      bitIndex = 0;
    }
  }

  if (byteIndex != 4) return;

  char hex[13];
  snprintf(hex, sizeof(hex), "%02X %02X %02X %02X", decoded[0], decoded[1],
           decoded[2], decoded[3]);
  lastFrameHex = hex;
  lastFrameAt = millis();
  rxCount++;

  uint8_t highNibble = decoded[3] >> 4;
  uint8_t expectedLow = highNibble ^ ((decoded[2] & 0x0F) ^ 0x07);
  if ((decoded[3] & 0x0F) != expectedLow) {
    rxChecksumFail++;
    Serial.printf("[rf] frame com checksum invalido: %s\n", hex);
    return;
  }

  if (learning) {
    memcpy(savedBytes, decoded, 4);
    learnFrameHex = hex;

    uint8_t counter = state_get(learnFan).counter & 0x07;
    int generic = genericIndex(decoded[2]);
    if (generic >= 0) {
      for (uint8_t s = 0; s < 8; s++) {
        if (STATE_TABLE[generic][s] == decoded[3]) {
          counter = (s + 1) & 0x07;
          break;
        }
      }
    } else {
      counter = (highNibble + 1) & 0x07;
    }

    uint8_t offset = (highNibble - ((counter + 7) & 0x07)) & 0x07;
    state_setCounter(learnFan, counter);
    state_setCommand(learnFan, learnCommand, decoded, offset);

    learning = false;
    learnFinished = true;
    learnSuccess = true;
    learnCancelled = false;
    Serial.printf("[rf] aprendizado OK fan=%u cmd=%s frame=%s offset=%u\n",
                  (unsigned)learnFan, state_commandName(learnCommand), hex,
                  (unsigned)offset);
    return;
  }

  // O contador so desempata quando ha um unico candidato forte. Se dois fans
  // compartilham o mesmo frame e nao ha como distinguir, ignora o frame em vez
  // de atualizar o ventilador errado.
  int strongFan = -1;
  int strongCommand = -1;
  int weakFan = -1;
  int weakCommand = -1;
  uint8_t strongCount = 0;
  uint8_t weakCount = 0;

  for (uint8_t fan = 0; fan < state_fanCount(); fan++) {
    const FanConfig &cfg = state_config(fan);
    uint8_t currentCounter = state_get(fan).counter & 0x07;
    for (uint8_t command = 0; command < COMMAND_COUNT; command++) {
      if (!cfg.hasCommand[command]) continue;
      if (decoded[0] != cfg.command[command][0] ||
          decoded[1] != cfg.command[command][1] ||
          decoded[2] != cfg.command[command][2]) {
        continue;
      }
      uint8_t stateIndex = (highNibble - cfg.stateOffset[command]) & 0x07;
      if (stateIndex == currentCounter) {
        strongCount++;
        if (strongFan < 0) {
          strongFan = fan;
          strongCommand = command;
        }
      } else {
        weakCount++;
        if (weakFan < 0) {
          weakFan = fan;
          weakCommand = command;
        }
      }
    }
  }

  int matchedFan = -1;
  int matchedCommand = -1;
  if (strongCount == 1) {
    matchedFan = strongFan;
    matchedCommand = strongCommand;
  } else if (strongCount > 1) {
    Serial.printf("[rf] frame ambiguo (%u candidatos fortes): %s\n",
                  (unsigned)strongCount, hex);
    return;
  } else if (weakCount == 1) {
    matchedFan = weakFan;
    matchedCommand = weakCommand;
  } else if (weakCount > 1) {
    Serial.printf("[rf] frame ambiguo (%u candidatos): %s\n",
                  (unsigned)weakCount, hex);
    return;
  } else {
    rxUnmatched++;
    Serial.printf("[rf] frame sem comando aprendido: %s\n", hex);
    return;
  }

  uint32_t now = millis();
  if (lastDebounceFan == (uint8_t)matchedFan &&
      lastDebounceCommand == (uint8_t)matchedCommand &&
      (uint32_t)(now - lastDebounceAt) < RF_RX_DEBOUNCE_MS) {
    return;
  }
  lastDebounceFan = (uint8_t)matchedFan;
  lastDebounceCommand = (uint8_t)matchedCommand;
  lastDebounceAt = now;

  uint8_t counter =
      ((highNibble - state_config(matchedFan).stateOffset[matchedCommand]) + 1) &
      0x07;

  rxMatched++;
  Serial.printf("[rf] RX fan=%u cmd=%s frame=%s\n", (unsigned)matchedFan,
                state_commandName(matchedCommand), hex);
  state_onRfCommand((uint8_t)matchedFan, (uint8_t)matchedCommand, counter);
}

void rf_begin() {
  radio.setSpiPin(PIN_SCK, PIN_MISO, PIN_MOSI, PIN_CSN);
  radio.setGDO(PIN_GDO0, PIN_GDO2);
  radio.Init();

  radioReady = radio.getCC1101();
  if (!radioReady) {
    Serial.println("[rf] CC1101 nao detectado; RF desabilitado");
    return;
  }

  radio.setMHZ(RF_FREQ_MHZ);
  radio.setModulation(2);
  radio.setCCMode(false);
  radio.setDRate(RF_BAUD);
  radio.setPA(10);
  enterRx();
  Serial.println("[rf] CC1101 OK");
}

void rf_loop() {
  if (!radioReady) return;

  if (learning && (int32_t)(millis() - learnDeadline) >= 0) {
    learning = false;
    learnFinished = true;
    learnSuccess = false;
    learnCancelled = false;
    learnFrameHex = "";
    Serial.println("[rf] aprendizado expirou");
  }

  bool ready;
  noInterrupts();
  ready = frameReady;
  interrupts();

  if (ready) {
    detachInterrupt(digitalPinToInterrupt(PIN_GDO0));
    processCapturedFrame();
    enterRx();
    return;
  }

  uint32_t last;
  uint32_t count;
  bool active;
  noInterrupts();
  last = lastEdge;
  count = edgeCount;
  active = capturing;
  interrupts();

  if (active && count > 10 && (micros() - last) > 25000) {
    noInterrupts();
    frameReady = true;
    capturing = false;
    interrupts();
  } else if (active && (micros() - last) > 2000000) {
    noInterrupts();
    capturing = false;
    edgeCount = 0;
    frameReady = false;
    interrupts();
  }
}

bool rf_radioOk() {
  return radioReady;
}

bool rf_isBusy() {
  return txInProgress || learning;
}

bool rf_isLearning() {
  return learning;
}

bool rf_transmitCommand(uint8_t fan, uint8_t command, String &error) {
  error = "";
  if (!radioReady) {
    error = "rf_unavailable";
    return false;
  }
  if (!state_validFan(fan) || command >= COMMAND_COUNT) {
    error = "invalid_device";
    return false;
  }
  if (learning) {
    error = "busy";
    return false;
  }
  if (txInProgress) {
    error = "busy";
    return false;
  }

  txInProgress = true;
  bool ok = false;
  const FanConfig &cfg = state_config(fan);

  if (cfg.hasCommand[command]) {
    ok = transmitLearned(fan, command);
    if (!ok) error = "rf_timeout";
  } else {
    switch (command) {
      case FAN_CMD_POWER:
        ok = transmitFifo(fan, RF_BYTE_POWER);
        break;
      case FAN_CMD_LIGHT:
        ok = transmitFifo(fan, RF_BYTE_OUTRO);
        break;
      case FAN_CMD_VEL_0:
        if (cfg.hasCommand[FAN_CMD_POWER]) {
          ok = transmitLearned(fan, FAN_CMD_POWER);
        } else {
          ok = transmitFifo(fan, RF_BYTE_POWER);
        }
        break;
      case FAN_CMD_VEL_1:
      case FAN_CMD_VEL_2:
      case FAN_CMD_VEL_3:
      case FAN_CMD_VEL_4:
      case FAN_CMD_VEL_5:
      case FAN_CMD_VEL_6:
        // Velocidade absoluta exige o botao correspondente aprendido.
        error = "not_supported";
        ok = false;
        break;
      case FAN_CMD_EXHAUST:
      case FAN_CMD_VENT:
        // Nao existe comando generico confiavel para direcao: o codigo OUTRO
        // e o mesmo usado pela luz. Exige vent/exhaust aprendidos.
        error = "not_supported";
        ok = false;
        break;
      default:
        error = "not_supported";
        ok = false;
        break;
    }
  }

  if (!ok && error.isEmpty()) error = "rf_timeout";
  txInProgress = false;
  return ok;
}

bool rf_startLearn(uint8_t fan, uint8_t command, const String &requestId,
                   String &error) {
  error = "";
  if (!radioReady) {
    error = "rf_unavailable";
    return false;
  }
  if (!state_validFan(fan) || command >= COMMAND_COUNT) {
    error = "invalid_device";
    return false;
  }
  if (txInProgress) {
    error = "busy";
    return false;
  }

  // Um novo learn.start substitui o aprendizado anterior.
  if (learning) {
    Serial.println("[rf] aprendizado anterior substituido");
  }

  learning = true;
  learnFinished = false;
  learnSuccess = false;
  learnCancelled = false;
  learnFan = fan;
  learnCommand = command;
  learnStartedAt = millis();
  learnDeadline = learnStartedAt + LEARN_TIMEOUT_MS;
  learnSerial++;
  learnRequestId = requestId;
  learnFrameHex = "";
  Serial.printf("[rf] aguardando aprendizado fan=%u cmd=%s\n", (unsigned)fan,
                state_commandName(command));
  return true;
}

void rf_cancelLearn() {
  if (!learning) return;
  learning = false;
  learnFinished = true;
  learnSuccess = false;
  learnCancelled = true;
  learnFrameHex = "";
}

LearnStatus rf_learnStatus() {
  LearnStatus status;
  status.active = learning;
  status.finished = learnFinished;
  status.success = learnSuccess;
  status.cancelled = learnCancelled;
  status.fan = learnFan;
  status.command = learnCommand;
  status.serial = learnSerial;
  status.startedAt = learnStartedAt;
  status.requestId = learnRequestId;
  status.frameHex = learnFrameHex;
  return status;
}

uint32_t rf_txCount() {
  return txCount;
}

uint32_t rf_rxCount() {
  return rxCount;
}

uint32_t rf_rxChecksumFail() {
  return rxChecksumFail;
}

uint32_t rf_rxUnmatched() {
  return rxUnmatched;
}

uint32_t rf_rxMatched() {
  return rxMatched;
}

String rf_lastFrameHex() {
  return lastFrameHex;
}

uint32_t rf_lastFrameAt() {
  return lastFrameAt;
}

bool rf_txGuardActive() {
  return (int32_t)(millis() - selfTxGuardUntil) < 0;
}
