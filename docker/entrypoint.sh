#!/bin/sh
# What a person would do, in order, on every container start.
#
# It is deliberately as dumb as it can be. Every decision that can be wrong
# already lives in Go, where it is tested; this only calls the CLI in the right
# order and prints the invite where somebody can read it.
#
# The one rule it exists to keep: THE ROOM IS NEVER CLOSED. It always tries the
# same one, and only renews the code when the code is provably gone.
#
# POSIX sh, no bashisms.
set -eu

DATA_DIR=/var/lib/kanpachi
SOCKET=/run/kanpachi/api.sock
DAEMON=/usr/libexec/kanpachi/kanpachid

# Long enough to wait behind the reopen the daemon starts on its own, which
# takes close to a minute. The CLI's own default is 90s and would be tight.
SLOW=300s

say()  { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die()  { printf '\n  %s\n\n' "$*" >&2; exit 1; }

# ----------------------------------------------------------------- the settings
#
# Read and checked BEFORE anything is started. A typo in a variable should cost
# a second, not a whole daemon boot with the complaint buried under its log.

: "${KANPACHI_SEED:?is required. It is the registry this machine opens its room on: a host name such as kanpachi.accentio.dev, with no scheme and no trailing slash}"

KANPACHI_ROOM_NAME="${KANPACHI_ROOM_NAME:-}"
KANPACHI_GAME="${KANPACHI_GAME:-}"
KANPACHI_GAME_NAME="${KANPACHI_GAME_NAME:-}"
KANPACHI_PORTS_TCP="${KANPACHI_PORTS_TCP:-}"
KANPACHI_PORTS_UDP="${KANPACHI_PORTS_UDP:-}"
KANPACHI_QUARANTINE="${KANPACHI_QUARANTINE:-false}"
KANPACHI_SEED_PASSWORD="${KANPACHI_SEED_PASSWORD:-}"
KANPACHI_SEED_PASSWORD_FILE="${KANPACHI_SEED_PASSWORD_FILE:-}"

room_name="${KANPACHI_ROOM_NAME:-$(hostname)}"

has_ports=false
if [ -n "$KANPACHI_PORTS_TCP" ] || [ -n "$KANPACHI_PORTS_UDP" ]; then
	has_ports=true
fi

if [ -n "$KANPACHI_GAME" ] && [ "$has_ports" = true ]; then
	die "KANPACHI_GAME and KANPACHI_PORTS_* cannot both be set: either the game
  comes from the catalogue with its ports, or you describe the ports yourself."
fi
if [ -n "$KANPACHI_GAME_NAME" ] && [ "$has_ports" != true ]; then
	die "KANPACHI_GAME_NAME needs KANPACHI_PORTS_TCP or KANPACHI_PORTS_UDP:
  naming a game without describing it means nothing."
fi

case "$KANPACHI_QUARANTINE" in
true | 1 | yes | on)  quarantine=on ;;
false | 0 | no | off) quarantine=off ;;
*) die "KANPACHI_QUARANTINE takes true or false, and it got \"$KANPACHI_QUARANTINE\"" ;;
esac

if [ -n "$KANPACHI_SEED_PASSWORD_FILE" ] && [ ! -r "$KANPACHI_SEED_PASSWORD_FILE" ]; then
	die "KANPACHI_SEED_PASSWORD_FILE points at $KANPACHI_SEED_PASSWORD_FILE, which cannot be read"
fi

# --------------------------------------------------- what the compose must grant
#
# A container cannot grant itself either of these, so the compose file has to.
# Checked here rather than left to the daemon, which aborts with a message about
# `sudo` that reads as nonsense inside a container.

capabilities_ok() {
	eff=$(awk '/^CapEff:/ { print $2 }' /proc/self/status 2>/dev/null || true)
	[ -n "$eff" ] || return 0   # cannot tell: do not block on it
	# CAP_NET_ADMIN is bit 12.
	[ $(( 0x$eff & 0x1000 )) -ne 0 ]
}

capabilities_ok || die "this container has no CAP_NET_ADMIN, and Kanpachi cannot
  build a network without it. Add to the service:

      cap_add: [NET_ADMIN]"

