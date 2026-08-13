# Arma la CARGA que el instalador empaqueta.
#
# Es lo que va dentro de Program Files\Kanpachi\, y nada mas. A diferencia de
# preparar-stage.ps1, que arma un banco de pruebas con sondas incluidas, esto
# produce exactamente lo que se le entrega a una persona.
#
# Las diferencias con el stage, y las tres importan:
#
#   - Sin sondas. dirprobe, fwprobe y compania son herramientas de medicion y no
#     tienen nada que hacer en la maquina de nadie.
#   - Flutter en RELEASE, no en debug. Un build de debug lleva el runtime de
#     depuracion, va lento y muestra el banner.
#   - Con -trimpath, para que las rutas de compilacion de esta maquina no
#     viajen dentro del binario, y con un SHA256SUMS al lado.
#
# Sin -ldflags "-s -w": quitar los simbolos dispara falsos positivos de Defender
# sobre binarios de Go, y el binario que se firma tiene que ser el que se probo.
# Mismo criterio que release-seed.yml.
#
#   .\scripts\preparar-carga.ps1
#   .\scripts\preparar-carga.ps1 -Salida .\dist\carga -Motor ..\kanpachi-engine\target\release\kanpachi-engine.exe
#
# Los defaults son RELATIVOS AL REPOSITORIO. Antes apuntaban a C:\kt, que es un
# directorio de trabajo de una sola maquina y no existe en ninguna otra ni en el
# CI: cualquiera que clonara el repositorio y corriera esto se llevaba una carga
# escrita en una ruta que no significa nada.
#
# No hace falta elevar: esto solo compila y copia.

[CmdletBinding()]
param(
    [string]$Salida = "",
    # El motor viene del otro repositorio, y no se compila aca: es Rust, con su
    # propia cadena de herramientas. Vacio lo busca al lado de este repositorio.
    [string]$Motor = "",
    # La version que se esta empaquetando, sin la "v". La escribe el workflow
    # desde el tag; a mano queda en "dev".
    #
    # No es adorno: es lo unico con lo que la interfaz puede comparar la ultima
    # version publicada para saber si la suya se quedo vieja. Y "dev" no es un
    # relleno, es una respuesta: un build de desarrollo NO avisa de nuevas
    # versiones, porque no hay nada sensato con lo que comparar y porque quien
    # lo compilo no necesita que le digan que descargue un instalador.
    [string]$Version = "dev"
)

$ErrorActionPreference = 'Stop'

