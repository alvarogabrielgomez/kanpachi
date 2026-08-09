<#
.SYNOPSIS
    Arma kanpachi-portable.exe: Kanpachi portable entero en UN solo ejecutable.

.DESCRIPTION
    Lo que sale de aqui es el archivo que se le pasa a un amigo por chat. Lleva
    dentro el daemon, la interfaz con todo su bundle de Flutter, el motor, las
    DLL y el marcador. Del otro lado se hace doble clic, Windows pide permiso de
    administrador una vez, y Kanpachi abre.

    NO INSTALA NADA. Ni servicio, ni arranque con Windows, ni accesos directos,
    ni ProgramData. Suelta todo en una carpeta temporal, corre el daemon
    portable y borra la carpeta al cerrarse. Lo unico que queda es kanpachi.log,
    junto al ejecutable, que es lo que se pide cuando algo falla.

    COMO SE ARMA, en tres pasos

      1. kanpachi-portable.ps1 -NoArrancar arma la carpeta portable de verdad.
         Es la misma que se usa a mano, asi que no hay dos recetas que se puedan
         desincronizar.
      2. Esa carpeta se copia a internal\kanpachibundle\carga\, porque go:embed
         solo empotra cosas DENTRO del directorio de su paquete: no acepta ..\.
      3. go build -tags bundle. Sin esa etiqueta el binario compila igual y se
         niega a correr diciendo por que, que es a proposito: un go build ./...
         normal no puede producir un bundle vacio que falla recien en la maquina
         de otra persona.

    LO QUE HAY QUE SABER ANTES DE MANDARLO

    Va sin firmar, asi que SmartScreen lo va a recibir con "Editor desconocido"
    y puede que Defender avise. Un ejecutable que suelta binarios y un driver en
    una carpeta temporal y pide administrador tiene la forma exacta de un
    dropper. Quien lo reciba tiene que pulsar "Mas informacion" y "Ejecutar de
    todas formas".

.PARAMETER Salida
    Donde queda el .exe. Por defecto dist\kanpachi-portable.exe.

.PARAMETER Motor
    Ruta del motor, si se quiere forzar una. Vacio busca el mas RECIENTE entre
    los sitios donde queda compilado. En los dos casos se comprueba que no sea
    mas viejo que el codigo del motor, y si lo es esto se planta.

.PARAMETER RecompilarMotor
    Recompila el motor desde su repositorio antes de empaquetar.

.PARAMETER ConservarCarga
    No borra internal\kanpachibundle\carga\ al terminar. Solo para depurar el
    propio bundle: son 90 MB dentro del arbol de codigo.

.EXAMPLE
    .\scripts\build_portable_bundle.ps1

.EXAMPLE
    .\scripts\build_portable_bundle.ps1 -Salida D:\reparto\kanpachi.exe
#>
[CmdletBinding()]
param(
    [string]$Salida,
    [string]$Motor,
    [switch]$RecompilarMotor,
    [switch]$ConservarCarga
)

$ErrorActionPreference = 'Stop'

