#include "state.h"

#include <Preferences.h>
#include <cstring>

#include "rf_ventisol.h"
#include "storage.h"
#include "system.h"

static FanConfig configs[MAX_FANS];
static FanState states[MAX_FANS];
static uint8_t fanCount = 0;
static StateChangedHandler changedHandler = nullptr;

static const char *COMMAND_NAMES[COMMAND_COUNT] = {
    "power", "light", "vel_0", "vel_1", "vel_2", "vel_3",
    "vel_4", "vel_5", "vel_6", "exhaust", "vent",
};

static void configKey(uint8_t fan, char *out, size_t len) {
  snprintf(out, len, "cfg%u", (unsigned)fan);
}

static void stateKey(uint8_t fan, char *out, size_t len) {
  snprintf(out, len, "st%u", (unsigned)fan);
}

static void defaultConfig(uint8_t fan) {
  memset(&configs[fan], 0, sizeof(FanConfig));
  snprintf(configs[fan].name, FAN_NAME_LEN, "Ventilador %u", (unsigned)(fan + 1));
}

static void defaultState(uint8_t fan) {
  memset(&states[fan], 0, sizeof(FanState));
  states[fan].power = false;
  states[fan].light = false;
  states[fan].speed = 0;
  states[fan].lastSpeed = 0;
  states[fan].direction = FanDirection::FORWARD;
  states[fan].counter = 0;
  states[fan].updatedAt = 0;
  states[fan].source = FanSource::BOOT;
}

static void sanitizeConfig(uint8_t fan) {
  configs[fan].name[FAN_NAME_LEN - 1] = '\0';
  if (configs[fan].name[0] == '\0') {
    snprintf(configs[fan].name, FAN_NAME_LEN, "Ventilador %u", (unsigned)(fan + 1));
  }
  for (uint8_t c = 0; c < COMMAND_COUNT; c++) {
    configs[fan].hasCommand[c] = configs[fan].hasCommand[c] ? 1 : 0;
    if (configs[fan].stateOffset[c] > 7) configs[fan].stateOffset[c] = 0;
  }
}

static void sanitizeState(uint8_t fan) {
  if (states[fan].speed > 6) states[fan].speed = 0;
  if (states[fan].lastSpeed > 6) states[fan].lastSpeed = 0;
  if (states[fan].counter > 7) states[fan].counter = 0;
  if (states[fan].direction != FanDirection::FORWARD &&
      states[fan].direction != FanDirection::REVERSE) {
    states[fan].direction = FanDirection::FORWARD;
  }
  if (states[fan].source > FanSource::CORRECTION) {
    states[fan].source = FanSource::BOOT;
  }
  if (!states[fan].power) states[fan].speed = 0;
  if (states[fan].speed > 0) states[fan].lastSpeed = states[fan].speed;
}

// Layout da NVS do firmware legacy (rf_project/tuya_project, namespace
// "ventisol"). Usado apenas para a importacao unica no primeiro boot.
struct LegacyFanData {
  char name[25];
  uint8_t counter;
  uint8_t hasCommand[COMMAND_COUNT];
  uint8_t stateOffset[COMMAND_COUNT];
  uint8_t command[COMMAND_COUNT][4];
};

static_assert(sizeof(LegacyFanData) == 92,
              "layout inesperado do FanData legacy");

