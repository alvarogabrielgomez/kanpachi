<#
.SYNOPSIS
  Mide la Proteccion Kanpachi con dos maquinas de verdad, en tres fases.

.DESCRIPTION
  Este encabezado ES el runbook. Antes vivia suelto en el texto de uso de
  fwprobe, o sea que solo lo leia quien ya sabia que existia.

  # Que se mide, y por que hacen falta TRES fases

  El 2026-08-04 se midio contra el droplet por Tailscale que Windows no
  devuelve RST hacia dentro ni con el puerto permitido. Es el modo sigiloso, y
  su consecuencia manda sobre todo este diseno: un puerto callado no distingue
  "bloqueado" de "no hay nadie escuchando".

  El canario quita esa ambiguedad por el unico camino que queda: pone a alguien
  detras de la puerta A PROPOSITO. Sabiendo con certeza que hay un oyente, el
  silencio pasa a tener una sola lectura.

  Con dos fases eso todavia no prueba nada sobre la DIRECCION del cambio. Si el
  firewall ya estaba puesto de una corrida anterior, "silencio" no dice quien lo
  puso. Por eso son tres:

    Fase 0   compuerta purgada    el canario CONTESTA por TCP y por UDP, y lo tocan
    Fase 1   compuerta PUESTA     SILENCIO por los dos, y NO lo tocan
    Fase 2   compuerta purgada    vuelve a contestar por los dos, y lo tocan

  La fase 2 no es opcional. Sin ella, una fase 1 en silencio se explica igual
  con "la compuerta funciona" que con "el canario nunca llego a abrir".

  # Las dos afirmaciones que cazan defectos de verdad

  UDP ademas de TCP en la fase 1. Un silencio solo de TCP pasa con una compuerta
  que solo bloquea TCP, y UDP es el protocolo por el que habla justo la
  herramienta que mas preocupa.

  Touched tiene que COINCIDIR con el informe remoto en las tres fases. Un
  desacuerdo es un bug del canario o de la sonda, y es exactamente el
  CanaryMismatch que el dominio modela: el host dice una cosa y el miembro otra.

  # La disciplina

  La compuerta sube o baja en su PROPIO paso, separado del que mide. Un estado
  suelto no prueba nada: lo que se mide es la transicion.

  El finally corre en todo camino, incluido Ctrl+C: purga la compuerta y mata el
  canario. La compuerta no puede sobrevivir a una corrida fallida, y un socket
  abierto en un proceso vecino a SYSTEM tampoco.

.PARAMETER Remote
  Donde correr la sonda, en forma de ssh. Ej: usuario@100.64.1.5

.PARAMETER LocalIP
  La IP de ESTA maquina en el adaptador virtual, que es donde liga el canario.
  Jamas 0.0.0.0: ligar en todas las interfaces abriria un puerto en la red de
  casa de quien lo corre, y el propio adaptador lo rechaza.

.PARAMETER Adapter
  Nombre del adaptador virtual, el de `fwprobe adapters`.

.PARAMETER PeerIP
  La IP virtual de la otra maquina, para el permiso de la compuerta.

.PARAMETER DataDir
  Directorio de datos que usa fwprobe.

.PARAMETER RemoteFwprobe
  Ruta del binario en la otra maquina. Tiene que estar ahi antes de empezar:

    GOOS=linux go build -o /tmp/fwprobe ./internal/fwprobe
    scp /tmp/fwprobe usuario@100.64.1.5:~/fwprobe

