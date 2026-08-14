<#
.SYNOPSIS
    Arma testTools\: roomprobe.exe con el motor y sus DLL al lado.

.DESCRIPTION
    roomprobe ejercita el ciclo de vida de una sala con los adaptadores reales,
    sin daemon ni interfaz. Necesita a kanpachi-engine.exe JUNTO a el, porque no
    lo busca en el PATH a proposito: un PATH que alguien pueda escribir es una
    forma de que un proceso elevado ejecute otro binario con ese nombre.

    NO hace falta consola elevada para esto: aqui solo se compila y se copia.
    Elevada la necesita roomprobe al correr, y se eleva sola.

.PARAMETER RecompilarMotor
    Obsoleto: el motor se recompila siempre. Se acepta para no romper lo que
    alguien tenga escrito en el historial de su consola.

.PARAMETER SinPreguntar
    No pregunta nada. Sirve para correrlo desatendido.

.PARAMETER Limpio
    Borra ademas roomprobe-data, data y roomprobe.log. Eso se lleva por delante
    identity.key y la libreta de huellas de esta maquina, o sea que la sonda
    vuelve a presentarse con otra cara y sin recordar a nadie. Sin este switch
    se borra solo lo construido.
#>
[CmdletBinding()]
param(
    [switch]$RecompilarMotor,
    [switch]$SinPreguntar,
    [switch]$Limpio
)

$ErrorActionPreference = "Stop"

# El repo se deduce de la ruta del script y NUNCA del directorio actual: una
# consola elevada arranca en system32.
$raizRepo   = Split-Path -Parent $PSScriptRoot
$raizMotor  = Join-Path (Split-Path -Parent $raizRepo) "kanpachi-engine"
$salida     = Join-Path $raizRepo "testTools"

if (-not (Test-Path $salida)) { New-Item -ItemType Directory -Path $salida | Out-Null }

