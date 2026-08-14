<#
.SYNOPSIS
    El registro del seed, medido contra kanpachi.accentio.dev de verdad.

.DESCRIPTION
    El test de contrato ya habla el protocolo entero, con las dos puntas de
    verdad, en proceso y sin red. Lo que queda sin medir es justo lo que ese
    test no puede tocar: el servidor del droplet, con su TLS, su proxy inverso
    y su limite de tasa, y el daemon hablandole desde Windows como SYSTEM.

    Lo que se comprueba, y por que cada cosa:

      1. Crear una sala DEJA TARJETA. Hasta ahora toda sala se abria con un
         aviso permanente en el log y la pagina mostraba la generica.

      2. Renombrar CAMBIA los bytes de la tarjeta. Es la unica forma de ver
         desde fuera que el segundo camino de publicacion funciona.

      3. Renovar el codigo emite un ID NUEVO que resuelve. El viejo deja de
         resolver, que es la mitad que le da sentido a renovar.

      4. Muerte sucia y reabrir DEVUELVE la tarjeta. Es el agujero que se
         cerro: el registro guarda las tarjetas en memoria y con vencimiento,
         asi que una sala que sobrevive a un apagon se encontraba a si misma
         en pie y su tarjeta muerta.

      5. Una llave AJENA no puede pisar el invite ID. Lo hace `dirprobe`,
         porque PowerShell 5.1 no sabe firmar Ed25519.

    Hay pausas a proposito: el registro limita a 30 peticiones por minuto y por
    IP, y cuenta tambien las que fallan.

    Necesita consola elevada.
#>
[CmdletBinding()]
param(
    [string]$Stage = "C:\kt\stage",
    [string]$Data = "$env:ProgramData\Kanpachi",
    [string]$Seed = "kanpachi.accentio.dev",
    [int]$Pausa = 3
)

$ErrorActionPreference = 'Stop'