// Le a configuracao aprendida do firmware antigo e converte para o formato
// novo. O namespace "ventisol" e mantido intacto, entao voltar para o firmware
// legacy continua funcionando e o reset de fabrica e quem o apaga.
static bool importLegacyConfig() {
  Preferences legacy;
  if (!legacy.begin("ventisol", true)) return false;

  uint8_t count = legacy.getUChar("count", 0);
  if (count == 0 || count > MAX_FANS) {
    legacy.end();
    return false;
  }

  uint8_t imported = 0;
  for (uint8_t i = 0; i < count; i++) {
    char key[8];
    snprintf(key, sizeof(key), "fan%u", (unsigned)i);
    LegacyFanData old;
    if (legacy.getBytes(key, &old, sizeof(old)) != sizeof(old)) continue;

    FanConfig &cfg = configs[imported];
    memset(&cfg, 0, sizeof(cfg));
    memcpy(cfg.name, old.name, sizeof(old.name));
    cfg.name[FAN_NAME_LEN - 1] = '\0';
    for (uint8_t c = 0; c < COMMAND_COUNT; c++) {
      cfg.hasCommand[c] = old.hasCommand[c] ? 1 : 0;
      cfg.stateOffset[c] = old.stateOffset[c] & 0x07;
      memcpy(cfg.command[c], old.command[c], sizeof(cfg.command[c]));
    }

    defaultState(imported);
    states[imported].counter = old.counter & 0x07;
    sanitizeConfig(imported);
    sanitizeState(imported);
    imported++;
  }
  legacy.end();

  if (imported == 0) return false;
  fanCount = imported;
  state_saveAll();
  Serial.printf("[state] %u ventilador(es) importado(s) da NVS legacy\n",
                (unsigned)fanCount);
  return true;
}

void state_begin() {
  memset(configs, 0, sizeof(configs));
  memset(states, 0, sizeof(states));
  fanCount = 0;

  uint8_t version = storage_getUChar("cfg_ver", 0);
  uint8_t count = storage_getUChar("count", 0);
  bool reset = (version != NVS_CONFIG_VERSION) || (count == 0) || (count > MAX_FANS);

  if (reset) {
    if (importLegacyConfig()) return;
    for (uint8_t i = 0; i < MAX_FANS; i++) {
      char key[8];
      configKey(i, key, sizeof(key));
      storage_remove(key);
      stateKey(i, key, sizeof(key));
      storage_remove(key);
    }
    fanCount = 1;
    defaultConfig(0);
    defaultState(0);
    state_saveAll();
    Serial.println("[state] configuracao inicial criada");
    return;
  }

  fanCount = count;
  for (uint8_t i = 0; i < fanCount; i++) {
    char key[8];
    configKey(i, key, sizeof(key));
    FanConfig cfg;
    if (storage_getBytes(key, &cfg, sizeof(cfg)) == sizeof(cfg)) {
      configs[i] = cfg;
      sanitizeConfig(i);
    } else {
      defaultConfig(i);
    }

    stateKey(i, key, sizeof(key));
    FanState st;
    if (storage_getBytes(key, &st, sizeof(st)) == sizeof(st)) {
      states[i] = st;
      sanitizeState(i);
    } else {
      defaultState(i);
    }
  }
  Serial.printf("[state] %u ventilador(es) carregado(s)\n", (unsigned)fanCount);
}

void state_setChangedHandler(StateChangedHandler handler) {
  changedHandler = handler;
}

uint8_t state_fanCount() {
  return fanCount;
}

bool state_validFan(uint8_t fan) {
  return fan < fanCount;
}

const FanConfig &state_config(uint8_t fan) {
  return configs[fan];
}

const FanState &state_get(uint8_t fan) {
  return states[fan];
}

String state_deviceId(uint8_t fan) {
  return String("fan") + String(fan);
}

uint8_t state_findById(const String &deviceId) {
  if (!deviceId.startsWith("fan")) return 255;
  String suffix = deviceId.substring(3);
  if (suffix.length() == 0) return 255;
  for (size_t i = 0; i < suffix.length(); i++) {
    if (!isDigit(suffix[i])) return 255;
  }
  long value = suffix.toInt();
  if (value < 0 || value >= fanCount) return 255;
  return (uint8_t)value;
}

const char *state_commandName(uint8_t command) {
  if (command >= COMMAND_COUNT) return "unknown";
  return COMMAND_NAMES[command];
}