function Paso($t) { Write-Host ""; Write-Host "=== $t ===" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  OK  $t" -ForegroundColor Green }
function Mal($t) { Write-Host "  MAL $t" -ForegroundColor Red }
function Nota($t) { Write-Host "  --  $t" -ForegroundColor DarkGray }

# El repositorio sale de la ubicacion del script y jamas del directorio actual.
$repo = Split-Path -Parent $PSScriptRoot
$fallos = 0

if ([string]::IsNullOrWhiteSpace($Salida)) { $Salida = Join-Path $repo 'dist\carga' }
if ([string]::IsNullOrWhiteSpace($Motor)) {
    $Motor = Join-Path (Split-Path -Parent $repo) 'kanpachi-engine\target\release\kanpachi-engine.exe'
}

if (Test-Path $Salida) {
    # Se borra entera. Una carga con restos de una version anterior es
    # exactamente el fallo que un instalador no puede tener: se envia un fichero
    # que nadie compilo en esta vuelta.
    Remove-Item -Recurse -Force $Salida
    Nota "borrada la carga anterior"
}
New-Item -ItemType Directory -Force -Path $Salida | Out-Null

Paso "el daemon"

Push-Location $repo
try {
    $destino = Join-Path $Salida 'kanpachid.exe'
    # -H windowsgui: el acceso directo apunta a este binario, asi que uno de
    # subsistema consola abriria una ventana negra al hacer doble clic. La
    # salida de --console no se pierde: se reengancha a la consola del padre.
    & go build -trimpath -ldflags "-H windowsgui" -o $destino ./daemon/cmd/kanpachid
    if ($LASTEXITCODE -ne 0) { Mal "no compilo el daemon"; $fallos++ }
    else {
        $kb = [math]::Round((Get-Item $destino).Length / 1KB)
        Bien ("kanpachid.exe    {0} KB" -f $kb)
    }
}
finally { Pop-Location }

Paso "la interfaz"

Push-Location (Join-Path $repo 'ui')
try {
    Nota "version $Version"
    # --build-name ademas del dart-define, y son dos cosas distintas:
    #
    #   - el dart-define es lo que la ventana CREE que es, y con lo que compara
    #     contra la ultima publicada.
    #   - --build-name es lo que WINDOWS dice que es en las propiedades del
    #     .exe. Sin el, Flutter lo saca de la version de pubspec.yaml, que se
    #     escribe a mano y no la mueve nadie: la v0.2.0 salio con la ventana
    #     diciendo 0.1.2+3 en sus propiedades.
    #
    # Se le pasa la parte numerica: el campo de VERSIONINFO no admite sufijos,
    # igual que VersionInfoVersion de Inno. Un "dev" no es una version y ahi se
    # deja lo que traiga pubspec, que es la respuesta honesta para un build a
    # mano.
    $flutterArgs = @('build', 'windows', '--release', "--dart-define=KANPACHI_VERSION=$Version")
    $numerica = ($Version -split '-')[0]
    if ($numerica -match '^\d+\.\d+\.\d+$') {
        $flutterArgs += "--build-name=$numerica"
        Nota "propiedades del .exe: $numerica"
    }
    & flutter @flutterArgs 2>&1 |
        Select-Object -Last 3 | ForEach-Object { Nota $_ }
    if ($LASTEXITCODE -ne 0) {
        Mal "no compilo la interfaz"
        $fallos++
    }
    else {
        $bundle = Join-Path $repo 'ui\build\windows\x64\runner\Release'
        if (-not (Test-Path (Join-Path $bundle 'kanpachiui.exe'))) {
            Mal "el build no dejo kanpachiui.exe donde se esperaba"
            $fallos++
        }
        else {
            # El bundle ENTERO: el ejecutable no arranca sin flutter_windows.dll,
            # sin data\ y sin los plugins. Copiar solo el .exe da un binario que
            # muere en el arranque sin decir por que.
            Copy-Item -Path (Join-Path $bundle '*') -Destination $Salida -Recurse -Force
            $kb = [math]::Round((Get-Item (Join-Path $Salida 'kanpachiui.exe')).Length / 1KB)
            Bien ("kanpachiui.exe   {0} KB, con su bundle" -f $kb)
        }
    }
}
finally { Pop-Location }

Paso "lo que no se compila aca"

$copias = @(
    @{ origen = 'daemon\adapter\catalog\jsonfile\builtin.json'; nombre = 'builtin.json' },
    @{ origen = 'third_party\easytier\Packet.dll';              nombre = 'Packet.dll' },
    @{ origen = 'third_party\easytier\wintun.dll';              nombre = 'wintun.dll' },
    # Faltaba, y docs/03 lo lista dentro del directorio de instalacion desde
    # siempre. La carpeta portable si lo copiaba, asi que la carga que se
    # empaqueta era la unica de las dos formas de entregar Kanpachi a la que le
    # faltaba un fichero.
    @{ origen = 'third_party\easytier\WinDivert64.sys';         nombre = 'WinDivert64.sys' }
)
foreach ($c in $copias) {
    $src = Join-Path $repo $c.origen
    if (-not (Test-Path $src)) { Mal ("falta " + $c.origen); $fallos++; continue }
    Copy-Item -Path $src -Destination (Join-Path $Salida $c.nombre) -Force
    Bien $c.nombre
}

Paso "el motor, que viene de otro repositorio"

if (Test-Path $Motor) {
    Copy-Item -Path $Motor -Destination (Join-Path $Salida 'kanpachi-engine.exe') -Force
    $mb = [math]::Round((Get-Item $Motor).Length / 1MB, 1)
    Bien ("kanpachi-engine.exe  {0} MB" -f $mb)
}
else {
    Mal "no esta el motor en $Motor"
    Nota "compilalo en kanpachi-engine y pasa su ruta con -Motor"
    $fallos++
}

Paso "las sumas"

# Un SHA256SUMS al lado de la carga, para que quien la reciba pueda comprobar
# que le llego lo mismo que salio. Mismo criterio que release-seed.yml.
$lineas = Get-ChildItem -Path $Salida -Recurse -File |
    Where-Object { $_.Name -ne 'SHA256SUMS' } |
    Sort-Object FullName |
    ForEach-Object {
        $rel = $_.FullName.Substring($Salida.Length).TrimStart('\')
        $h = (Get-FileHash -Algorithm SHA256 $_.FullName).Hash.ToLower()
        "$h  $rel"
    }
$lineas | Out-File -FilePath (Join-Path $Salida 'SHA256SUMS') -Encoding ascii
Bien ("SHA256SUMS con {0} ficheros" -f $lineas.Count)

Paso "resultado"

if ($fallos -gt 0) {
    Mal "$fallos problema(s): la carga NO esta lista"
    exit 1
}
$total = [math]::Round(((Get-ChildItem -Path $Salida -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
Bien "la carga esta en $Salida, $total MB"