function Paso($t) { Write-Host ''; Write-Host "=== $t ===" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  OK  $t" -ForegroundColor Green }
function Mal($t) { Write-Host "  MAL $t" -ForegroundColor Red }
function Nota($t) { Write-Host "  --  $t" -ForegroundColor DarkGray }

# El repositorio sale de la ubicacion del script y jamas del directorio actual:
# una consola elevada arranca en system32.
$repo = Split-Path -Parent $PSScriptRoot
if (-not $Salida) { $Salida = Join-Path $repo 'dist\kanpachi-portable.exe' }

$dirCarga = Join-Path $repo 'internal\kanpachibundle\carga'
# El stage se arma fuera del arbol de codigo y se copia despues. Armarlo
# directamente dentro de carga\ dejaria un arbol a medias si algo falla por el
# camino, y el siguiente build lo empotraria sin quejarse.
$stage = Join-Path ([System.IO.Path]::GetTempPath()) ("kanpachi-bundle-stage-" + [System.Guid]::NewGuid().ToString('N'))

Write-Host ''
Write-Host 'kanpachi-portable: un solo ejecutable con todo dentro' -ForegroundColor White

# ---------------------------------------------------------------------------
Paso 'la puerta del CI, corrida aqui'

# El job de Linux hace `go vet ./internal/...`, y un bundle nuevo es justo el
# tipo de paquete que se lo rompe importando x/sys/windows sin etiqueta. Cuesta
# segundos y evita descubrirlo despues de un push.
$env:GOOS = 'linux'
try {
    Push-Location $repo
    try {
        go vet ./internal/...
        if ($LASTEXITCODE -ne 0) { throw 'el paquete del bundle rompe el job de Linux del CI' }
    } finally { Pop-Location }
} finally { Remove-Item Env:GOOS }
Bien 'go vet de Linux limpio'

# ---------------------------------------------------------------------------
Paso 'el motor, que viene de otro repositorio'

# Esto NO es celo de mas, y ya mordio: el default anterior apuntaba a
# C:\kt\stage a secas, que tenia un motor de tres dias antes, sin el commit
# "turn secure mode on for the host and the lobby too". Ese es justo el que hace
# que el host acepte a los invitados a los que el mismo le dio credencial, o sea
# que el bundle habria salido con el producto roto y sin nada que lo dijera.
#
# El bundle es lo que se le manda a otra persona. Un motor viejo ahi dentro no
# se descubre aca: se descubre en su maquina, sin log del lado del que lo armo.
$raizMotor = Join-Path (Split-Path -Parent $repo) 'kanpachi-engine'

if ($RecompilarMotor) {
    if (-not (Test-Path (Join-Path $raizMotor 'Cargo.toml'))) {
        throw "no esta el repo kanpachi-engine en $raizMotor"
    }
    $buildMotor = Join-Path $raizMotor 'scripts\build.ps1'
    if (-not (Test-Path $buildMotor)) {
        throw "falta $buildMotor, que prepara MSVC, protoc, libclang y 7-Zip antes de compilar"
    }
    Nota 'recompilando el motor, esto tarda'
    & $buildMotor
    if ($LASTEXITCODE -ne 0) { throw 'no compilo el motor' }
}

if (-not $Motor) {
    # El MAS RECIENTE de los que existan, y no el primero que aparezca. Son
    # varios sitios porque el motor se compila de varias formas, y cual de ellos
    # tiene el bueno depende de como se compilo la ultima vez. Elegir por orden
    # fijo es elegir por costumbre.
    $Motor = @(
        'C:\kt\release\kanpachi-engine.exe',
        'C:\kt\stage\kanpachi-engine.exe',
        (Join-Path $raizMotor 'target\release\kanpachi-engine.exe')
    ) | Where-Object { Test-Path $_ } |
        Sort-Object { (Get-Item $_).LastWriteTime } -Descending |
        Select-Object -First 1
}

if (-not $Motor -or -not (Test-Path $Motor)) {
    Mal 'no hay ningun motor compilado'
    Nota 'compilalo con -RecompilarMotor, o pasa su ruta con -Motor'
    throw 'sin motor no hay bundle: se puede hablar con el daemon, no se puede abrir una sala'
}
$infoMotor = Get-Item $Motor
Bien ("motor: {0}" -f $Motor)
Nota ("compilado el {0}, {1} MB" -f $infoMotor.LastWriteTime, [math]::Round($infoMotor.Length / 1MB, 1))

# La comprobacion que importa: que el binario sea posterior a su propio codigo.
if (Test-Path $raizMotor) {
    $fuentes = @(Get-ChildItem (Join-Path $raizMotor 'src') -Recurse -File -ErrorAction SilentlyContinue) +
               @(Get-ChildItem $raizMotor -File -Filter 'Cargo.toml' -ErrorAction SilentlyContinue) +
               @(Get-ChildItem $raizMotor -File -Filter 'build.rs' -ErrorAction SilentlyContinue)
    $masNueva = $fuentes | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if ($masNueva -and $masNueva.LastWriteTime -gt $infoMotor.LastWriteTime) {
        Mal 'el motor es MAS VIEJO que su codigo fuente'
        Nota ("  {0} es del {1}" -f $masNueva.Name, $masNueva.LastWriteTime)
        Nota ("  el binario es del {0}" -f $infoMotor.LastWriteTime)
        Nota 'recompilalo con -RecompilarMotor, o pasa el bueno con -Motor'
        throw 'no se empaqueta un motor viejo: el fallo aparece en la maquina de la otra persona'
    }
    Bien 'el motor es posterior a su codigo, no esta viejo'
}
else {
    Nota "no esta el repo del motor en $raizMotor, no se puede comprobar si esta viejo"
}

# ---------------------------------------------------------------------------
Paso 'armando la carpeta portable'

# La MISMA receta que se usa a mano. -NoArrancar porque aqui solo se empaqueta,
# y -Limpio para que no se cuele la llave de identidad ni la ultima sala de una
# corrida anterior: eso viajaria dentro del .exe hasta la maquina de otra
# persona.
& (Join-Path $PSScriptRoot 'kanpachi-portable.ps1') -Salida $stage -Motor $Motor -NoArrancar -Limpio
if ($LASTEXITCODE -ne 0) { throw 'no se pudo armar la carpeta portable' }

# ---------------------------------------------------------------------------
Paso 'comprobando que la carpeta esta completa'

# Por su NOMBRE, uno a uno. Un bundle al que le falte Packet.dll arranca igual
# y muere despues dentro del motor con un 0xC0000135 que no menciona ningun
# archivo; sin flutter_windows.dll la interfaz se cierra sin decir nada.
$imprescindibles = @(
    'kanpachid.exe',
    'kanpachiui.exe',
    'kanpachi-engine.exe',
    'flutter_windows.dll',
    'wintun.dll',
    'Packet.dll',
    'builtin.json',
    'kanpachi.portable',
    'data\app.so',
    'data\icudtl.dat'
)
foreach ($f in $imprescindibles) {
    if (-not (Test-Path (Join-Path $stage $f))) {
        Mal "falta en la carpeta portable: $f"
        throw "la carpeta portable esta incompleta, no se arma un bundle roto"
    }
}
Bien "$($imprescindibles.Count) piezas imprescindibles, todas presentes"

# Que el motor que quedo en la carpeta sea EL QUE SE ELIGIO arriba. Comprobar
# la eleccion y empaquetar otra cosa es el fallo que toda esta seccion existe
# para impedir, y son dos scripts distintos los que deciden y copian.
#
# Por hash y no por tamano: dos compilaciones del mismo motor pesan casi igual,
# asi que el tamano diria que si justo en el caso que hay que cazar.
$hashElegido = (Get-FileHash $Motor -Algorithm SHA256).Hash
$hashEnStage = (Get-FileHash (Join-Path $stage 'kanpachi-engine.exe') -Algorithm SHA256).Hash
if ($hashElegido -ne $hashEnStage) {
    Mal 'el motor de la carpeta no es el que se comprobo'
    Nota "  elegido:  $hashElegido"
    Nota "  empaquetado: $hashEnStage"
    throw 'se empaqueto un motor distinto del comprobado'
}
Bien ("el motor empaquetado es el comprobado (SHA256 {0}...)" -f $hashElegido.Substring(0, 12))

# El directorio de datos NO viaja. Lo crea el daemon en destino, y meterlo aqui
# mandaria la llave de identidad de ESTA maquina dentro del ejecutable.
$datosStage = Join-Path $stage 'kanpachi-data'
if (Test-Path $datosStage) {
    Remove-Item $datosStage -Recurse -Force
    Nota 'quitado kanpachi-data\: los datos se crean en la maquina de destino'
}

# ---------------------------------------------------------------------------
Paso 'copiando la carga dentro del paquete'

if (-not (Test-Path $dirCarga)) { New-Item -ItemType Directory -Path $dirCarga | Out-Null }
Get-ChildItem $dirCarga -Exclude '.gitignore' -Force -ErrorAction SilentlyContinue |
    Remove-Item -Recurse -Force
Copy-Item -Path (Join-Path $stage '*') -Destination $dirCarga -Recurse -Force

$mbCarga = [math]::Round(((Get-ChildItem $dirCarga -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
Bien "carga lista ($mbCarga MB)"

# ---------------------------------------------------------------------------
Paso 'compilando el bundle'

$dirSalida = Split-Path -Parent $Salida
if ($dirSalida -and -not (Test-Path $dirSalida)) { New-Item -ItemType Directory -Path $dirSalida | Out-Null }

Push-Location $repo
try {
    # -H windowsgui: SIN consola. Con ella quedaba una ventana negra abierta
    # toda la sesion de juego y un SEGUNDO icono en la barra de tareas, que hace
    # parecer que Kanpachi se abrio dos veces. Lo que se pierde son los mensajes
    # de progreso; lo que queda para un fallo es un cuadro de dialogo, que es
    # mas visible que una consola que nadie mira.
    go build -tags bundle -ldflags '-H=windowsgui' -o $Salida ./internal/kanpachibundle
    if ($LASTEXITCODE -ne 0) { throw 'no compilo el bundle' }
} finally {
    Pop-Location
    if (-not $ConservarCarga) {
        # Se borra siempre: son 90 MB dentro del arbol de codigo, y dejarlos ahi
        # hace lento el proximo `go build ./...` y asusta en un `git status`.
        Get-ChildItem $dirCarga -Exclude '.gitignore' -Force -ErrorAction SilentlyContinue |
            Remove-Item -Recurse -Force
    }
    if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
}

$mb = [math]::Round((Get-Item $Salida).Length / 1MB, 1)
Bien "kanpachi-portable.exe listo ($mb MB)"

Write-Host ''
Write-Host "  Esto es lo que se manda por chat:" -ForegroundColor Cyan
Write-Host "  $Salida" -ForegroundColor Cyan
Write-Host ''
Nota 'Del otro lado: doble clic, aceptar el UAC una vez, y listo.'
Nota 'Va sin firmar: SmartScreen dira "Editor desconocido". Mas informacion > Ejecutar de todas formas.'
Nota 'Si algo falla, el log queda como kanpachi.log junto al .exe.'