int state_commandFromName(const String &name) {
  for (uint8_t i = 0; i < COMMAND_COUNT; i++) {
    if (name.equals(COMMAND_NAMES[i])) return i;
  }
  return -1;
}

const char *state_sourceName(FanSource source) {
  switch (source) {
    case FanSource::HUB:
      return "hub";
    case FanSource::HTTP:
      return "http";
    case FanSource::RF_REMOTE:
      return "rf_remote";
    case FanSource::CORRECTION:
      return "correction";
    case FanSource::BOOT:
    default:
      return "boot";
  }
}

bool state_sourceFromName(const String &name, FanSource &source) {
  if (name.equals("hub") || name.equals("mqtt")) {
    // "mqtt" e apenas um valor legado persistido na NVS; hoje equivale a hub.
    source = FanSource::HUB;
  } else if (name.equals("http")) {
    source = FanSource::HTTP;
  } else if (name.equals("rf_remote")) {
    source = FanSource::RF_REMOTE;
  } else if (name.equals("correction")) {
    source = FanSource::CORRECTION;
  } else if (name.equals("boot")) {
    source = FanSource::BOOT;
  } else {
    return false;
  }
  return true;
}

const char *state_directionName(FanDirection direction) {
  return direction == FanDirection::REVERSE ? "reverse" : "forward";
}

bool state_directionFromName(const String &name, FanDirection &direction) {
  if (name.equals("forward")) {
    direction = FanDirection::FORWARD;
    return true;
  }
  if (name.equals("reverse")) {
    direction = FanDirection::REVERSE;
    return true;
  }
  return false;
}

bool state_parseTarget(JsonObjectConst obj, FanTarget &target) {
  target = FanTarget{};
  if (obj.isNull()) return false;

  bool any = false;
  if (!obj["power"].isNull()) {
    target.hasPower = true;
    target.power = obj["power"].as<bool>();
    any = true;
  }
  if (!obj["light"].isNull()) {
    target.hasLight = true;
    target.light = obj["light"].as<bool>();
    any = true;
  }
  if (!obj["speed"].isNull()) {
    int speed = obj["speed"].as<int>();
    if (speed < 0 || speed > 6) return false;
    target.hasSpeed = true;
    target.speed = (uint8_t)speed;
    any = true;
  }
  if (!obj["direction"].isNull()) {
    FanDirection direction;
    if (!state_directionFromName(obj["direction"].as<String>(), direction)) {
      return false;
    }
    target.hasDirection = true;
    target.direction = direction;
    any = true;
  }
  return any;
}

static void notifyChanged(uint8_t fan, bool transmittedRf) {
  if (changedHandler) changedHandler(fan, transmittedRf);
}

