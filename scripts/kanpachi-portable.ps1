<#
.SYNOPSIS
    Arma una carpeta de Kanpachi que funciona sola, y la arranca.

.DESCRIPTION
    Una sola orden que compila todo lo que hace falta, lo deja en una carpeta, y
    levanta el daemon con los permisos que necesita. Sustituye a la secuencia
    manual de compilar el daemon, compilar la interfaz, copiar el catalogo y las
    DLL, abrir una terminal elevada y arrancar cada cosa por su lado.

    QUE DEJA, y por que la carpeta es portable

    Dentro va el producto entero: el daemon, la interfaz con su bundle de
    Flutter, el catalogo de fabrica, el motor y sus DLL. Ademas un fichero
    kanpachi.portable, que es lo que convierte a la carpeta en portable: con el
    presente, el daemon guarda sus datos en kanpachi-data\ ahi mismo en vez de
    en ProgramData, y arranca en su propio proceso en vez de como servicio. La
    interfaz lee el mismo marcador y llega a la misma conclusion, sin que nadie
    se lo diga.

    Asi que la carpeta se copia a un USB, se manda en un ZIP, y del otro lado se
    hace doble clic en kanpachid.exe. No hay nada que instalar y no hay nada que
    desinstalar: se borra la carpeta y no queda rastro.

    LO QUE CUESTA, dicho claro

      - Un UAC por arranque. La version instalada pide uno solo, al instalar, y
        a cambio el instalador le concede al usuario permiso para arrancar el
        servicio. Una carpeta que se copio no concedio nada, asi que eleva cada
        vez.
      - Los datos heredan los permisos de donde este la carpeta. La version
        instalada pone una ACL propia en ProgramData; aca no hay instalador que
        la ponga.
      - No arranca con Windows. No hay servicio registrado que Windows pueda
        levantar.

    LOS DOS MODOS

      prod   (por defecto)  interfaz en release, daemon como daemon portable.
                            Es lo que se le manda a una persona.
      debug                 interfaz en debug hablando por el pipe de consola,
                            daemon con --console en una terminal elevada donde
                            se le ve el log.

.EXAMPLE
    .\scripts\kanpachi-portable.ps1
    Arma .\Kanpachi y lo arranca en modo produccion.

.EXAMPLE
    .\scripts\kanpachi-portable.ps1 debug
    Lo mismo en modo desarrollo: daemon de consola a la vista, interfaz en debug.

.EXAMPLE
    .\scripts\kanpachi-portable.ps1 -Salida D:\reparto\Kanpachi -NoArrancar
    Solo arma la carpeta, para comprimirla y mandarla.
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('prod', 'debug')]
    [string]$Modo = 'prod',

    # Donde se arma. Por defecto una subcarpeta del directorio actual, y no el
    # directorio actual a secas: la carpeta ES la unidad que se copia, y
    # mezclada con lo que ya hubiera al lado no se puede ni comprimir.
    [string]$Salida = (Join-Path (Get-Location).Path 'Kanpachi'),

    # El motor viene de otro repositorio. Por defecto se busca en el stage.
    [string]$Motor = 'C:\kt\stage\kanpachi-engine.exe',

    # Armar y no arrancar. Es lo que se quiere antes de comprimir.
    [switch]$NoArrancar,

    # Borrar tambien data\. Se lleva por delante la llave de esta instalacion y
    # la ultima sala, asi que no es el default.
    [switch]$Limpio
)

$ErrorActionPreference = 'Stop'