.EXAMPLE
  # Consola ELEVADA, porque la compuerta escribe en el motor de filtrado.
  pwsh scripts/canario-dos-maquinas.ps1 `
      -Remote usuario@100.64.1.5 -LocalIP 100.64.1.1 `
      -Adapter kanpachi0 -PeerIP 100.64.1.5 -DataDir C:\kanpachi-datos
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Remote,
    [Parameter(Mandatory = $true)][string]$LocalIP,
    [Parameter(Mandatory = $true)][string]$Adapter,
    [Parameter(Mandatory = $true)][string]$PeerIP,
    [Parameter(Mandatory = $true)][string]$DataDir,
    [string]$RemoteFwprobe = './fwprobe',

    # El rango de la sala. Tiene que ser un /24 dentro del espacio donde viven
    # las salas, y lo exige `wfp.Scope.Valid`: un prefijo mas ancho convertiria
    # el bloqueo de todo en el bloqueo de una red que no es nuestra.
    #
    # Existe como parametro porque el bloqueo de la compuerta se emite DOS veces,
    # por adaptador y por rango. Dejando el de por defecto, el segundo no casaria
    # con el trafico real de esta prueba y quedaria el de adaptador como asidero
    # unico, que es justo la situacion que el diseno emite dos filtros para
    # evitar. Se pasa el /24 que contiene a -LocalIP.
    [string]$RoomCIDR = '100.64.1.0/24',

    # El puerto que la compuerta deja abierto en la fase 1. NO es el del
    # canario: el canario coge un efimero que nadie elige, y la gracia es
    # justamente que caiga en "todo lo demas". Este existe porque la compuerta
    # necesita al menos un permiso para no ser un bloqueo total.
    [int]$OpenPort = 45871,

    # Presupuestos, contra la linea base medida de 3376 ms para una ronda real
    # con ssh incluido. Un plazo generoso deja de ser una red de seguridad.
    [int]$PhaseTimeoutSec = 15,
    [int]$TotalTimeoutSec = 60
)

$ErrorActionPreference = 'Stop'

# La raiz del repo se deduce del sitio del script, JAMAS del directorio actual.
#
# Medido: lanzado con UAC, el proceso elevado arranca en system32, asi que un
# `go build ./internal/fwprobe` relativo no encuentra nada. Y esta es la forma
# en la que este script se va a correr siempre, porque las fases 1 y 2 escriben
# en el motor de filtrado y eso exige elevar.
$raiz = Split-Path -Parent $PSScriptRoot

# El binario se compila una vez y se reusa. Correrlo con `go run` metería el
# tiempo de compilación dentro del presupuesto de cada fase.
$fwprobe = Join-Path $env:TEMP 'kanpachi-fwprobe.exe'

# Estado que el finally necesita ver. Va acá arriba porque el finally puede
# correr por Ctrl+C antes de que la fase que lo llenaría haya empezado.
$script:canario = $null
$script:compuertaPuesta = $false
$script:fallos = @()

function Write-Paso {
    param([string]$Texto)
    Write-Host ''
    Write-Host "== $Texto" -ForegroundColor Cyan
}

# Todo comando se imprime antes de correrlo. Sin eso, una corrida que sale mal
# no se puede reproducir a mano, que es lo primero que uno quiere hacer.
#
# # El fallo se decide por el CODIGO DE SALIDA, jamas por stderr
#
# Con `$ErrorActionPreference = 'Stop'` puesto arriba, un `2>&1` sobre un
# ejecutable nativo envuelve cada linea de su stderr en un ErrorRecord, y eso
# ABORTA el script aunque el programa haya terminado con exito. Medido: la
# primera corrida murio ahi.
#
# No es un detalle de esta funcion. `ssh` escribe avisos por stderr de forma
# rutinaria (host key nuevo, algoritmo obsoleto), asi que la fase 1 se habria
# caido sin decir por que, y una fase que no llega a medir se lee igual de mal
# que una que mide y falla.
#
# La solucion es la que corresponde: se baja la preferencia mientras corre el
# proceso hijo, se junta su stderr con su stdout para poder ensenarlo, y se
# juzga por $LASTEXITCODE, que es la senal que un exe usa de verdad para decir
# que fallo.
function Invoke-Mostrando {
    param([string]$Exe, [string[]]$Argumentos, [switch]$PermitirFallo)

    Write-Host "   $Exe $($Argumentos -join ' ')" -ForegroundColor DarkGray

    $anterior = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $salida = & $Exe @Argumentos 2>&1 | Out-String
        $codigo = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $anterior
    }

    if ($salida.Trim()) { Write-Host $salida.TrimEnd() }
    if ($codigo -ne 0 -and -not $PermitirFallo) {
        throw "$Exe salio con codigo $codigo"
    }
    return $salida
}