bool state_apply(uint8_t fan, const FanTarget &target, FanSource source,
                 String &error, uint16_t *sentMask, bool force) {
  error = "";
  if (sentMask) *sentMask = 0;
  if (!state_validFan(fan)) {
    error = "invalid_device";
    return false;
  }

  FanState current = states[fan];
  FanState next = current;

  if (target.hasPower) next.power = target.power;
  if (target.hasLight) next.light = target.light;
  if (target.hasSpeed) next.speed = target.speed;
  if (target.hasDirection) next.direction = target.direction;

  if (next.speed > 6) next.speed = 6;

  if (target.hasSpeed) {
    if (next.speed == 0) {
      next.power = false;
    } else {
      next.power = true;
      next.lastSpeed = next.speed;
    }
  }
  if (target.hasPower) {
    if (!next.power) {
      next.speed = 0;
    } else if (next.speed == 0) {
      next.speed = next.lastSpeed > 0 ? next.lastSpeed : 1;
    }
  }
  if (!next.power) next.speed = 0;
  if (next.speed > 0) next.lastSpeed = next.speed;

  bool powerChanged = next.power != current.power;
  bool lightChanged = next.light != current.light;
  bool speedChanged = next.power && next.speed != current.speed;
  bool dirChanged = next.direction != current.direction;

  // Um toque explicito (force) reenvia o comando mesmo sem mudanca de estado —
  // por exemplo, tocar de novo na velocidade atual. So forca o que tem comando
  // aprendido.
  if (force && source != FanSource::CORRECTION) {
    const FanConfig &cfg = state_config(fan);
    if (target.hasLight && cfg.hasCommand[FAN_CMD_LIGHT]) {
      lightChanged = true;
    }
    if (target.hasSpeed && next.speed > 0 &&
        cfg.hasCommand[FAN_CMD_VEL_0 + next.speed]) {
      speedChanged = true;
    }
    if (target.hasDirection) {
      uint8_t dirCmd = next.direction == FanDirection::FORWARD ? FAN_CMD_VENT
                                                               : FAN_CMD_EXHAUST;
      if (cfg.hasCommand[dirCmd]) dirChanged = true;
    }
    if (target.hasPower && !next.power) {
      powerChanged = true;
    }
  }

  bool changed = powerChanged || lightChanged || speedChanged || dirChanged;

  if (!changed) {
    if (source == FanSource::CORRECTION) {
      states[fan].source = source;
      states[fan].updatedAt = system_now_epoch();
      state_saveState(fan);
      notifyChanged(fan, false);
    }
    return true;
  }

  // speed:0 pode significar "usar o botao vel_0" ou "desligar". Se o usuario
  // nao pediu power:false explicitamente e o vel_0 foi aprendido, usa vel_0.
  bool explicitPowerOff = target.hasPower && !target.power;
  bool speedZeroRequested = target.hasSpeed && target.speed == 0;
  bool useVelZero = powerChanged && !next.power && speedZeroRequested &&
                    !explicitPowerOff &&
                    state_config(fan).hasCommand[FAN_CMD_VEL_0];

  // POWER so pode ser enviado em um desligamento explicito (target power:false).
  // Para ligar/mudar velocidade usamos o botao de velocidade aprendido.
  bool speedCommandLearned =
      next.speed > 0 && state_config(fan).hasCommand[FAN_CMD_VEL_0 + next.speed];
  if (speedChanged && !speedCommandLearned) {
    // Sem o botao absoluto aprendido nao ha como chegar na velocidade pedida;
    // falha antes de qualquer RF para nao ligar o motor pela metade.
    error = "not_supported";
    return false;
  }

  uint8_t commands[4];
  uint8_t commandCount = 0;
  if (powerChanged) {
    if (explicitPowerOff) {
      commands[commandCount++] = FAN_CMD_POWER;
    } else if (useVelZero) {
      commands[commandCount++] = FAN_CMD_VEL_0;
    } else if (!(speedChanged && speedCommandLearned)) {
      // Nao ha como ligar/desligar sem um comando de velocidade ou desligar
      // explicito; nunca cai no POWER automaticamente.
      error = "not_supported";
      return false;
    }
  }
  if (lightChanged) commands[commandCount++] = FAN_CMD_LIGHT;
  if (speedChanged) commands[commandCount++] = FAN_CMD_VEL_0 + next.speed;
  if (dirChanged) {
    commands[commandCount++] =
        next.direction == FanDirection::FORWARD ? FAN_CMD_VENT : FAN_CMD_EXHAUST;
  }

  bool ok = true;
  if (source != FanSource::CORRECTION) {
    // Prioridade: transmitir todo o RF primeiro. A NVS e o push ao hub so
    // sao atualizados depois, com o estado final.
    for (uint8_t i = 0; i < commandCount; i++) {
      uint8_t command = commands[i];
      String rfError;
      if (rf_transmitCommand(fan, command, rfError)) {
        if (sentMask) *sentMask |= (uint16_t)(1u << command);
        continue;
      }
      ok = false;
      if (error.isEmpty()) error = rfError;
      switch (command) {
        case FAN_CMD_POWER:
        case FAN_CMD_VEL_0:
          next.power = current.power;
          next.speed = current.speed;
          next.lastSpeed = current.lastSpeed;
          break;
        case FAN_CMD_LIGHT:
          next.light = current.light;
          break;
        case FAN_CMD_EXHAUST:
        case FAN_CMD_VENT:
          next.direction = current.direction;
          break;
        default:
          next.speed = current.speed;
          next.lastSpeed = current.lastSpeed;
          break;
      }
    }
  }

  // Comportamento nativo do ventilador: o frame de direcao (vent/exhaust)
  // tambem liga o motor na velocidade 1 quando ele estava desligado. O RF ja
  // faz isso no aparelho; aqui so espelhamos no estado, para o reported (e o
  // hub) refletirem o que o ventilador realmente fez. Uma correcao (sem RF)
  // nao aciona nada, entao nao entra aqui.
  if (ok && dirChanged && source != FanSource::CORRECTION &&
      !target.hasPower && !current.power) {
    next.power = true;
    next.speed = 1;
    next.lastSpeed = 1;
  }

  // Nativo: o comando POWER ("desligar") desliga tudo, motor e luz. Nao ha um
  // frame de luz separado nesse caso, entao espelhamos a luz no estado.
  if (ok && explicitPowerOff && source != FanSource::CORRECTION) {
    next.light = false;
  }

  // Se o POWER funcionou mas o VEL_N falhou, o fan ficou ligado. Mantem o
  // estado consistente usando a ultima velocidade conhecida.
  if (next.power && next.speed == 0) {
    next.speed = next.lastSpeed > 0 ? next.lastSpeed : 1;
  }
  next.counter = states[fan].counter;
  next.lastSpeed = next.speed > 0 ? next.speed : next.lastSpeed;
  next.updatedAt = system_now_epoch();
  next.source = source;

  bool finalChanged = next.power != current.power || next.light != current.light ||
                      next.speed != current.speed ||
                      next.direction != current.direction;
  if (finalChanged) {
    states[fan] = next;
    state_saveState(fan);
    notifyChanged(fan, source != FanSource::CORRECTION && commandCount > 0);
  }
  return ok;
}

