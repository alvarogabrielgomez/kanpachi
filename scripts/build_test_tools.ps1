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
    Recompila el motor aunque ya haya uno en testTools.

.PARAMETER SinPreguntar
    No pregunta nada. Sirve para correrlo desatendido.
#>
[CmdletBinding()]
param(
    [switch]$RecompilarMotor,
    [switch]$SinPreguntar
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

# ── 0. Basura de corridas anteriores ─────────────────────────────────────────
# El `.exe~` lo deja Go al reemplazar un binario en uso, y el de la raiz sale de
# un `go build` sin -o.
Get-ChildItem $salida -Filter "*.exe~" -ErrorAction SilentlyContinue | Remove-Item -Force
$suelto = Join-Path $raizRepo "roomprobe.exe"
if (Test-Path $suelto) { Remove-Item $suelto -Force; Nota "borrado el roomprobe.exe suelto de la raiz" }

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

# ── 2. El motor ───────────────────────────────────────────────────────────────
$destinoMotor = Join-Path $salida "kanpachi-engine.exe"
$saltarMotor  = $false

if ((Test-Path $destinoMotor) -and (-not $RecompilarMotor)) {
    $mb = [math]::Round((Get-Item $destinoMotor).Length / 1MB, 1)
    if ($SinPreguntar) {
        $saltarMotor = $true
        Nota "kanpachi-engine.exe ya esta ($mb MB), no se recompila"
    } else {
        Aviso "kanpachi-engine.exe ya existe en testTools ($mb MB)"
        $r = Read-Host "  Recompilarlo? (s/N)"
        if ($r -ne "s" -and $r -ne "S") { $saltarMotor = $true }
    }
}

if (-not $saltarMotor) {
    if ($RecompilarMotor) {
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
            throw "El motor se compiló pero no apareció en $motorExe."
        }
    } else {
        Paso "Buscando un motor ya compilado..."
        $motorExe = $null
        foreach ($p in @(
            "C:\kt\release\kanpachi-engine.exe",
            "C:\kt\stage\kanpachi-engine.exe",
            (Join-Path $raizMotor "target\release\kanpachi-engine.exe"))) {
            if (Test-Path $p) { $motorExe = $p; break }
        }

        if (-not $motorExe) {
            Aviso "No hay ninguno compilado. Compilando desde $raizMotor"
            if (-not (Test-Path (Join-Path $raizMotor "Cargo.toml"))) {
                throw "No esta el repo kanpachi-engine en $raizMotor. Clonalo al lado de kanpachi."
            }
            $buildMotor = Join-Path $raizMotor "scripts\build.ps1"
            if (Test-Path $buildMotor) {
                & $buildMotor
            } else {
                Push-Location $raizMotor
                try { cargo build --release } finally { Pop-Location }
            }
            $motorExe = Join-Path $raizMotor "target\release\kanpachi-engine.exe"
            if (-not (Test-Path $motorExe)) {
                $motorExe = "C:\kt\release\kanpachi-engine.exe"
                if (-not (Test-Path $motorExe)) { throw "El motor no se compilo. Mira la salida de cargo build." }
            }
        }
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