function Paso($t) { Write-Host "`n$t" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  $t" -ForegroundColor Green }
function Nota($t) { Write-Host "  $t" -ForegroundColor DarkGray }
function Aviso($t) { Write-Host "  $t" -ForegroundColor Yellow }

# ── 0. Se BORRA todo lo construido, siempre ──────────────────────────────────
#
# No se reutiliza nada de una corrida anterior. Un directorio donde conviven
# binarios de distintos dias no falla al construirse: falla al correr, con un
# error que habla de otra cosa —un campo JSON que el motor no conoce, un menu
# que no coincide con el codigo— y lo paga quien esta probando, no quien
# construyo.
#
# Se borran los productos: los .exe, las DLL, el driver y la carga que se empotra
# en el bundle. NO se borran `roomprobe-data` ni `roomprobe.log`: ahi viven
# `identity.key`, la libreta de huellas y la evidencia de la ultima corrida, que
# es justo lo que se manda por chat. Para eso esta -Limpio.
Paso "Borrando lo construido antes (no se reutiliza nada)"
$productos = @("roomprobe.exe", "roombundle.exe", "kanpachi-engine.exe",
               "wintun.dll", "Packet.dll", "WinDivert64.sys")
foreach ($f in $productos) {
    $ruta = Join-Path $salida $f
    if (Test-Path $ruta) { Remove-Item $ruta -Force; Nota "borrado $f" }
}
Get-ChildItem $salida -Filter "*.exe~" -ErrorAction SilentlyContinue | Remove-Item -Force
$carga = Join-Path $raizRepo "internal\roombundle\carga"
if (Test-Path $carga) {
    Remove-Item (Join-Path $carga "*") -Force -Recurse -ErrorAction SilentlyContinue
    Nota "vaciada internal\roombundle\carga"
}
$suelto = Join-Path $raizRepo "roomprobe.exe"
if (Test-Path $suelto) { Remove-Item $suelto -Force; Nota "borrado el roomprobe.exe suelto de la raiz" }

if ($Limpio) {
    foreach ($d in @("roomprobe-data", "data")) {
        $ruta = Join-Path $salida $d
        if (Test-Path $ruta) { Remove-Item $ruta -Recurse -Force; Aviso "borrado $d (identidad y libreta incluidas)" }
    }
    $log = Join-Path $salida "roomprobe.log"
    if (Test-Path $log) { Remove-Item $log -Force; Aviso "borrado roomprobe.log" }
}

# ── 1. La puerta del CI, corrida aqui ────────────────────────────────────────
# El job de Linux hace `go vet ./internal/...`, y roomprobe se lo rompio una vez
# importando x/sys/windows sin etiqueta de compilacion. Cuesta unos segundos y
# evita descubrirlo despues de un push.
Paso "Comprobando que no se rompe el job de Linux del CI..."
$env:GOOS = "linux"
try {
    Push-Location $raizRepo
    try {
        go vet ./internal/...
        if ($LASTEXITCODE -ne 0) { throw "roomprobe rompe el job de Linux del CI" }
    } finally { Pop-Location }
} finally { Remove-Item Env:GOOS }
Bien "Linux compila y vetea"

# ── 2. El motor, recompilado SIEMPRE ─────────────────────────────────────────
#
# Nunca se reutiliza uno que haya por ahi. De reutilizarlo salio el fallo del
# 2026-08-14: el daemon manda `log_dir` en cada orden, el .exe que estaba en
# testTools era anterior al campo, y abrir una sala moria en
#
#   unreadable command: unknown field `log_dir`, expected one of `dev_name`...
#
# Ese mensaje no se parece en nada a "ese binario es de hace seis dias", asi que
# lo paga en investigacion quien esta probando. Cuestan unos minutos de Rust y
# quitan una clase entera de fallo.
$destinoMotor = Join-Path $salida "kanpachi-engine.exe"

Paso "Recompilando el motor desde $raizMotor..."
if (-not (Test-Path (Join-Path $raizMotor "Cargo.toml"))) {
    throw "No esta el repo kanpachi-engine en $raizMotor. Clonalo al lado de kanpachi."
}
$buildMotor = Join-Path $raizMotor "scripts\build.ps1"
if (-not (Test-Path $buildMotor)) {
    throw "Falta $buildMotor, que prepara MSVC, protoc, libclang y 7-Zip antes de compilar."
}
& $buildMotor -Stage $salida
$motorExe = "C:\kt\release\kanpachi-engine.exe"
if (-not (Test-Path $motorExe)) {
    throw "El motor se compilo pero no aparecio en $motorExe."
}

Copy-Item $motorExe $destinoMotor -Force
$mb = [math]::Round((Get-Item $destinoMotor).Length / 1MB, 1)
Bien "Motor copiado desde $motorExe ($mb MB)"

# Packet.dll es importacion dura del motor y wintun.dll es lo que crea el
# adaptador virtual: sin ellas el motor arranca y no levanta ninguna red.
$dirMotor = Split-Path -Parent $motorExe
foreach ($dep in @('Packet.dll', 'wintun.dll', 'WinDivert64.sys')) {
    $src = Join-Path $dirMotor $dep
    if (Test-Path $src) { Copy-Item $src (Join-Path $salida $dep) -Force; Nota "+ $dep" }
}

# ── 3. roomprobe ──────────────────────────────────────────────────────────────
# Sin `-ldflags "-s -w"` a proposito: dispara falsos positivos de Defender sobre
# binarios de Go, y aqui no hay ningun tamano que ahorrar que valga eso.
Paso "Compilando roomprobe..."
$destinoProbe = Join-Path $salida "roomprobe.exe"
Push-Location $raizRepo
try {
    go build -o $destinoProbe ./internal/roomprobe
    if ($LASTEXITCODE -ne 0) { throw "no compilo roomprobe" }
} finally { Pop-Location }
Bien "roomprobe.exe listo"

# ── 4. Comprobar lo armado ────────────────────────────────────────────────────
# Un `go build` verde con el motor ausente sigue siendo una herramienta que no
# levanta ninguna sala.
$cinco = @('roomprobe.exe', 'kanpachi-engine.exe', 'wintun.dll', 'Packet.dll', 'WinDivert64.sys')
$faltan = $cinco | Where-Object { -not (Test-Path (Join-Path $salida $_)) }
if ($faltan) { throw "Faltan en testTools: $($faltan -join ', ')" }

# ── 5. roombundle: los cinco dentro de UN ejecutable ─────────────────────────
# `go:embed` solo empotra ficheros que esten DENTRO del directorio del paquete
# (no acepta `../`), asi que hay que copiarlos ahi justo antes de compilar.
#
# La etiqueta `-tags bundle` es lo que hace que la carga entre. Sin ella el
# paquete compila igual y el binario se niega a correr diciendo por que: es a
# proposito, para que un `go build ./...` normal no produzca un bundle vacio que
# falla recien en la maquina de otra persona.
Paso "Armando roombundle (los cinco en un solo ejecutable)..."
$dirCarga = Join-Path $raizRepo "internal\roombundle\carga"
if (-not (Test-Path $dirCarga)) { New-Item -ItemType Directory -Path $dirCarga | Out-Null }
Get-ChildItem $dirCarga -Exclude ".gitignore" -ErrorAction SilentlyContinue | Remove-Item -Force
foreach ($f in $cinco) { Copy-Item (Join-Path $salida $f) (Join-Path $dirCarga $f) -Force }

$destinoBundle = Join-Path $salida "roombundle.exe"
Push-Location $raizRepo
try {
    go build -tags bundle -o $destinoBundle ./internal/roombundle
    if ($LASTEXITCODE -ne 0) { throw "no compilo roombundle" }
} finally {
    Pop-Location
    # La carga se borra siempre: son 48 MB dentro del arbol de codigo, y
    # dejarlos ahi hace que el proximo `go build ./...` sea mas lento y que un
    # `git status` distraido asuste.
    Get-ChildItem $dirCarga -Exclude ".gitignore" -ErrorAction SilentlyContinue | Remove-Item -Force
}
$mb = [math]::Round((Get-Item $destinoBundle).Length / 1MB, 1)
Bien "roombundle.exe listo ($mb MB) - esto es lo que se manda por chat"

# ── 5.5 Los cinco tienen que estar, y recien construidos ─────────────────────
#
# El paso 0 los borra todos, asi que lo que quede aqui salio de esta corrida. Se
# comprueba igualmente: el script del motor copia sus DLL desde el stage, y si
# ese stage esta vacio se queda avisando en gris y sigue. Un testTools sin
# Packet.dll construye bien y muere al levantar la red, en la maquina de otro.
Paso "Comprobando que estan los cinco"
# Dos se CONSTRUYEN aqui y tienen que ser de esta corrida. Los otros tres son de
# terceros: se descargan una vez, su fecha es la del dia que se bajaron y estan
# fijados por hash en internal/arch/suministro_test.go, asi que de ellos solo se
# exige que esten.
$arranque = (Get-Date).AddMinutes(-90)
foreach ($f in @("roomprobe.exe", "kanpachi-engine.exe")) {
    $ruta = Join-Path $salida $f
    if (-not (Test-Path $ruta)) { throw "Falta $f en testTools: el bundle quedaria incompleto." }
    if ((Get-Item $ruta).LastWriteTime -lt $arranque) {
        throw "$f tiene fecha de $((Get-Item $ruta).LastWriteTime): no salio de esta corrida."
    }
}
foreach ($f in @("wintun.dll", "Packet.dll", "WinDivert64.sys")) {
    if (-not (Test-Path (Join-Path $salida $f))) {
        throw "Falta $f en testTools. Es de terceros y lo deja el build del motor; sin el, el motor no levanta ninguna red."
    }
}
Bien "los cinco estan, y los dos que se construyen son de esta corrida"

# ── 6. El servicio, que se van a pelear ───────────────────────────────────────
$svc = Get-Service kanpachi-daemon -ErrorAction SilentlyContinue
if ($svc -and $svc.Status -eq 'Running') {
    Aviso "kanpachi-daemon esta CORRIENDO."
    Aviso "roomprobe se niega a arrancar salvo con -force, y con razon: al"
    Aviso "construir la sesion purga las reglas de firewall de ese daemon."
    Aviso "  Paralo con:  Stop-Service kanpachi-daemon"
}

Paso "Listo. Contenido de testTools:"
Get-ChildItem $salida | ForEach-Object {
    $t = if ($_.PSIsContainer) { "dir" } else { "$([math]::Round($_.Length / 1MB, 1)) MB" }
    Write-Host "  $($_.Name)  ($t)"
}
Write-Host "`n  Corre:  $destinoProbe" -ForegroundColor Cyan
Write-Host "  El log queda en: $(Join-Path $salida 'roomprobe.log')" -ForegroundColor Cyan