bool state_correct(uint8_t fan, const FanTarget &target, String &error) {
  return state_apply(fan, target, FanSource::CORRECTION, error);
}

void state_onRfCommand(uint8_t fan, uint8_t command, uint8_t counter) {
  if (!state_validFan(fan) || command >= COMMAND_COUNT) return;

  FanState st = states[fan];
  switch (command) {
    case FAN_CMD_POWER:
      st.power = !st.power;
      if (!st.power) {
        st.speed = 0;
        st.light = false;  // nativo: POWER desliga tudo
      } else if (st.speed == 0) {
        st.speed = st.lastSpeed > 0 ? st.lastSpeed : 1;
      }
      break;
    case FAN_CMD_LIGHT:
      st.light = !st.light;
      break;
    case FAN_CMD_VEL_0:
    case FAN_CMD_VEL_1:
    case FAN_CMD_VEL_2:
    case FAN_CMD_VEL_3:
    case FAN_CMD_VEL_4:
    case FAN_CMD_VEL_5:
    case FAN_CMD_VEL_6: {
      uint8_t speed = (uint8_t)(command - FAN_CMD_VEL_0);
      st.speed = speed;
      st.power = speed > 0;
      if (speed > 0) st.lastSpeed = speed;
      break;
    }
    case FAN_CMD_EXHAUST:
      st.direction = FanDirection::REVERSE;
      // Nativo: a direcao tambem liga o motor na velocidade 1 se estava off.
      if (!st.power) {
        st.power = true;
        st.speed = 1;
        st.lastSpeed = 1;
      }
      break;
    case FAN_CMD_VENT:
      st.direction = FanDirection::FORWARD;
      if (!st.power) {
        st.power = true;
        st.speed = 1;
        st.lastSpeed = 1;
      }
      break;
    default:
      return;
  }

  if (counter <= 7) st.counter = counter;
  st.updatedAt = system_now_epoch();
  st.source = FanSource::RF_REMOTE;
  states[fan] = st;
  state_saveState(fan);
  notifyChanged(fan, false);
}

