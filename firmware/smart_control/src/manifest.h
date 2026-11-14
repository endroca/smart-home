#pragma once

#include <Arduino.h>
#include <ArduinoJson.h>

// Manifesto de descoberta do device (schema contracts/manifest.schema.json).
//
// Este é o contrato entre o gateway e o hub: o documento é servido em HTTP
// (`GET /api/manifest`) e anunciado por mDNS. Ele não depende de nenhum
// transporte — é a descrição do device e dos sub-devices que ele expõe.
//
// A versão do manifesto (`manifestVersion`) é a chave de invalidação: quando
// ela muda, o hub relê o documento.

// manifest_build fills doc with the discovery document.
void manifest_build(JsonDocument &doc);

// manifest_version returns the current manifest version.
uint32_t manifest_version();

// manifest_bump increments the manifest version and persists it.
void manifest_bump();