function Assert-Igual {
    param([string]$Que, $Esperado, $Real, [string]$PorQue)

    if ($Esperado -eq $Real) {
        Write-Host "   OK   $Que = $Real" -ForegroundColor Green
        return
    }
    Write-Host "   FALLA $Que : se esperaba $Esperado y llego $Real" -ForegroundColor Red
    Write-Host "         $PorQue" -ForegroundColor Red
    $script:fallos += "$Que : esperado $Esperado, real $Real. $PorQue"
}

# Arranca el canario y devuelve puerto y nonce.
#
# Se lanza como proceso APARTE y no en primer plano porque tiene que seguir
# escuchando mientras la otra maquina marca. Su salida va a un archivo, que es
# de donde se leen el puerto, el nonce y, al final, el veredicto propio.
function Start-Canario {
    param([string]$Addr)

    $log = Join-Path $env:TEMP "kanpachi-canario-$([System.Guid]::NewGuid().ToString('N')).txt"
    Write-Host "   $fwprobe canary -addr $Addr" -ForegroundColor DarkGray
    $p = Start-Process -FilePath $fwprobe -ArgumentList @('canary', '-addr', $Addr) `
        -RedirectStandardOutput $log -RedirectStandardError "$log.err" `
        -NoNewWindow -PassThru

    # Se espera a que IMPRIMA el puerto, no un tiempo fijo. Un sleep a ojo o
    # mide antes de que abra, o regala segundos del presupuesto de la fase.
    $limite = (Get-Date).AddSeconds(8)
    $puerto = 0
    $nonce = ''
    while ((Get-Date) -lt $limite) {
        if (Test-Path $log) {
            $texto = Get-Content $log -Raw -ErrorAction SilentlyContinue
            if ($texto -match 'canary-probe -host \S+ -port (\d+) -nonce ([0-9a-fA-F]+)') {
                $puerto = [int]$Matches[1]
                $nonce = $Matches[2]
                break
            }
        }
        Start-Sleep -Milliseconds 100
    }

    if ($puerto -eq 0) {
        throw "el canario no llego a abrir en 8 s. Su salida esta en $log"
    }

    Write-Host "   canario escuchando en ${Addr}:$puerto" -ForegroundColor DarkGray
    return [pscustomobject]@{
        Proceso = $p
        Log     = $log
        Puerto  = $puerto
        Nonce   = $nonce
    }
}

# Lee el veredicto PROPIO del canario, que es el unico hecho infalsificable de
# toda esta medicion: lo vio el socket del host y no un mensaje de nadie.
#
# # Como se leen los dos casos, y no son simetricos
#
# El SI es una afirmacion directa: el canario imprime "LO TOCARON" y corta solo.
#
# El NO es la AUSENCIA de esa linea. Al canario sin tocar hay que pararlo, y se
# le para a la fuerza, asi que no llega a imprimir su linea de cierre. Leer una
# ausencia solo vale porque Start-Canario ya demostro que el socket abrio: sin
# esa prueba previa, "no dijo nada" y "nunca llego a existir" se verian iguales,
# que es exactamente la ambiguedad que este script existe para quitar.
#
# La linea se busca sin acentos a proposito. "LO TOCARON" es ASCII puro y
# sobrevive a cualquier consola; el resto del texto del canario lleva tildes que
# se ven llegar mal segun la pagina de codigos.
function Read-Touched {
    param($Canario, [int]$EsperarSeg)

    # Se le da margen a que escriba su linea final. Cuando lo tocan corta solo y
    # es inmediato; cuando no, hay que esperar a que alguien lo pare.
    $limite = (Get-Date).AddSeconds($EsperarSeg)
    while ((Get-Date) -lt $limite -and -not $Canario.Proceso.HasExited) {
        Start-Sleep -Milliseconds 100
    }
    if (-not $Canario.Proceso.HasExited) {
        Stop-Process -Id $Canario.Proceso.Id -Force -ErrorAction SilentlyContinue
        $Canario.Proceso.WaitForExit(3000) | Out-Null
    }

    $texto = Get-Content $Canario.Log -Raw -ErrorAction SilentlyContinue
    if ($texto) { Write-Host $texto.TrimEnd() -ForegroundColor DarkGray }
    return [bool]($texto -match 'LO TOCARON')
}