bool state_addFan(uint8_t &newFan, String &error) {
  error = "";
  if (fanCount >= MAX_FANS) {
    error = "fan_limit";
    return false;
  }
  uint8_t index = fanCount;
  defaultConfig(index);
  defaultState(index);
  fanCount++;
  state_saveAll();
  newFan = index;
  return true;
}

bool state_removeFan(uint8_t fan, String &error) {
  error = "";
  if (!state_validFan(fan)) {
    error = "invalid_device";
    return false;
  }
  if (fanCount <= 1) {
    error = "last_fan";
    return false;
  }
  for (uint8_t i = fan; i + 1 < fanCount; i++) {
    configs[i] = configs[i + 1];
    states[i] = states[i + 1];
  }
  fanCount--;
  state_saveAll();
  return true;
}

bool state_renameFan(uint8_t fan, const String &name, String &error) {
  error = "";
  if (!state_validFan(fan)) {
    error = "invalid_device";
    return false;
  }
  if (name.isEmpty() || name.length() >= FAN_NAME_LEN) {
    error = "invalid_name";
    return false;
  }
  memset(configs[fan].name, 0, FAN_NAME_LEN);
  strncpy(configs[fan].name, name.c_str(), FAN_NAME_LEN - 1);
  state_saveConfig(fan);
  return true;
}

void state_setCounter(uint8_t fan, uint8_t counter, bool persist) {
  if (!state_validFan(fan)) return;
  if (counter > 7) counter = 0;
  if (states[fan].counter == counter) return;
  states[fan].counter = counter;
  // Durante uma sequencia de RF a persistencia e adiada para depois de todos
  // os comandos; o RF tem prioridade sobre a gravacao na NVS.
  if (persist) state_saveState(fan);
}

void state_setCommand(uint8_t fan, uint8_t command, const uint8_t data[4],
                      uint8_t stateOffset) {
  if (!state_validFan(fan) || command >= COMMAND_COUNT) return;
  memcpy(configs[fan].command[command], data, 4);
  configs[fan].hasCommand[command] = 1;
  configs[fan].stateOffset[command] = stateOffset & 0x07;
  state_saveConfig(fan);
}

void state_saveConfig(uint8_t fan) {
  if (!state_validFan(fan)) return;
  char key[8];
  configKey(fan, key, sizeof(key));
  if (!storage_putBytes(key, &configs[fan], sizeof(FanConfig))) {
    Serial.printf("[state] falha ao gravar config fan=%u no NVS\n",
                  (unsigned)fan);
  }
}

void state_saveState(uint8_t fan) {
  if (!state_validFan(fan)) return;
  char key[8];
  stateKey(fan, key, sizeof(key));
  if (!storage_putBytes(key, &states[fan], sizeof(FanState))) {
    Serial.printf("[state] falha ao gravar estado fan=%u no NVS\n",
                  (unsigned)fan);
  }
}

void state_saveAll() {
  storage_putUChar("cfg_ver", NVS_CONFIG_VERSION);
  storage_putUChar("count", fanCount);
  bool ok = true;
  char key[8];
  for (uint8_t i = 0; i < fanCount; i++) {
    configKey(i, key, sizeof(key));
    ok = storage_putBytes(key, &configs[i], sizeof(FanConfig)) && ok;
    stateKey(i, key, sizeof(key));
    ok = storage_putBytes(key, &states[i], sizeof(FanState)) && ok;
  }
  for (uint8_t i = fanCount; i < MAX_FANS; i++) {
    configKey(i, key, sizeof(key));
    storage_remove(key);
    stateKey(i, key, sizeof(key));
    storage_remove(key);
  }
  if (!ok) Serial.println("[state] falha ao gravar configuracao no NVS");
}

