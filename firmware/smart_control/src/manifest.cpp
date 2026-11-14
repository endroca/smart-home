#include "manifest.h"

#include "config.h"
#include "state.h"
#include "storage.h"
#include "wifi_provision.h"

// O contador do manifesto vive na NVS: ele precisa sobreviver a um reboot, já
// que é a chave com que o hub decide se relê a descoberta.
static uint32_t manifestVersion = 1;
static bool manifestLoaded = false;

static void ensureLoaded() {
  if (manifestLoaded) return;
  manifestVersion = storage_getUInt("manver", 1);
  if (manifestVersion == 0) manifestVersion = 1;
  manifestLoaded = true;
}

void manifest_build(JsonDocument &doc) {
  ensureLoaded();

  doc["schemaVersion"] = 2;
  doc["manifestVersion"] = manifestVersion;
  doc["thing"] = THING_NAME;
  doc["model"] = MODEL_NAME;
  doc["protocol"] = PROTOCOL_NAME;

  JsonObject firmware = doc["firmware"].to<JsonObject>();
  firmware["version"] = FIRMWARE_VERSION;
  firmware["build"] = __DATE__ " " __TIME__;

  JsonObject http = doc["http"].to<JsonObject>();
  http["mdns"] = wifi_mdns_name() + ".local";
  http["port"] = HTTP_PORT;

  // O device empurra estado, status e eventos para o hub (POST /events).
  // Declarar isso no manifesto deixa explícito o que o hub pode esperar.
  JsonObject events = doc["events"].to<JsonObject>();
  events["state"] = true;
  events["status"] = true;
  events["heartbeat"] = HUB_HEARTBEAT_MS / 1000;

  JsonArray actions = doc["actions"].to<JsonArray>();
  actions.add("fan.add");
  actions.add("fan.remove");
  actions.add("config.get");
  actions.add("config.set");
  actions.add("reboot");
  actions.add("ota.install");

  JsonArray devices = doc["devices"].to<JsonArray>();
  for (uint8_t i = 0; i < state_fanCount(); i++) {
    const FanConfig &cfg = state_config(i);
    JsonObject device = devices.add<JsonObject>();
    device["id"] = state_deviceId(i);
    device["type"] = "ceiling_fan";
    device["name"] = cfg.name;
     state_deviceId(i);

    JsonObject capabilities = device["capabilities"].to<JsonObject>();
    JsonObject power = capabilities["power"].to<JsonObject>();
    power["kind"] = "switch";
    power["stateful"] = true;
    power["writable"] = true;
    JsonObject light = capabilities["light"].to<JsonObject>();
    light["kind"] = "switch";
    light["stateful"] = true;
    light["writable"] = true;
    JsonObject speed = capabilities["speed"].to<JsonObject>();
    speed["kind"] = "number";
    speed["stateful"] = true;
    speed["writable"] = true;
    speed["min"] = 0;
    speed["max"] = 6;
    speed["step"] = 1;
    JsonObject direction = capabilities["direction"].to<JsonObject>();
    direction["kind"] = "enum";
    direction["stateful"] = true;
    direction["writable"] = true;
    JsonArray values = direction["values"].to<JsonArray>();
    values.add("forward");
    values.add("reverse");

    JsonArray deviceActions = device["actions"].to<JsonArray>();
    deviceActions.add("state.correct");
    deviceActions.add("learn.start");
    deviceActions.add("learn.cancel");
    deviceActions.add("fan.rename");
  }
}

uint32_t manifest_version() {
  ensureLoaded();
  return manifestVersion;
}

void manifest_bump() {
  ensureLoaded();
  manifestVersion++;
  storage_putUInt("manver", manifestVersion);
}