[ -c /dev/net/tun ] || die "/dev/net/tun is not here, and the virtual adapter is
  built on it. Add to the service:

      devices: [\"/dev/net/tun:/dev/net/tun\"]

  If it is already there and this still fails, the host kernel has no tun
  module: run modprobe tun on the machine, not in here."

# ------------------------------------------------------------------- the state
#
# The systemd unit creates this with StateDirectory=, the package does not, and
# the daemon refuses to start without it.
install -d -m 0700 "$DATA_DIR"

if [ ! -e "$DATA_DIR/identity.key" ]; then
	say ""
	say "  First start on this volume. The room's identity is about to be created"
	say "  in $DATA_DIR, and losing that directory loses the invite code for good."
fi

# ------------------------------------------------------------------ the daemon
#
# NOTIFY_SOCKET is what selects the production branch: real socket, real log,
# every guard armed, and blocking in the foreground. The readiness datagram
# reaches nobody and that is not fatal, on purpose.
#
# --console would be the wrong tool: it is development mode, it listens on a
# different socket, it prints the API token on stdout, and it skips the guard
# that refuses to run a binary carrying provisional adapters.
daemon_pid=""

forward_term() {
	[ -n "$daemon_pid" ] || exit 0
	kill -TERM "$daemon_pid" 2>/dev/null || true
}
trap forward_term TERM INT

# KANPACHI_CONTAINER is what lets the daemon send the room's traffic to wherever
# the game actually listens. A server bound to the container's own address never
# sees what arrives on Kanpachi's, and the room dies with everything looking
# perfect; here the intent is unambiguous, because this container declared a game
# and shares Kanpachi's network. On a normal machine that same rewrite would undo
# somebody's decision to bind narrowly, so it is off everywhere else.
NOTIFY_SOCKET=/nonexistent KANPACHI_CONTAINER=1 "$DAEMON" &
daemon_pid=$!

# The wait asks the daemon a QUESTION, and does not look for its socket file.
#
# $SOCKET lives on a volume that outlives the container, so a restart finds
# yesterday's socket file sitting there with nobody behind it. Testing for the
# file passed on the first pass, the CLI two lines below got ECONNREFUSED,
# `set -e` ended the script, the container died, and the dead file stayed for
# the next start to trip over. Measured on 2026-08-26 in Kubernetes: 27
# restarts in two hours, each one a boot that got 40 ms before the daemon had
# bound anything.
#
# `status` is the cheapest question the daemon answers, and answering it is
# proof of the only thing this loop cares about.
waited=0
until kanpachi --json status >/dev/null 2>&1; do
	kill -0 "$daemon_pid" 2>/dev/null \
		|| die "the daemon died before opening its control channel. What it said is above."
	waited=$(( waited + 1 ))
	[ "$waited" -lt 60 ] || die "the daemon never answered on $SOCKET"
	sleep 1
done

# ---------------------------------------------------------------- small helpers

# ask runs the CLI in JSON mode. The daemon answers a single JSON document
# either way: the result on success, and {"error":{"code":"..."}} on failure,
# both on stdout.
#
# It swallows the exit status on purpose, and the caller reads the document
# instead. Under `set -e` a failing `x=$(ask ...)` would end the script, and the
# place that hurts is the last one: the invite would stop being printed exactly
# when something is wrong and somebody needs to read it.
ask() { kanpachi --json "$@" 2>/dev/null || true; }

error_code() { printf '%s' "$1" | jq -r '.error.code // ""' 2>/dev/null || true; }

# explicar traduce el código del daemon a lo que hay que tocar en el compose.
#
# It exists because the first version of this script threw the document away and
# said "ask doctor", and doctor answers "nothing stops a room from opening" when
# what is missing is a password. Sending somebody to a healthy report is worse
# than saying nothing.
explicar() {
	documento="$1"
	qué="$2"
	case "$(error_code "$documento")" in
	seed_password)
		die "$KANPACHI_SEED asks for a password to host on it, and none was given.
  Put one in the compose file, either of these:

      KANPACHI_SEED_PASSWORD_FILE: /run/secrets/seed_password
      KANPACHI_SEED_PASSWORD: \"...\""
		;;
	no_registry | seed_missing | no_own_seed)
		die "the registry $KANPACHI_SEED did not answer, so there is nowhere to open a
  room. Check the name in KANPACHI_SEED, and that this machine can reach it."
		;;
	firewall_blocks)
		die "a firewall on this machine is blocking the room, and the consent to open
  it did not get through. Look at it with:
      docker compose exec kanpachi kanpachi doctor"
		;;
	quarantine_undecided)
		die "the daemon is still waiting for an answer about the base quarantine.
  Set KANPACHI_QUARANTINE to true or false in the compose file."
		;;
	unknown_game)
		die "there is no game called \"$game_id\" in this machine's catalogue. The ones
  there are:
      docker compose exec kanpachi kanpachi games"
		;;
	*)
		die "$qué: $documento"
		;;
	esac
}

field() { printf '%s' "$1" | jq -r "$2 // \"\"" 2>/dev/null || true; }

# --------------------------------------------------------- applying the settings
#
# Everything here is idempotent: the container runs it again on every start.
# The values were already read and checked above; what happens here is the part
# that needs a daemon to talk to.

# The registry. Only written when it differs, so a start with the seed already
# set does not spend a network round trip verifying it again.
if [ "$(field "$(ask seed)" .seed)" != "$KANPACHI_SEED" ]; then
	say "  registry: $KANPACHI_SEED"
	kanpachi seed "$KANPACHI_SEED"
fi