void state_reset() {
  for (uint8_t i = 0; i < MAX_FANS; i++) {
    char key[8];
    configKey(i, key, sizeof(key));
    storage_remove(key);
    stateKey(i, key, sizeof(key));
    storage_remove(key);
  }
  storage_remove("count");
  storage_remove("cfg_ver");
  fanCount = 1;
  defaultConfig(0);
  defaultState(0);
  state_saveAll();
}

void state_configToJson(JsonObject target) {
  target["version"] = NVS_CONFIG_VERSION;
  JsonArray fans = target["fans"].to<JsonArray>();
  for (uint8_t i = 0; i < fanCount; i++) {
    const FanConfig &cfg = configs[i];
    const FanState &st = states[i];
    JsonObject fan = fans.add<JsonObject>();
    fan["id"] = state_deviceId(i);
    fan["name"] = cfg.name;

    JsonObject state = fan["state"].to<JsonObject>();
    state["power"] = st.power;
    state["light"] = st.light;
    state["speed"] = st.speed;
    state["lastSpeed"] = st.lastSpeed;
    state["direction"] = state_directionName(st.direction);
    state["counter"] = st.counter;
    state["updatedAt"] = st.updatedAt;
    state["source"] = state_sourceName(st.source);

    JsonObject commands = fan["commands"].to<JsonObject>();
    for (uint8_t c = 0; c < COMMAND_COUNT; c++) {
      if (!cfg.hasCommand[c]) continue;
      JsonObject command = commands[state_commandName(c)].to<JsonObject>();
      JsonArray data = command["data"].to<JsonArray>();
      for (uint8_t b = 0; b < 4; b++) data.add(cfg.command[c][b]);
      command["offset"] = cfg.stateOffset[c];
    }
  }
}

void state_configToLegacyJson(JsonObject target) {
  target["version"] = 1;
  JsonArray fans = target["fans"].to<JsonArray>();
  for (uint8_t i = 0; i < fanCount; i++) {
    const FanConfig &cfg = configs[i];
    JsonObject fan = fans.add<JsonObject>();
    fan["name"] = cfg.name;
    fan["counter"] = states[i].counter;
    JsonArray commands = fan["commands"].to<JsonArray>();
    for (uint8_t c = 0; c < COMMAND_COUNT; c++) {
      if (!cfg.hasCommand[c]) continue;
      JsonObject command = commands.add<JsonObject>();
      command["id"] = state_commandName(c);
      JsonArray data = command["data"].to<JsonArray>();
      for (uint8_t b = 0; b < 4; b++) data.add(cfg.command[c][b]);
      command["offset"] = cfg.stateOffset[c];
    }
  }
}

static bool parseCommandData(JsonObjectConst entry, uint8_t bytes[4],
                             uint8_t &offset) {
  if (entry.isNull()) return false;
  JsonArrayConst data = entry["data"].as<JsonArrayConst>();
  if (data.isNull() || data.size() != 4) return false;
  for (uint8_t b = 0; b < 4; b++) {
    int value = data[b] | -1;
    if (value < 0 || value > 255) return false;
    bytes[b] = (uint8_t)value;
  }
  int value = entry["offset"] | 0;
  if (value < 0 || value > 7) return false;
  offset = (uint8_t)value;
  return true;
}

