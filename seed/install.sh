#!/bin/sh
# Instalador de kanpachi-seed.
#
#   curl -fsSL https://raw.githubusercontent.com/accentiostudios/kanpachi/main/seed/install.sh | sudo sh
#
# Baja el binario, comprueba su firma de contenido, y le cede el trabajo a
# `kanpseed init`, que es quien elige puertos, coloca EasyTier, escribe los
# servicios de systemd y dice qué hay que poner en el proxy inverso.
#
# Este script es a propósito lo más tonto posible. Toda la lógica que puede
# equivocarse vive en Go, donde se puede probar; aquí solo hay lo que
# forzosamente ocurre antes de tener un binario.
#
# POSIX sh, sin bashismos: el droplet trae dash como /bin/sh.
set -eu

REPO="accentiostudios/kanpachi"
DESTINO="/usr/local/bin/kanpseed"

decir()  { printf '%s\n' "$*"; }
morir()  { printf '\n  %s\n\n' "$*" >&2; exit 1; }

[ "$(id -u)" = "0" ] || morir "esto necesita root: vuelve a ejecutarlo con sudo"

[ "$(uname -s)" = "Linux" ] || morir "el seed es Linux, y esta máquina es $(uname -s)"

case "$(uname -m)" in
  x86_64|amd64)  ARCO="amd64" ;;
  aarch64|arm64) ARCO="arm64" ;;
  *) morir "no hay build para $(uname -m): las arquitecturas soportadas son x86_64 y aarch64" ;;
esac

command -v systemctl >/dev/null 2>&1 || morir "esta máquina no usa systemd, y el seed se instala como servicio de systemd"

if command -v curl >/dev/null 2>&1; then
  BAJAR="curl -fsSL -o"
elif command -v wget >/dev/null 2>&1; then
  BAJAR="wget -qO"
else
  morir "hace falta curl o wget"
fi

VERSION="${KANPSEED_VERSION:-latest}"
if [ "$VERSION" = "latest" ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi

TMP="$(mktemp -d)"
# shellcheck disable=SC2064
trap "rm -rf '$TMP'" EXIT INT TERM

decir ""
decir "  bajando de $BASE"
$BAJAR "$TMP/kanpseed" "$BASE/kanpseed-linux-$ARCO" \
  || morir "no se pudo bajar el binario. ¿Existe ya un release publicado?"
# La página va aparte del binario a propósito: así se puede corregir un texto
# sin recompilar, y el binario no crece con un HTML incrustado.
$BAJAR "$TMP/index.html" "$BASE/index.html" \
  || morir "no se pudo bajar la página de invitación"
$BAJAR "$TMP/SHA256SUMS" "$BASE/SHA256SUMS" \
  || morir "no se pudo bajar SHA256SUMS: sin él no se verifica nada y no se instala nada"

# Se verifica ANTES de darle permiso de ejecución. Esto termina corriendo como
# servicio en un servidor con IP pública, así que la comprobación no es un
# trámite. Se comprueban los dos archivos, no solo el binario: la página se
# sirve a desconocidos y es igual de sustituible en tránsito.
verificar() {
  archivo="$1"
  quiero="$(grep " $archivo\$" "$TMP/SHA256SUMS" | cut -d' ' -f1)"
  [ -n "$quiero" ] || morir "SHA256SUMS no menciona $archivo"
  tengo="$(cd "$TMP" && sha256sum "$2" | cut -d' ' -f1)"
  [ "$tengo" = "$quiero" ] || morir "el SHA256 de $archivo no coincide
  esperado $quiero
  obtenido $tengo
  no se instala nada"
}
verificar "kanpseed-linux-$ARCO" "kanpseed"
verificar "index.html" "index.html"
decir "  SHA256 verificado"

install -m 0755 "$TMP/kanpseed" "$DESTINO"

# A partir de aquí manda el binario: elegir puertos, colocar EasyTier, escribir
# las units y decir qué falta en el proxy. Los argumentos se pasan tal cual, así
# que `... | sudo sh -s -- --domain kanpachi.midominio.com` funciona.
exec "$DESTINO" init --page "$TMP/index.html" "$@"
