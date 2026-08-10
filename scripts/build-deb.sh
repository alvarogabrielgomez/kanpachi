#!/usr/bin/env bash
#
# Arma `kanpachi-amd64.deb` con lo que ya está compilado.
#
# # Qué NO hace, y por qué
#
# No compila el motor. Ese binario sale de `scripts/build-linux.sh` del
# repositorio del motor, corriendo EN Linux, y este script lo recibe por
# `--engine`. Meter las dos cosas en un script haría que armar el paquete
# dependiera de tener el toolchain de Rust, que en el runner que publica son dos
# pasos distintos y en una máquina de pruebas es media hora de diferencia.
#
# # La versión va DENTRO del paquete y no en su nombre
#
# El fichero se llama `kanpachi-amd64.deb`, sin versión, porque la página apunta
# a `releases/latest/download/<fichero>`, que es una URL permanente. La versión
# viaja en el campo `Version` del control, que es de donde la lee `dpkg`. La
# razón está escrita en `release.yml`: una constante de versión a mano quedó
# vacía para siempre y la pantalla dijo "todavía no hay instalador" durante toda
# la vida del producto.
#
# Uso:
#   scripts/build-deb.sh --version 0.2.0 --engine /ruta/kanpachi-engine [--out dist]

set -euo pipefail

raiz="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version=""
engine=""
out="$raiz/dist"

while [ $# -gt 0 ]; do
	case "$1" in
	--version) shift; version="${1:-}" ;;
	--engine) shift; engine="${1:-}" ;;
	--out) shift; out="${1:-}" ;;
	*) echo "opción desconocida: $1" >&2; exit 2 ;;
	esac
	shift
done

paso() { printf '\n=== %s ===\n' "$1"; }
bien() { printf '  OK  %s\n' "$1"; }

[ -n "$version" ] || { echo "hace falta --version" >&2; exit 2; }
[ -n "$engine" ] || { echo "hace falta --engine, con la ruta del motor ya compilado" >&2; exit 2; }
[ -x "$engine" ] || { echo "no está o no es ejecutable: $engine" >&2; exit 1; }
command -v dpkg-deb >/dev/null || { echo "falta dpkg-deb. Instalalo con: sudo apt install dpkg-dev" >&2; exit 1; }

# Sin la `v` inicial: dpkg ordena versiones y `v0.2.0` no es un número para él.
version="${version#v}"

paso "compilando el daemon y el CLI"
# CGO apagado: sin él el binario no arrastra el enlazador dinámico de glibc para
# NSS, que es lo que hace que un binario compilado en una distribución no arranque
# en otra. El daemon no usa nada que lo necesite.
export CGO_ENABLED=0
mkdir -p "$out"
# compilar, con el respaldo para el caso del checkout ajeno.
#
# `go build` sella el binario con el estado del repositorio, y para eso le
# pregunta a git. Corriendo desde WSL sobre un checkout de Windows, git se niega
# con `detected dubious ownership` porque los ficheros son de otro usuario, y lo
# que llega es `error obtaining VCS status: exit status 128`, que no nombra ni a
# git ni al dueño.
#
# Se reintenta sin el sellado y se DICE. Perderlo en el artefacto que se publica
# sería una pérdida de verdad, y ahí no pasa: el runner compila su propio
# checkout. Acá pasa siempre, y no es motivo para no poder armar un paquete de
# prueba.
compilar() {
	local destino=$1 paquete=$2
	if go build -trimpath -o "$destino" "$paquete" 2>/dev/null; then
		return 0
	fi
	if go build -trimpath -buildvcs=false -o "$destino" "$paquete"; then
		echo "  --  sin sellado de git: este checkout es de otro usuario." >&2
		return 0
	fi
	return 1
}

compilar "$out/kanpachid" "$raiz/daemon/cmd/kanpachid"
bien "kanpachid"

# PROVISIONAL: `/usr/bin/kanpachi` es todavía `kanpctl`, que es la herramienta de
# diagnóstico, no el CLI. Habla el protocolo entero y pide el método por nombre
# con el JSON crudo en `-params`, o sea sirve para probar el paquete y no para
# dárselo a nadie. Lo reemplaza el CLI de verdad, con sus subcomandos y su
# asistente interactivo.
compilar "$out/kanpachi" "$raiz/internal/kanpctl"
bien "kanpachi (provisional: todavía es kanpctl)"

paso "armando el árbol del paquete"
arbol="$out/kanpachi-deb"
rm -rf "$arbol"
mkdir -p \
	"$arbol/DEBIAN" \
	"$arbol/usr/bin" \
	"$arbol/usr/libexec/kanpachi" \
	"$arbol/usr/share/kanpachi" \
	"$arbol/lib/systemd/system"

install -m 0755 "$out/kanpachi" "$arbol/usr/bin/kanpachi"
install -m 0755 "$out/kanpachid" "$arbol/usr/libexec/kanpachi/kanpachid"
install -m 0755 "$engine" "$arbol/usr/libexec/kanpachi/kanpachi-engine"

# El catálogo que viene con el paquete. Es de SOLO LECTURA para el daemon: lo
# actualiza el paquete, y lo propio del usuario vive aparte en /var/lib.
if [ -f "$raiz/catalog/builtin.json" ]; then
	install -m 0644 "$raiz/catalog/builtin.json" "$arbol/usr/share/kanpachi/builtin.json"
	bien "builtin.json"
else
	echo "  --  no hay catalog/builtin.json, el paquete va sin catálogo" >&2
fi

install -m 0644 "$raiz/packaging/systemd/kanpachid.service" "$arbol/lib/systemd/system/"
install -m 0644 "$raiz/packaging/systemd/kanpachi-quarantine.service" "$arbol/lib/systemd/system/"

sed "s/@VERSION@/$version/" "$raiz/packaging/debian/control" >"$arbol/DEBIAN/control"
for s in postinst prerm postrm; do
	install -m 0755 "$raiz/packaging/debian/$s" "$arbol/DEBIAN/$s"
done
bien "árbol en $arbol"

paso "comprobando antes de empaquetar"
# El piso de glibc del MOTOR decide en qué Ubuntu corre el paquete, y es el
# único de los tres binarios que lo tiene: los de Go van sin cgo. Compilado en
# 24.04 pide 2.39 y NO arranca en 22.04, que es la que más se ve en un VPS.
glibc="$(objdump -T "$arbol/usr/libexec/kanpachi/kanpachi-engine" 2>/dev/null |
	grep -o 'GLIBC_[0-9.]*' | sort -V | tail -1 || true)"
if [ -n "$glibc" ]; then
	printf '  el motor exige %s\n' "$glibc"
	case "$glibc" in
	GLIBC_2.3[6-9] | GLIBC_2.4*)
		echo "  --  por encima del 2.35 de Ubuntu 22.04: este paquete NO va a arrancar ahí." >&2
		echo "      El artefacto que se publica se compila en un runner 22.04." >&2
		;;
	esac
fi

paso "empaquetando"
deb="$out/kanpachi-amd64.deb"
# `--root-owner-group` para que todo salga de root:root sin necesitar fakeroot.
dpkg-deb --build --root-owner-group "$arbol" "$deb" >/dev/null
bien "$deb ($(( $(stat -c %s "$deb") / 1024 )) KiB)"

paso "lo que quedó dentro"
dpkg-deb --contents "$deb" | awk '{print "  " $1, $6}'

paso "listo"