# The hosting password, if that registry asks for one. It goes in on stdin and
# nowhere else: an argument would be readable in /proc/<pid>/cmdline by anybody
# on the machine, which is why the command has no flag for it.
if [ -n "$KANPACHI_SEED_PASSWORD_FILE" ]; then
	kanpachi password <"$KANPACHI_SEED_PASSWORD_FILE"
elif [ -n "$KANPACHI_SEED_PASSWORD" ]; then
	printf '%s' "$KANPACHI_SEED_PASSWORD" | kanpachi password
fi

# The quarantine. An absent decision is UNDECIDED, not "no", and the CLI refuses
# to host while it is undecided, so this is answered on every start.
kanpachi quarantine "$quarantine" >/dev/null

# A profile of your own. Saving it overwrites the one already under this id
# instead of adding a second, which is what makes re-running this safe.
game_id="$KANPACHI_GAME"
if [ "$has_ports" = true ]; then
	game_id=custom
	set -- profile "$game_id" --name "${KANPACHI_GAME_NAME:-Custom game}"
	[ -z "$KANPACHI_PORTS_TCP" ] || set -- "$@" --tcp "$KANPACHI_PORTS_TCP"
	[ -z "$KANPACHI_PORTS_UDP" ] || set -- "$@" --udp "$KANPACHI_PORTS_UDP"
	kanpachi "$@" >/dev/null
	say "  profile: $game_id (tcp ${KANPACHI_PORTS_TCP:-none}, udp ${KANPACHI_PORTS_UDP:-none})"
fi

# ---------------------------------------------------------------------- the room
#
# resume, and never a look at status first, and the reason is not style.
#
# The daemon reopens the saved room on its own at every start, in the
# background, and while it does, status still answers with the snapshot from
# before it began: idle, with no code. So status cannot tell "reopening right
# now" from "nothing was ever saved", and anybody reading it would open a SECOND
# room on top of the one that was coming back.
#
# Asking for resume sidesteps the question entirely. The session lock serialises
# it behind the reopen already in flight, so there is nothing to poll and no
# clock to get wrong. busy means that reopen won the race, which is success.
if out=$(kanpachi --timeout "$SLOW" --json resume 2>/dev/null); then
	say "  room: reopened with the code it already had"
else
	case "$(error_code "$out")" in
	busy)
		say "  room: already open"
		;;
	no_pending)
		say "  room: none was saved, opening one"
		abierta=$(kanpachi --timeout "$SLOW" --json host "$room_name" \
			--yes --quarantine "$quarantine" 2>/dev/null) \
			|| explicar "$abierta" "the room could not be opened"
		;;
	*)
		explicar "$out" "the saved room could not be reopened"
		;;
	esac
fi

# The one case where the code has to change, and it never heals on its own: the
# registry has affirmed it no longer knows this invite ID, and publishing does
# not create. Everything handed out stops working, so it is said loudly.
if [ "$(field "$(ask status)" .code_lost)" = "true" ]; then
	warn ""
	warn "  ####  THE INVITE CODE EXPIRED AND HAD TO BE RENEWED  ####"
	warn "  Every link handed out before now is dead. The new one is below."
	warn ""
	kanpachi --timeout "$SLOW" --json rotate >/dev/null \
		|| die "the code is gone and could not be renewed"
fi

# The game. Nothing opens until somebody is in the room: the rules are computed
# from the members present, and with nobody there the desired set is empty.
if [ -n "$game_id" ]; then
	activada=$(kanpachi --json game "$game_id" 2>/dev/null) \
		|| explicar "$activada" "the game could not be activated"
	say "  game: $game_id"
fi

# ------------------------------------------------------------------ the invite
#
# Printed on EVERY start, including the ones where nothing was done. On an
# unattended server this output is the only place anybody can go looking for the
# code, so a start that stays quiet is a server nobody can reach.
#
# Both values come out of one status, and the link is NEVER rebuilt from the
# code and the seed: the card key that closes it appears in no other field.
state=$(ask status)
code=$(field "$state" .code)
seed=$(field "$state" .seed)
link=$(field "$state" .link)

say ""
say "  ------------------------------------------------------------"
say "  Room   $room_name"
say "  Code   $code@$seed"
say "  Link   $link"
say "  ------------------------------------------------------------"
say ""
say "  Hand out either one. The same code comes back on every restart, as long"
say "  as $DATA_DIR keeps its volume."
say ""

# ------------------------------------------------------------------------ wait
#
# This is what keeps the container alive. The daemon's own log is the detailed
# one and lives in the volume:
#
#   docker compose exec kanpachi tail -f /var/lib/kanpachi/logs/kanpachi.log
#
# The daemon's own exit status is what this container exits with. A SIGTERM
# interrupts the wait before the process is actually gone, so it is waited for
# again rather than reporting the interruption as the daemon's answer.
salida=0
wait "$daemon_pid" || salida=$?
while kill -0 "$daemon_pid" 2>/dev/null; do
	sleep 1
	salida=0
done
exit "$salida"
