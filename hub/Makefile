# Hub smart_home - atalhos de desenvolvimento e operacao.
# Uso: make help

GO             ?= go
BINARY         ?= smart-home-hub
CONFIG         ?= config.json
EXAMPLE_CONFIG ?= config.example.json
GOFLAGS        ?=
LDFLAGS        ?= -s -w
VERSION        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
WEB_DIR        ?= web/app
WEB_DIST       ?= web/dist
NPM            ?= npm

.DEFAULT_GOAL := help
.PHONY: help init config firmware build build-arm run test test-race vet fmt fmt-check tidy check web web-assets web-deps dev-web test-web clean deploy seed-db install-pi

help: ## mostra os alvos disponiveis
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

init: config firmware ## prepara o ambiente local (config.json, firmware/ e dependencias)
	$(GO) mod download

web-deps: ## instala as dependencias do front (npm ci)
	cd "$(WEB_DIR)" && $(NPM) ci

web: ## compila o front (Svelte/Vite) para web/dist (embutido no binario Go)
	cd "$(WEB_DIR)" && $(NPM) run build

# O dist do front e gerado (nao versionado) e embutido no binario: os alvos Go
# dependem deste guard, que compila o front so quando o dist esta ausente.
web-assets: ## garante web/dist (compila o front se faltar)
	@if [ ! -f "$(WEB_DIST)/index.html" ]; then \
		echo "== $(WEB_DIST) ausente; compilando o front"; \
		cd "$(WEB_DIR)" && { [ -d node_modules ] || $(NPM) ci --no-fund --no-audit; } && $(NPM) run build; \
	fi

dev-web: ## sobe o vite dev com proxy para o hub em :8080 (HMR)
	cd "$(WEB_DIR)" && $(NPM) run dev

test-web: ## roda os testes do front (vitest)
	cd "$(WEB_DIR)" && $(NPM) test

config: ## cria config.json a partir do exemplo, sem sobrescrever
	@if [ -f "$(CONFIG)" ]; then \
		echo "$(CONFIG) ja existe; mantido"; \
	else \
		cp "$(EXAMPLE_CONFIG)" "$(CONFIG)"; \
		echo "criado $(CONFIG); ajuste tokens e URLs dos gateways"; \
	fi

firmware: ## cria o diretorio de binarios OTA
	@mkdir -p firmware

build: web-assets ## compila o binario local
	$(GO) build $(GOFLAGS) -ldflags="-X smart_home/hub/internal/hub.Version=$(VERSION)" -o "$(BINARY)" ./cmd/smart-home-hub

build-arm: web-assets ## compila o binario armv7 para a Raspberry Pi
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 $(GO) build $(GOFLAGS) -trimpath -ldflags="$(LDFLAGS) -X smart_home/hub/internal/hub.Version=$(VERSION)" -o "$(BINARY)-linux-armv7" ./cmd/smart-home-hub

run: web-assets ## inicia o hub em modo dev (go run; recompila em cache, sem binario na raiz)
	@if [ ! -f "$(CONFIG)" ]; then \
		echo "erro: $(CONFIG) nao existe; rode 'make init' primeiro"; \
		exit 1; \
	fi
	$(GO) run $(GOFLAGS) -ldflags="-X smart_home/hub/internal/hub.Version=$(VERSION)" ./cmd/smart-home-hub -config "$(CONFIG)"

test: web-assets ## roda os testes
	$(GO) test ./...

test-race: web-assets ## roda os testes com o detector de corrida
	$(GO) test -race ./...

vet: ## roda go vet
	$(GO) vet ./...

fmt: ## formata o codigo com gofmt
	$(GO) fmt ./...

fmt-check: ## falha se algum arquivo nao estiver formatado
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "arquivos sem gofmt:"; \
		echo "$$out"; \
		exit 1; \
	fi

tidy: ## ajusta go.mod e go.sum
	$(GO) mod tidy

check: fmt-check vet test test-web ## validacao completa (formatacao, vet, testes e UI)
	@echo "check ok"

install-pi: ## bootstrap do servico na Pi (deploy/install-pi.sh)
	./deploy/install-pi.sh

deploy: ## compila o front + armv7 e envia o binario para a Pi (deploy/deploy.sh)
	./deploy/deploy.sh

seed-db: ## envia o hub.db local para a Pi (deploy/seed-db.sh; --force para sobrescrever)
	./deploy/seed-db.sh


clean: ## remove os binarios gerados
	rm -f "$(BINARY)" "$(BINARY)-linux-armv7"
	rm -rf dist
