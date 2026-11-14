# Firmware smart_control (ESP32 + CC1101) - atalhos de build e gravacao.
# Uso: make help        (nesta pasta)
#      make -C firmware/smart_control build   (na raiz do repositorio)
#
# Cada alvo apenas chama o PlatformIO; a configuracao real fica no
# platformio.ini ao lado. Requer o `pio` no PATH.

# Caminho absoluto desta pasta, para funcionar de qualquer diretorio.
MK_DIR  := $(patsubst %/,%,$(dir $(realpath $(lastword $(MAKEFILE_LIST)))))
REPO    := $(realpath $(MK_DIR)/../..)

PIO     ?= pio
PIO_ENV ?= esp32dev
BAUD    ?= 115200

# Porta serial. Vazio = autodetecta. Ex.: make upload PORT=/dev/ttyUSB0
PORT        ?=
UPLOAD_PORT := $(if $(PORT),--upload-port $(PORT),)
MONITOR_PORT := $(if $(PORT),-p $(PORT),)

# Versao embutida no binario (FIRMWARE_VERSION em src/config.h), usada no nome
# do arquivo publicado para o OTA: <nome>-<versao>.bin.
VERSION ?= $(shell sed -n 's/.*#define FIRMWARE_VERSION "\([^"]*\)".*/\1/p' $(MK_DIR)/src/config.h | head -n1)
VERSION := $(if $(VERSION),$(VERSION),0.0.0)

# Diretorio de firmware do hub de onde o OTA le os .bin.
HUB_FW_DIR ?= $(REPO)/hub/firmware

BIN     := $(MK_DIR)/.pio/build/$(PIO_ENV)/firmware.bin

.DEFAULT_GOAL := help
.PHONY: help build upload monitor clean erase css compiledb publish check

help: ## mostra os alvos disponiveis
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-11s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## compila o firmware (pio run)
	$(PIO) run -d $(MK_DIR) -e $(PIO_ENV)

upload: ## compila e grava no ESP32 pela serial
	$(PIO) run -d $(MK_DIR) -e $(PIO_ENV) -t upload $(UPLOAD_PORT)

monitor: ## abre o monitor serial (BAUD=115200)
	$(PIO) device monitor -d $(MK_DIR) -b $(BAUD) $(MONITOR_PORT)

clean: ## limpa os artefatos de build
	$(PIO) run -d $(MK_DIR) -t clean

erase: ## APAGA a flash inteira, inclusive a NVS (Wi-Fi, token, estado)
	@echo "ATENCAO: isto apaga a NVS do device (Wi-Fi, token, hub_url e estado)."
	$(PIO) run -d $(MK_DIR) -e $(PIO_ENV) -t erase

css: ## regenera src/web_ui_css.h (precisa de npm; so ao mudar a UI)
	$(REPO)/tools/gen-web-css.sh

compiledb: ## gera compile_commands.json para o editor/clangd
	$(PIO) run -d $(MK_DIR) -e $(PIO_ENV) -t compiledb

publish: build ## copia o .bin para o OTA do hub (<nome>-<versao>.bin)
	@mkdir -p $(HUB_FW_DIR)
	cp $(BIN) $(HUB_FW_DIR)/smart_control-$(VERSION).bin
	@echo "publicado: $(HUB_FW_DIR)/smart_control-$(VERSION).bin"

check: build ## validacao (espelha o CI): compila o firmware
	@echo "check ok - binario: $(BIN)"
