// Importa todos os layouts para que se registrem (efeito colateral). Ordem
// importa apenas para a inferencia: o generic e o ultimo recurso.

import "./fan/index";
// O modulo tuya e compartilhado (nao registra layout): e importado por
// socket/switch/lock/generic.
import "./socket/index";
// A fechadura vem antes do interruptor: sem isso o switch (que casa qualquer
// Tuya com specs) venceria a inferencia.
import "./lock/index";
// O multi-tecla vem antes do interruptor simples: sem isso o switch (que casa
// qualquer Tuya com specs) venceria a inferencia.
import "./gang/index";
import "./switch/index";
import "./midea/index";
import "./generic/index";

export * from "./registry";