function Paso($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  OK  $t" -ForegroundColor Green }
function Mal($t) { Write-Host "  MAL $t" -ForegroundColor Red }
function Nota($t) { Write-Host "  --  $t" -ForegroundColor DarkGray }

$esAdmin = ([Security.Principal.WindowsPrincipal] `
    [Security.Principal.WindowsIdentity]::GetCurrent()
).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $esAdmin) { throw "Hace falta una consola elevada." }

$fallos = 0
$out = Join-Path $env:TEMP 'kanpachi-directorio.out'
$daemon = $null

# Ctl llama a pipeprobe armando la linea de comandos A MANO.
#
# # Por que no se invoca con el operador de llamada y una lista
#
# Porque PowerShell 5.1 PARTE el argumento en los espacios cuando ya contiene
# comillas escapadas, y lo hace en silencio. Medido con un volcador de argv:
#
#   sin espacios:  [1] "{\"nickname\":\"Alvaro\",\"name\":\"Prueba\"}"      entero
#   con espacios:  [1] "{\"nickname\":\"Alvaro\",\"name\":\"Los"            cortado
#                  [2] "panas\"}"
#
# El sintoma no se parece a la causa: pipeprobe recibe un JSON truncado y contesta
# "unexpected end of JSON input", tres pasos antes de donde el script se cae.
# Ninguna combinacion de comillas, escapes o backticks lo arregla; lo unico que
# funciona es construir la linea entera y que nadie la vuelva a interpretar.
#
# El script de medir el motor no lo sufre por casualidad: su sala se llama
# "Prueba", sin espacios.
function Ctl($metodo, $params) {
    $linea = "-data `"$Data`""
    if ($params) {
        $linea += ' -params "' + $params.Replace('"', '\"') + '"'
    }
    $linea += " $metodo"

    $psi = New-Object Diagnostics.ProcessStartInfo
    $psi.FileName = Join-Path $Stage 'pipeprobe.exe'
    $psi.Arguments = $linea
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $p = [Diagnostics.Process]::Start($psi)
    $salida = $p.StandardOutput.ReadToEnd() + $p.StandardError.ReadToEnd()
    $p.WaitForExit()
    $salida
}

# RespuestaDe saca la linea de la respuesta del METODO pedido.
#
# pipeprobe SALUDA antes de cada llamada, y ese saludo siempre trae "result". Asi
# que buscar "result" en la salida entera da verde sobre cualquier error, que es
# exactamente lo que paso la primera vez que se corrio esto: create_room fallo,
# el script lo dio por bueno, y el sintoma aparecio tres pasos despues como
# "la sala no reporto codigo".
function RespuestaDe($salida, $metodo) {
    foreach ($l in ($salida -split "`r?`n")) {
        if ($l.StartsWith("$metodo ")) { return $l.Trim() }
    }
    return ""
}

# Fallo devuelve el texto del error, o vacio si la llamada salio bien.
function Fallo($salida, $metodo) {
    $l = RespuestaDe $salida $metodo
    if (-not $l) {
        # Sin linea del metodo, lo util es la salida ENTERA: ahi esta el saludo
        # que fallo, el timeout del cliente, o lo que sea que paso antes.
        return "$metodo no contesto. Salida entera: " + ($salida -replace "`r?`n", " | ")
    }
    if ($l -match '"error"') { return $l }
    return ""
}

function Arranca() {
    $p = Start-Process -FilePath (Join-Path $Stage 'kanpachid.exe') `
        -ArgumentList '--console', '--data', $Data -PassThru -WindowStyle Minimized `
        -RedirectStandardOutput $out -RedirectStandardError "$out.err"
    Start-Sleep -Seconds 3
    if ($p.HasExited) { throw "el daemon murio al arrancar" }
    $p
}

# Resuelve devuelve la vista que el registro publica de un invite ID, o $null.
# Es la MISMA consulta que hace la pagina de invitacion.
function Resuelve($id) {
    Start-Sleep -Seconds $Pausa
    $crudo = $id.Replace('-', '')
    try {
        $r = Invoke-WebRequest -Uri "https://$Seed/api/i/$crudo" -UseBasicParsing -TimeoutSec 20
        return ($r.Content | ConvertFrom-Json)
    }
    catch {
        return $null
    }
}

function CodigoDeStatus() {
    $st = Ctl 'status' $null
    if ($st -match '"code"\s*:\s*"([A-Z0-9-]+)"') { return $Matches[1] }
    return ""
}

try {
    Paso "arrancando el daemon"
    $daemon = Arranca

    Paso "1. crear una sala deja tarjeta en el registro"
    $params = @{ nickname = 'Alvaro'; name = 'Los panas' } | ConvertTo-Json -Compress
    $r = Ctl 'create_room' $params
    $e = Fallo $r 'create_room'
    if ($e) { throw "no se pudo crear la sala: $e" }
    $codigo = CodigoDeStatus
    if (-not $codigo) { throw "la sala no reporto codigo" }
    Nota "codigo $codigo"

    $vista = Resuelve $codigo
    if (-not $vista) {
        Mal "el registro no conoce la sala recien creada"
        $fallos++
    }
    elseif (-not $vista.card) {
        Mal "el registro conoce la sala y no tiene tarjeta"
        $fallos++
    }
    else {
        Bien "tarjeta publicada, $($vista.card.Length) caracteres base64"
        Nota "llave del host $($vista.host_key.Substring(0,12))..."
        # members ausente es lo esperado si el contador del droplet no habla con
        # el motor. Se anota, no se juzga: ausente dice la verdad.
        if ($null -eq $vista.members) { Nota "members ausente, el contador no lo sabe" }
        else { Nota "members = $($vista.members)" }
    }
    $tarjetaCreada = $vista.card

    # La comprobacion que ningun script puede hacer: la clave viaja en el
    # FRAGMENTO del enlace, que el navegador no manda al servidor, asi que
    # descifrar exige el enlace completo.
    $enlace = Ctl 'invite_link' $null
    if ($enlace -match '(https://[^\s"]+)') {
        Nota "abrir a mano para ver el nombre: $($Matches[1])"
    }

    Paso "2. renombrar cambia la tarjeta"
    $params = @{ name = 'Los panas 2' } | ConvertTo-Json -Compress
    $r = Ctl 'rename_room' $params
    $e = Fallo $r 'rename_room'
    if ($e) { Mal "renombrar fallo: $e"; $fallos++ }

    $vista = Resuelve $codigo
    if (-not $vista) {
        Mal "tras renombrar el registro dejo de conocer la sala"
        $fallos++
    }
    elseif ($vista.card -eq $tarjetaCreada) {
        Mal "la tarjeta no cambio al renombrar"
        $fallos++
    }
    else { Bien "la tarjeta cambio al renombrar" }
    $tarjetaRenombrada = $vista.card

    Paso "3. renovar el codigo emite uno nuevo que resuelve"
    $r = Ctl 'rotate_invite_code' $null
    $e = Fallo $r 'rotate_invite_code'
    if ($e) { Mal "renovar fallo: $e"; $fallos++ }
    $nuevo = CodigoDeStatus
    if (-not $nuevo -or $nuevo -eq $codigo) {
        Mal "el codigo no cambio: antes $codigo, ahora $nuevo"
        $fallos++
    }
    else {
        Bien "codigo nuevo $nuevo"
        $vista = Resuelve $nuevo
        if ($vista -and $vista.card) { Bien "el codigo nuevo resuelve con tarjeta" }
        else { Mal "el codigo nuevo no resuelve"; $fallos++ }
    }

    Paso "4. muerte sucia y reabrir devuelve la tarjeta"
    $vivo = CodigoDeStatus
    Stop-Process -Id $daemon.Id -Force
    Start-Sleep -Seconds 4
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
    Start-Sleep -Seconds 2

    if (-not (Test-Path (Join-Path $Data 'hosted-room.json'))) {
        Mal "la muerte sucia no dejo hosted-room.json, asi que no hay nada que reabrir"
        $fallos++
    }
    else { Bien "quedo hosted-room.json, que es la senal de mal cierre" }

    $daemon = Arranca
    $r = Ctl 'resume_room' $null
    $e = Fallo $r 'resume_room'
    if ($e) {
        Mal "no se pudo reabrir la sala: $e"
        $fallos++
    }
    else {
        Bien "sala reabierta con el codigo $vivo"
        $vista = Resuelve $vivo
        if ($vista -and $vista.card) {
            Bien "la tarjeta sigue publicada tras reabrir"
            if ($vista.card -ne $tarjetaRenombrada -and $tarjetaRenombrada) {
                Nota "los bytes cambiaron respecto a los de antes de renovar, y esta bien:"
                Nota "renovar sello una tarjeta nueva para el codigo nuevo"
            }
        }
        else { Mal "tras reabrir la sala quedo sin tarjeta"; $fallos++ }
    }

    Paso "5. una llave ajena no puede pisar el invite ID"
    $probe = Join-Path $Stage 'dirprobe.exe'
    if (-not (Test-Path $probe)) {
        Mal "falta dirprobe.exe en el stage: go build -o $probe ./internal/dirprobe"
        $fallos++
    }
    else {
        Start-Sleep -Seconds $Pausa
        $antes = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        & $probe -seed $Seed -code $vivo 2>&1 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
        $codigoSalida = $LASTEXITCODE
        $ErrorActionPreference = $antes
        if ($codigoSalida -eq 0) { Bien "el registro rechazo a la llave ajena" }
        else { Mal "una llave ajena llego a escribir"; $fallos++ }
    }

    Paso "6. y la sala sigue siendo del dueno"
    $vista = Resuelve $vivo
    if ($vista -and $vista.card) { Bien "la tarjeta del host sigue en pie" }
    else { Mal "la tarjeta del host desaparecio"; $fallos++ }
}
catch {
    Mal "el script se rompio: $_"
    Write-Host $_.ScriptStackTrace -ForegroundColor DarkGray
    $fallos++
}
finally {
    if ($daemon -and -not $daemon.HasExited) {
        Ctl 'leave_room' $null | Out-Null
        Start-Sleep -Seconds 2
        Stop-Process -Id $daemon.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 3
    Get-Process kanpachi-engine -ErrorAction SilentlyContinue | Stop-Process -Force
}

Paso "la llave de esta instalacion"
$llave = Join-Path $Data 'identity.key'
if (Test-Path $llave) {
    $fi = Get-Item $llave
    Bien "identity.key existe, $($fi.Length) bytes"
    if ($fi.Length -ne 32) { Mal "y tiene que medir 32"; $fallos++ }
    # Los permisos: solo SYSTEM y Administradores, sin herencia.
    $acl = Get-Acl $llave
    Nota "permisos: $(($acl.Access | ForEach-Object { $_.IdentityReference.Value }) -join ', ')"
    if ($acl.AreAccessRulesProtected) { Bien "no hereda los permisos del directorio" }
    else { Mal "hereda del directorio, o sea que todos los usuarios la pueden leer"; $fallos++ }

    # Se compara por SID y NUNCA por nombre. Este Windows esta en portugues y
    # SYSTEM se llama "AUTORIDADE NT\SISTEMA", asi que buscar la palabra SYSTEM
    # daba un rojo sobre una ACL correcta. Los SID no se traducen:
    #   S-1-5-18      LocalSystem
    #   S-1-5-32-544  BUILTIN\Administradores
    $permitidos = @('S-1-5-18', 'S-1-5-32-544')
    $ajenos = @()
    foreach ($regla in $acl.Access) {
        try {
            $sid = $regla.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value
        }
        catch {
            $sid = $regla.IdentityReference.Value
        }
        if ($permitidos -notcontains $sid) { $ajenos += "$($regla.IdentityReference.Value) ($sid)" }
    }
    if ($ajenos.Count -eq 0) { Bien "solo SYSTEM y Administradores, comprobado por SID" }
    else { Mal "hay permisos de mas: $($ajenos -join ', ')"; $fallos++ }
}
else { Mal "no se creo identity.key"; $fallos++ }

Paso "el log del daemon"
$log = Get-Content $out -Encoding UTF8 -ErrorAction SilentlyContinue
$avisos = @($log | Where-Object { $_ -match '^(aviso|error) ' })
if ($avisos.Count -gt 0) {
    Write-Host "  --  $($avisos.Count) aviso(s):" -ForegroundColor Yellow
    $avisos | Select-Object -Last 12 | ForEach-Object { Write-Host "      $_" -ForegroundColor DarkGray }
}
else { Bien "ni un aviso en toda la corrida" }
if ($log -match 'la sala va sin tarjeta') {
    Mal "el log dice que la sala fue sin tarjeta, que es justo lo que esto cierra"
    $fallos++
}

Paso "resultado"
if ($fallos -eq 0) {
    Write-Host "  El registro publica, actualiza, reabre y se niega a quien no es el dueno." -ForegroundColor Green
    exit 0
}
Write-Host "  $fallos comprobacion(es) fallaron." -ForegroundColor Red
exit 1