function Paso($t) { Write-Host ''; Write-Host "=== $t ===" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  OK  $t" -ForegroundColor Green }
function Mal($t) { Write-Host "  MAL $t" -ForegroundColor Red }
function Nota($t) { Write-Host "  --  $t" -ForegroundColor DarkGray }

# El repositorio sale de la ubicacion del script y jamas del directorio actual:
# una consola elevada arranca en system32.
$repo = Split-Path -Parent $PSScriptRoot
$fallos = 0

$daemonExe = Join-Path $Salida 'kanpachid.exe'
$uiExe = Join-Path $Salida 'kanpachiui.exe'
$marcador = Join-Path $Salida 'kanpachi.portable'
# kanpachi-data y NO data: el bundle de Windows de Flutter trae su propio data\
# con icudtl.dat, app.so y flutter_assets\, y aca los dos ejecutables comparten
# directorio. Se midio en la primera corrida de este script: se mezclaron. El
# nombre esta escrito igual en portable.go y en pipe_names.dart.
$datos = Join-Path $Salida 'kanpachi-data'

Write-Host ''
Write-Host "Kanpachi portable, modo $Modo" -ForegroundColor White
Nota "salida: $Salida"

# ---------------------------------------------------------------------------
Paso 'apagando lo que hubiera corriendo de esta carpeta'

# Hay que hacerlo ANTES de compilar: Windows bloquea el .exe de un proceso vivo,
# y go build fallaria con un mensaje de acceso denegado que no dice nada de
# esto. Se filtra por RUTA y no por nombre, para no tumbar un Kanpachi
# instalado que no tiene nada que ver con esta carpeta.
$vivos = @()
foreach ($nombre in @('kanpachid', 'kanpachiui', 'kanpachi-engine')) {
    try {
        $vivos += Get-Process -Name $nombre -ErrorAction Stop |
            Where-Object { $_.Path -and $_.Path.StartsWith($Salida, [System.StringComparison]::OrdinalIgnoreCase) }
    }
    catch {
        # No hay ninguno con ese nombre. Es el caso normal.
        Write-Verbose "sin procesos $nombre : $($_.Exception.Message)"
    }
}

if ($vivos.Count -eq 0) {
    Nota 'no habia nada corriendo'
}
else {
    foreach ($p in $vivos) {
        try {
            Stop-Process -Id $p.Id -Force -ErrorAction Stop
            Bien ("detenido {0} (pid {1})" -f $p.ProcessName, $p.Id)
        }
        catch {
            # El daemon corre elevado. Desde una terminal sin elevar no se le
            # puede tocar, y el sintoma sin esta explicacion seria un go build
            # que falla por acceso denegado.
            Mal ("no se pudo detener {0} (pid {1}): {2}" -f $p.ProcessName, $p.Id, $_.Exception.Message)
            Nota 'cierralo desde el icono de la bandeja, o corre esto en una terminal elevada'
            $fallos++
        }
    }
    # El job del daemon se lleva la interfaz por delante, y eso tarda un
    # instante en verse en el sistema de ficheros.
    Start-Sleep -Milliseconds 700
}

if ($fallos -gt 0) { exit 1 }

# ---------------------------------------------------------------------------
Paso 'preparando la carpeta'

if (Test-Path $Salida) {
    # Se borra el contenido y NO la carpeta: kanpachi-data\ sobrevive salvo que
    # se pida lo contrario. Ahi dentro esta la llave de esta instalacion, que es
    # lo que impide que otro se haga pasar por este equipo ante quien ya jugo con
    # el, y tirarla en cada compilacion convertiria a cada build en un equipo
    # nuevo.
    Get-ChildItem -Path $Salida -Force | Where-Object {
        $Limpio -or $_.Name -ne 'kanpachi-data'
    } | Remove-Item -Recurse -Force
    if ($Limpio) { Nota 'borrada tambien kanpachi-data\' }
    else { Nota 'conservada kanpachi-data\' }
}
else {
    New-Item -ItemType Directory -Force -Path $Salida | Out-Null
}

# ---------------------------------------------------------------------------
Paso 'el daemon'

Push-Location $repo
try {
    # -H windowsgui: se hace doble clic en este binario, asi que uno de
    # subsistema consola abriria una ventana negra. La salida de --console no se
    # pierde: se reengancha a la consola del padre cuando la hay, que es
    # exactamente lo que hace el modo debug de aca abajo.
    #
    # Sin -ldflags "-s -w": quitar los simbolos dispara falsos positivos de
    # Defender sobre binarios de Go, y el binario que se manda tiene que ser el
    # que se probo.
    & go build -trimpath -ldflags '-H windowsgui' -o $daemonExe ./daemon/cmd/kanpachid
    if ($LASTEXITCODE -ne 0) {
        Mal 'no compilo el daemon'
        $fallos++
    }
    else {
        $kb = [math]::Round((Get-Item $daemonExe).Length / 1KB)
        Bien ("kanpachid.exe        {0} KB" -f $kb)
    }
}
finally { Pop-Location }

# ---------------------------------------------------------------------------
Paso 'la interfaz'

# En debug se compila apuntando al pipe de consola. Es una definicion de
# COMPILACION y no una opcion en disco a proposito: un binario publicado tiene
# esa rama podada, asi que ningun fichero en la maquina de nadie puede apuntar
# la app a un pipe que cualquier proceso puede crear.
if ($Modo -eq 'debug') {
    $flutterArgs = @('build', 'windows', '--debug', '--dart-define=KANPACHI_CONSOLE_PIPE=true')
    $bundle = Join-Path $repo 'ui\build\windows\x64\runner\Debug'
}
else {
    $flutterArgs = @('build', 'windows', '--release')
    $bundle = Join-Path $repo 'ui\build\windows\x64\runner\Release'
}

Push-Location (Join-Path $repo 'ui')
try {
    & flutter @flutterArgs 2>&1 | Select-Object -Last 3 | ForEach-Object { Nota $_ }
    if ($LASTEXITCODE -ne 0) {
        Mal 'no compilo la interfaz'
        $fallos++
    }
    elseif (-not (Test-Path (Join-Path $bundle 'kanpachiui.exe'))) {
        Mal "el build no dejo kanpachiui.exe en $bundle"
        $fallos++
    }
    else {
        # El bundle ENTERO. El ejecutable no arranca sin flutter_windows.dll, sin
        # data\ y sin los plugins: copiar solo el .exe da un binario que muere en
        # el arranque sin decir por que.
        Copy-Item -Path (Join-Path $bundle '*') -Destination $Salida -Recurse -Force
        $kb = [math]::Round((Get-Item $uiExe).Length / 1KB)
        Bien ("kanpachiui.exe       {0} KB, con su bundle" -f $kb)
    }
}
finally { Pop-Location }

# ---------------------------------------------------------------------------
Paso 'lo que no se compila'

$copias = @(
    @{ origen = 'daemon\adapter\catalog\jsonfile\builtin.json'; nombre = 'builtin.json'; obligatorio = $true },
    @{ origen = 'third_party\easytier\Packet.dll'; nombre = 'Packet.dll'; obligatorio = $true },
    @{ origen = 'third_party\easytier\wintun.dll'; nombre = 'wintun.dll'; obligatorio = $true },
    @{ origen = 'third_party\easytier\WinDivert64.sys'; nombre = 'WinDivert64.sys'; obligatorio = $false }
)
foreach ($c in $copias) {
    $src = Join-Path $repo $c.origen
    if (-not (Test-Path $src)) {
        if ($c.obligatorio) {
            Mal ('falta en el repositorio: ' + $c.origen)
            $fallos++
        }
        else {
            Nota ('no esta y no es obligatorio: ' + $c.origen)
        }
        continue
    }
    Copy-Item -Path $src -Destination (Join-Path $Salida $c.nombre) -Force
    Bien $c.nombre
}

# ---------------------------------------------------------------------------
Paso 'el motor, que viene de otro repositorio'

if (Test-Path $Motor) {
    Copy-Item -Path $Motor -Destination (Join-Path $Salida 'kanpachi-engine.exe') -Force
    $mb = [math]::Round((Get-Item $Motor).Length / 1MB, 1)
    Bien ('kanpachi-engine.exe  {0} MB' -f $mb)
}
else {
    Mal "no esta el motor en $Motor"
    Nota 'compilalo en el repositorio kanpachi-engine y pasa su ruta con -Motor'
    Nota 'sin el se puede hablar con el daemon, no se puede abrir una sala'
    $fallos++
}

# ---------------------------------------------------------------------------
Paso 'el marcador'

# ASCII y una sola linea. Lo lee un os.Stat en Go y un existsSync en Dart:
# ninguno de los dos mira el contenido, que esta ahi para quien abra la carpeta
# y se pregunte que hace este fichero.
'Kanpachi portable. Con este fichero presente, el daemon guarda sus datos en kanpachi-data\ y corre sin servicio. Borralo para volver a ProgramData.' |
    Out-File -FilePath $marcador -Encoding ascii
Bien 'kanpachi.portable'

if (-not (Test-Path $datos)) {
    # El daemon lo crearia solo en su primer arranque. Se hace aca para que la
    # carpeta que se comprime ya tenga su sitio y no dependa de haberla
    # arrancado una vez.
    New-Item -ItemType Directory -Force -Path $datos | Out-Null
    Bien 'kanpachi-data\'
}

# ---------------------------------------------------------------------------
Paso 'resultado'

if ($fallos -gt 0) {
    Mal "$fallos problema(s): la carpeta NO esta lista"
    exit 1
}

$total = [math]::Round(((Get-ChildItem -Path $Salida -Recurse -File | Measure-Object Length -Sum).Sum / 1MB), 1)
Bien "la carpeta esta en $Salida, $total MB"

if ($NoArrancar) {
    Nota 'no se arranca nada: -NoArrancar'
    Nota 'comprimela entera y del otro lado doble clic en kanpachid.exe'
    exit 0
}

# ---------------------------------------------------------------------------
Paso 'arrancando'

if ($Modo -eq 'debug') {
    # El daemon de consola, elevado y CON consola a la vista.
    #
    # Va por cmd.exe /k y no directo, y hace falta: kanpachid.exe esta enlazado
    # con -H windowsgui, asi que no crea consola propia; lo que hace es
    # engancharse a la del padre. Arrancado con -Verb RunAs no hay padre con
    # consola, y todo el log del daemon iria a ningun sitio, que es justo lo que
    # el modo debug existe para evitar.
    #
    # Elevado porque el pipe vive bajo ProtectedPrefix\Administrators y escribir
    # en el firewall exige administrador.
    Nota 'el daemon abre su propia terminal elevada. Windows va a pedir permiso'
    Start-Process -FilePath 'cmd.exe' -Verb RunAs -ArgumentList @('/k', ('"' + $daemonExe + '" --console'))
    Bien 'daemon en modo consola'

    # La interfaz aparte: el daemon de consola NO hospeda la interfaz, a
    # proposito. Quien usa --console tiene una terminal delante y la arranca
    # cuando quiere; levantarle una ventana en cada arranque taparia el caso que
    # el producto de verdad tiene que resolver, que es el daemon lanzandola el.
    #
    # Se le da un respiro al daemon antes: la interfaz pregunta por el catalogo
    # al abrirse, y contra un pipe que todavia no escucha enseniaria el cartel
    # de que no hay servicio nada mas arrancar.
    Start-Sleep -Seconds 3
    Start-Process -FilePath $uiExe -ArgumentList '--show'
    Bien 'interfaz en modo debug'
    Nota 'la interfaz habla por el pipe de consola, que es el que abrio ese daemon'
}
else {
    # El camino de verdad, el mismo que hace un doble clic: esto arranca el
    # LANZADOR. El sondea el pipe, ve que no hay nadie, y se relanza a si mismo
    # elevado como daemon portable. El daemon abre la interfaz.
    #
    # --show porque quien acaba de correr esto esta mirando.
    Nota 'Windows va a pedir permiso de administrador: lo pide el daemon portable'
    Start-Process -FilePath $daemonExe -ArgumentList '--show'
    Bien 'Kanpachi arrancando'
    Nota "el log del daemon queda en $datos\logs\kanpachid.log"
}
