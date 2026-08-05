<#
.SYNOPSIS
  Quita las reglas de firewall que el motor viejo dejo puestas en esta maquina.

.DESCRIPTION
  # Que son esas reglas y por que sobran

  EasyTier de upstream escribe reglas de PERMISO en el Firewall de Windows
  mientras crea el adaptador virtual, por COM, desde dentro del arranque de red.
  Son dos clases y la segunda es la grave:

    EasyTier kanpachi0 - ALL Protocol (Inbound)   permitir TODO sobre kanpachi0
    EasyTier <ruta al exe> (Inbound)              permitir TODO hacia el motor,
                                                  en TODAS las interfaces de la
                                                  maquina, la red de casa incluida

  Sin puerto, sin direccion de origen, en los tres perfiles, y sobreviven al
  reinicio y a desinstalar el programa que las causo.

  Kanpachi abre solo los puertos del perfil del juego activo, solo hacia las
  direcciones de los miembros presentes. Un permiso de todo sobre la misma
  interfaz deshace eso en la misma capa que Kanpachi usa para conceder.

  **El motor ya no las escribe.** Consume un fork de EasyTier con esas dos
  llamadas borradas, asi que en una maquina limpia no hay nada que quitar. Este
  script existe para las maquinas que corrieron el motor VIEJO, que hoy es la de
  desarrollo y manana la de cualquiera que probo una version anterior.

  # Que borra, y que no

  Solo lo que Kanpachi causo. Se reconoce por dos vias:

    - la ruta del programa nombra un binario de Kanpachi;
    - el nombre de la regla nombra un adaptador de Kanpachi.

  Una instalacion de EasyTier del usuario, con su propio adaptador y su propio
  ejecutable, NO casa con ninguna de las dos y se queda intacta. Borrar software
  ajeno seria exactamente el tipo de cosa que este producto promete no hacer.

  Tampoco toca el grupo Kanpachi ni Kanpachi-base: el primero lo purga el daemon
  en cada arranque, y el segundo es la cuarentena, que vale justamente porque
  sigue puesta con Kanpachi apagado.

  # Va en seco por defecto

  Lista lo que quitaria y no quita nada. Hace falta -Aplicar para borrar, porque
  esto modifica el firewall de la maquina y un script que borra por defecto es
  un script que alguien corre sin leer.

.PARAMETER Aplicar
  Borra de verdad. Sin esto solo lista.
#>
[CmdletBinding()]
param(
    [switch]$Aplicar
)

$ErrorActionPreference = 'Stop'

function Paso($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Bien($t) { Write-Host "  OK  $t" -ForegroundColor Green }

$esAdmin = ([Security.Principal.WindowsPrincipal] `
    [Security.Principal.WindowsIdentity]::GetCurrent()
).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $esAdmin) { throw "Hace falta una consola elevada: escribir en el firewall es privilegiado." }

# Los binarios que Kanpachi ejecuta o ha ejecutado. Se compara la RUTA y no solo
# el nombre, por lo mismo que el adaptador mata huerfanos por ruta completa:
# esto corre elevado, y actuar sobre cualquier fichero que se llame igual seria
# actuar sobre la instalacion de otro.
$nuestros = @('kanpachi-engine.exe', 'kanpachi-engine-spike.exe')

Paso "buscando reglas que dejo el motor"

$candidatas = Get-NetFirewallRule -ErrorAction SilentlyContinue | ForEach-Object {
    $regla = $_
    $programa = ($regla | Get-NetFirewallApplicationFilter).Program

    $porPrograma = $false
    if ($programa -and $programa -ne 'Any') {
        $hoja = Split-Path -Leaf $programa
        # Un binario nuestro por nombre de fichero, o cualquier cosa que viva
        # dentro del arbol de Kanpachi, que cubre el easytier-core vendorizado
        # que se uso para probar el seed.
        $porPrograma = ($nuestros -contains $hoja) -or ($programa -like '*\kanpachi\*')
    }

    # Las reglas de interfaz no llevan programa. Se reconocen por el nombre, que
    # EasyTier compone con el nombre del adaptador. Y NO por el filtro de
    # interfaz: cuando el adaptador ya no existe, Windows devuelve ahi el GUID
    # en vez del alias, asi que filtrar por alias no casaria con nada justo en
    # el caso normal, que es el de una maquina sin sala abierta.
    $porNombre = $regla.DisplayName -like 'EasyTier kanpachi*'

    if ($porPrograma -or $porNombre) {
        [PSCustomObject]@{
            Regla    = $regla
            Nombre   = $regla.DisplayName
            Grupo    = if ($regla.Group) { $regla.Group } else { '(sin grupo)' }
            Dir      = $regla.Direction
            Accion   = $regla.Action
            Programa = if ($programa) { $programa } else { 'Any' }
        }
    }
}
$candidatas = @($candidatas)

if ($candidatas.Count -eq 0) {
    Bien "no quedo ninguna. Esta maquina esta limpia."
    exit 0
}

Write-Host "  $($candidatas.Count) regla(s):" -ForegroundColor Yellow
$candidatas | ForEach-Object {
    Write-Host ("    [{0}] {1}  ({2} {3})" -f $_.Grupo, $_.Nombre, $_.Dir, $_.Accion) -ForegroundColor Yellow
    if ($_.Programa -ne 'Any') { Write-Host "        $($_.Programa)" -ForegroundColor DarkGray }
}

if (-not $Aplicar) {
    Paso "en seco"
    Write-Host "  No se borro nada. Vuelve a correrlo con -Aplicar para quitarlas." -ForegroundColor Cyan
    exit 0
}

Paso "borrando"
$borradas = 0
foreach ($c in $candidatas) {
    try {
        Remove-NetFirewallRule -Name $c.Regla.Name -ErrorAction Stop
        $borradas++
    }
    catch {
        # Se sigue con las demas a proposito: abortar en la primera dejaria la
        # limpieza a medias y sin forma de saber cuanto se hizo.
        Write-Host "  MAL no se pudo borrar '$($c.Nombre)': $_" -ForegroundColor Red
    }
}
Bien "$borradas de $($candidatas.Count) borrada(s)"

Paso "comprobando"
$quedan = @(Get-NetFirewallRule -ErrorAction SilentlyContinue |
    Where-Object { $_.DisplayName -like 'EasyTier kanpachi*' })
if ($quedan.Count -gt 0) {
    Write-Host "  Quedan $($quedan.Count) regla(s) de interfaz sin quitar." -ForegroundColor Red
    exit 1
}
Bien "no queda ninguna regla de interfaz del motor"

# La cuarentena tiene que seguir entera. Este script no la toca, y comprobarlo
# vale porque es la unica proteccion que sigue puesta con Kanpachi apagado.
$base = @(Get-NetFirewallRule -Group 'Kanpachi-base' -ErrorAction SilentlyContinue)
Write-Host "  --  la cuarentena de base sigue con $($base.Count) reglas" -ForegroundColor DarkGray
exit 0
