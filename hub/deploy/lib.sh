#!/usr/bin/env bash
# Helpers comuns aos scripts de deploy (deploy.sh, seed-db.sh, install-pi.sh).

# deploy_sudo_hint <remote>
#
# Imprime o passo a passo para configurar sudo sem senha na Pi.
deploy_sudo_hint() {
  local remote="$1"
  local user="${remote%@*}"
  cat >&2 <<MSG
   ssh -t $remote 'echo "$user ALL=(ALL) NOPASSWD:ALL" | sudo tee /etc/sudoers.d/010-$user-nopasswd'
   ssh -t $remote 'sudo chmod 440 /etc/sudoers.d/010-$user-nopasswd'
   ssh $remote 'sudo visudo -c'      # valida a sintaxe

   (o '-t' e necessario nesta primeira vez porque o sudo vai pedir a senha)
MSG
}

# deploy_require_ssh <remote>
#
# Verifica, antes de enviar qualquer coisa:
#   1) acesso SSH NAO interativo (chave, sem senha);
#   2) 'sudo -n' (sudo sem senha) no destino.
# Se algo falhar, explica como configurar e retorna 1.
deploy_require_ssh() {
  local remote="$1"
  local user="${remote%@*}"

  if ! ssh -o BatchMode=yes -o ConnectTimeout=10 "$remote" true 2>/dev/null; then
    cat >&2 <<MSG
ERRO: sem acesso SSH nao interativo a $remote.

Configure uma chave SSH, uma unica vez:
   ssh-keygen -t ed25519 -N ''            # se ainda nao tiver uma chave
   ssh-copy-id $remote
   ssh -o BatchMode=yes $remote true      # teste: deve sair com 0
MSG
    return 1
  fi

  if ! ssh -o BatchMode=yes -o ConnectTimeout=10 "$remote" sudo -n true 2>/dev/null; then
    cat >&2 <<MSG
ERRO: 'sudo' na Pi ($remote) pede senha; o deploy roda comandos remotos com
'sudo' de forma nao interativa.

Habilite sudo sem senha para '$user' (uma vez):
MSG
    deploy_sudo_hint "$remote"
    cat >&2 <<MSG

Teste: ssh -o BatchMode=yes $remote sudo -n true
MSG
    return 1
  fi

  return 0
}