function Stop-Canario {
    if ($null -eq $script:canario) { return }
    if (-not $script:canario.Proceso.HasExited) {
        Stop-Process -Id $script:canario.Proceso.Id -Force -ErrorAction SilentlyContinue
    }
    $script:canario = $null
}

function Set-Compuerta {
    param([switch]$Puesta)

    if ($Puesta) {
        Invoke-Mostrando $fwprobe @(
            'apply', '-data', $DataDir, '-adapter', $Adapter, '-room', $RoomCIDR,
            '-peer', $PeerIP, '-open', "$OpenPort", '-yes') | Out-Null
        $script:compuertaPuesta = $true
        return
    }
    Invoke-Mostrando $fwprobe @('purge', '-data', $DataDir) | Out-Null
    $script:compuertaPuesta = $false
}

# Una fase entera: cuatro pasos separados, y el de la compuerta es uno de ellos.
function Invoke-Fase {
    param(
        [int]$Numero,
        [string]$Titulo,
        [bool]$ConCompuerta,
        [bool]$SeEsperaQueLlegue
    )

    Write-Paso "FASE $Numero  $Titulo"
    $reloj = [System.Diagnostics.Stopwatch]::StartNew()

    # Paso 1: la compuerta, sola y en su propio paso.
    if ($ConCompuerta) { Set-Compuerta -Puesta } else { Set-Compuerta }

    # Paso 2: el canario local.
    $script:canario = Start-Canario -Addr $LocalIP

    # Paso 3: la otra maquina marca, por los DOS protocolos.
    # PermitirFallo: un ssh caido no puede abortar la corrida entera. La fase se
    # queda sin medicion, y eso lo cazan las aserciones de abajo con un mensaje
    # que dice cual fue, en vez de morir a mitad con la compuerta puesta.
    $sonda = Invoke-Mostrando 'ssh' @(
        $Remote, $RemoteFwprobe, 'canary-probe',
        '-host', $LocalIP, '-port', "$($script:canario.Puerto)",
        '-nonce', $script:canario.Nonce) -PermitirFallo

    # Se busca el prefijo sin acento a proposito. La sonda imprime "CONTESTO"
    # con tilde y la otra maquina es Linux en UTF-8, asi que la tilde puede
    # llegar mal segun la consola. "CONTEST" no colisiona con "SILENCIO", que es
    # la unica otra salida posible, y no depende de como se decodifique.
    $remotoLlego = $sonda -match 'CONTEST'

    # Paso 4: lo que vio el propio canario.
    $tocado = Read-Touched -Canario $script:canario -EsperarSeg 3
    Stop-Canario

    Assert-Igual "fase ${Numero}: el remoto alcanza el canario" $SeEsperaQueLlegue ([bool]$remotoLlego) `
        $(if ($ConCompuerta) {
            'Con la compuerta puesta un paquete cruzo igual. Es exactamente la fuga que el producto existe para impedir.'
        } else {
            'Sin compuerta el canario tiene que contestar. Si no, no llego a abrir o la sonda no marco, y entonces la fase 1 no probaria nada.'
        })

    # Las dos fuentes tienen que decir lo mismo. Un desacuerdo es
    # CanaryMismatch: el host y el miembro no ven lo mismo, y con la compuerta
    # puesta eso puede ser alguien mintiendo.
    Assert-Igual "fase ${Numero}: Touched coincide con el informe remoto" ([bool]$remotoLlego) $tocado `
        'El socket del host y el informe del miembro no coinciden. Es un bug del canario o de la sonda, o alguien informando de mas.'

    # Y por separado, el hecho propio contra lo que se esperaba.
    Assert-Igual "fase ${Numero}: Touched" $SeEsperaQueLlegue $tocado `
        'Es el unico hecho infalsificable de la medicion: lo vio el socket del host.'

    # El presupuesto de la fase se comprueba, no se declara y ya. Una fase que
    # se va de tiempo suele ser el canario tardando en abrir o la sonda
    # esperando un plazo entero, y las dos cosas cambian lo que la medicion
    # significa: un silencio que llego porque nadie marco a tiempo se lee igual
    # que un silencio que puso la compuerta.
    $seg = [int]$reloj.Elapsed.TotalSeconds
    if ($seg -gt $PhaseTimeoutSec) {
        Write-Host "   FALLA fase ${Numero}: tardo $seg s, presupuesto $PhaseTimeoutSec s" -ForegroundColor Red
        $script:fallos += "fase ${Numero}: tardo $seg s, por encima del presupuesto de $PhaseTimeoutSec s. Un silencio por plazo vencido se lee igual que uno que puso la compuerta."
        return
    }
    Write-Host "   OK   fase ${Numero}: $seg s de $PhaseTimeoutSec" -ForegroundColor Green
}

$total = [System.Diagnostics.Stopwatch]::StartNew()
try {
    Write-Paso 'Compilando fwprobe'
    Push-Location $raiz
    try {
        Invoke-Mostrando 'go' @('build', '-o', $fwprobe, './internal/fwprobe') | Out-Null
    } finally {
        Pop-Location
    }

    Invoke-Fase -Numero 0 -Titulo 'compuerta purgada: el canario tiene que contestar' `
        -ConCompuerta $false -SeEsperaQueLlegue $true

    Invoke-Fase -Numero 1 -Titulo 'COMPUERTA PUESTA: silencio por TCP y por UDP' `
        -ConCompuerta $true -SeEsperaQueLlegue $false

    Invoke-Fase -Numero 2 -Titulo 'purgada otra vez: vuelve a contestar' `
        -ConCompuerta $false -SeEsperaQueLlegue $true

    if ($total.Elapsed.TotalSeconds -gt $TotalTimeoutSec) {
        $script:fallos += "la corrida entera tardo $([int]$total.Elapsed.TotalSeconds) s, por encima del presupuesto de $TotalTimeoutSec s"
    }
}
finally {
    # Corre en TODO camino, incluido Ctrl+C. La compuerta no puede sobrevivir a
    # una corrida fallida, y un canario abierto en un proceso vecino a SYSTEM
    # tampoco.
    Write-Paso 'Limpieza'
    Stop-Canario
    if ($script:compuertaPuesta) {
        Write-Host '   la compuerta quedo puesta: purgando' -ForegroundColor Yellow
        # Sin `throw` y con la preferencia bajada: esto corre DENTRO del finally,
        # asi que una excepcion aca taparia el error que trajo hasta aca, y de
        # paso dejaria la compuerta puesta por reportar mal el intento de
        # quitarla.
        Invoke-Mostrando $fwprobe @('purge', '-data', $DataDir) -PermitirFallo | Out-Null
    } else {
        Write-Host '   la compuerta ya estaba purgada' -ForegroundColor DarkGray
    }
    Write-Host '   si algo quedo raro, reiniciar tambien lo arregla: los filtros de la compuerta no son persistentes' -ForegroundColor DarkGray
}

Write-Host ''
if ($script:fallos.Count -eq 0) {
    Write-Host "TODO VERDE en $([int]$total.Elapsed.TotalSeconds) s. La compuerta contiene, y se demostro por la transicion." -ForegroundColor Green
    exit 0
}

Write-Host 'FALLOS:' -ForegroundColor Red
foreach ($f in $script:fallos) { Write-Host "  - $f" -ForegroundColor Red }
exit 1