bool state_configFromJson(JsonObjectConst source, String &error) {
  error = "";
  if (!source["fans"].is<JsonArray>()) {
    error = "invalid_config";
    return false;
  }
  JsonArrayConst fans = source["fans"].as<JsonArrayConst>();
  if (fans.size() < 1 || fans.size() > MAX_FANS) {
    error = "invalid_fan_count";
    return false;
  }

  FanConfig tempConfigs[MAX_FANS];
  FanState tempStates[MAX_FANS];
  memset(tempConfigs, 0, sizeof(tempConfigs));
  memset(tempStates, 0, sizeof(tempStates));
  uint8_t index = 0;

  for (JsonObjectConst fan : fans) {
    if (fan.isNull()) {
      error = "invalid_config";
      return false;
    }
    FanConfig &cfg = tempConfigs[index];
    FanState &st = tempStates[index];
    memset(&cfg, 0, sizeof(FanConfig));
    memset(&st, 0, sizeof(FanState));

    const char *name = fan["name"] | "";
    if (strlen(name) > FAN_NAME_LEN - 1) {
      error = "invalid_name";
      return false;
    }
    strncpy(cfg.name, name, FAN_NAME_LEN - 1);
    if (cfg.name[0] == '\0') {
      snprintf(cfg.name, FAN_NAME_LEN, "Ventilador %u", (unsigned)(index + 1));
    }

    JsonObjectConst state = fan["state"].as<JsonObjectConst>();
    st.power = state["power"] | false;
    st.light = state["light"] | false;
    int speed = state["speed"] | 0;
    if (speed < 0) speed = 0;
    if (speed > 6) speed = 6;
    st.speed = (uint8_t)speed;
    int lastSpeed = state["lastSpeed"] | speed;
    if (lastSpeed < 0) lastSpeed = 0;
    if (lastSpeed > 6) lastSpeed = 6;
    st.lastSpeed = (uint8_t)lastSpeed;
    if (!st.power) {
      st.speed = 0;
    } else if (st.speed == 0) {
      st.speed = st.lastSpeed > 0 ? st.lastSpeed : 1;
    }
    if (st.speed > 0) {
      st.power = true;
      st.lastSpeed = st.speed;
    }
    const char *direction = state["direction"] | "forward";
    state_directionFromName(String(direction), st.direction);
    int counter = fan["counter"] | 0;  // export do firmware legacy
    if (!state["counter"].isNull()) counter = state["counter"] | 0;
    if (counter < 0 || counter > 7) counter = 0;
    st.counter = (uint8_t)counter;
    st.updatedAt = state["updatedAt"] | 0;
    const char *source = state["source"] | "boot";
    state_sourceFromName(String(source), st.source);

    // Formato novo: commands e um objeto { "power": { data, offset } }.
    // Formato legacy (export do rf_project/tuya_project): commands e um array
    // [{ "id": "power", "data": [...], "offset": 0 }].
    JsonObjectConst commands = fan["commands"].as<JsonObjectConst>();
    JsonArrayConst commandList = fan["commands"].as<JsonArrayConst>();
    if (!commands.isNull()) {
      for (JsonPairConst kv : commands) {
        int command = state_commandFromName(String(kv.key().c_str()));
        if (command < 0) continue;
        uint8_t bytes[4];
        uint8_t offset = 0;
        if (!parseCommandData(kv.value().as<JsonObjectConst>(), bytes, offset)) {
          error = "invalid_config";
          return false;
        }
        cfg.hasCommand[command] = 1;
        cfg.stateOffset[command] = offset;
        memcpy(cfg.command[command], bytes, 4);
      }
    } else if (!commandList.isNull()) {
      for (JsonObjectConst entry : commandList) {
        int command = state_commandFromName(entry["id"] | "");
        if (command < 0) continue;
        uint8_t bytes[4];
        uint8_t offset = 0;
        if (!parseCommandData(entry, bytes, offset)) {
          error = "invalid_config";
          return false;
        }
        cfg.hasCommand[command] = 1;
        cfg.stateOffset[command] = offset;
        memcpy(cfg.command[command], bytes, 4);
      }
    }
    index++;
  }

  fanCount = index;
  for (uint8_t i = 0; i < fanCount; i++) {
    configs[i] = tempConfigs[i];
    states[i] = tempStates[i];
  }
  state_saveAll();
  return true;
}
