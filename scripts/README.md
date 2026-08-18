# El mapa de scripts

Una carpeta con veinte scripts necesita decir cuál se corre para qué. Este es
ese mapa; la receta exacta de cada uno vive en su propia cabecera, y el porqué
del régimen entero en `docs/CLAUDE.md`: lo que se repite se talla en un script,
el CI llama al MISMO script que se usa a mano, y el YAML es pegamento.

## Lo que quieres armar, y con qué

| Quiero… | Corro |
|---|---|
| el motor (`kanpachi-engine.exe`) | `build-engine.ps1` |
| la carpeta portable, y que arranque | `build-portable.ps1` |
| el portable de UN solo `.exe`, para mandar por chat | `build-portable-bundle.ps1` |
| la carpeta de producción (lo que va a `Program Files\`), sin instalador | `build-production.ps1` |
| el cliente de Windows entero: `kanpachi-setup.exe` + portable + sumas | `build-installer.ps1` |
| el `.deb` de Linux | `build-deb.sh` |
| el seed (`kanpseed`) y su página | `build-seed.sh` |
| correr los chequeos antes de commitear | `verify.ps1` |

## El motor, que vive en otro repositorio

`build-engine.ps1` no sabe compilar Rust: delega en `scripts\build.ps1` del
repositorio `kanpachi-engine`, que es el ÚNICO sitio que conoce esa receta
(MSVC, protoc, libclang, 7-Zip, target dir corto porque `cl.exe` no entiende
rutas largas). El binario queda en `C:\kt\release\`, y ahí lo encuentran solos
todos los `build-*`: la lista de sitios donde un motor compilado puede estar
—y el chequeo de que no sea más viejo que su propio fuente— vive UNA vez, en
`lib\engine.ps1`. Un motor que no aparece corta con el nombre de este script
en el error; `-Engine <ruta>` fuerza uno concreto.

En Linux la receta es `scripts/build-linux.sh` del mismo repositorio del motor,
y `release.yml` llama a esos dos scripts contra su propio checkout (`motor\`),
con el target dir dentro del workspace.

## Los procedimientos, desde una máquina limpia

Todos comparten los dos primeros pasos, una sola vez:

```powershell
# 1. Las DLL del motor (Packet.dll, wintun.dll, WinDivert64.sys y compañía).
#    No están en el repositorio por tamaño: esto las baja del release oficial
#    de EasyTier y las deja en third_party\easytier.
.\scripts\fetch-third-party.ps1

# 2. El motor. Pide el repo kanpachi-engine clonado al lado, con Rust, Visual
#    Studio (C++), protoc, LLVM y 7-Zip; el script del motor localiza todo y
#    dice con nombre y comando de instalación lo que falte.
.\scripts\build-engine.ps1
```

Con eso hecho, cada artefacto es UNA orden. Ninguna pide consola elevada:
compilan y copian, nada más.

**La carpeta portable** — el producto entero en una carpeta que se puede
comprimir y mandar, con el marcador `kanpachi.portable` que hace que el daemon
guarde sus datos ahí mismo y corra sin servicio:

```powershell
.\scripts\build-portable.ps1              # arma .\Kanpachi y la arranca (UAC)
.\scripts\build-portable.ps1 -NoLaunch    # solo arma, para comprimir
.\scripts\build-portable.ps1 debug        # daemon de consola a la vista
```

**El portable de un solo `.exe`** — la MISMA carpeta, empotrada con `go:embed`
en un ejecutable que la suelta en un temporal, corre y borra al salir. Es el
fichero que se manda por chat:

```powershell
.\scripts\build-portable-bundle.ps1       # deja dist\kanpachi-portable.exe
```

Por dentro llama a `build-portable.ps1 -NoLaunch -Clean` sobre un stage
temporal —una receta, no dos que derivan— y antes de empaquetar verifica por
SHA256 que el motor empotrado es el que eligió, que no es más viejo que su
fuente, y que las diez piezas esenciales están.

**La carpeta de producción sin instalador** — exactamente lo que el instalador
copia a `Program Files\Kanpachi\`, con licencias y `SHA256SUMS`:

```powershell
.\scripts\build-production.ps1            # deja dist\carga
```

La diferencia con la portable: sin marcador (los datos van a ProgramData, el
daemon corre como servicio), con licencias y sumas, y con la versión sellada
si se pasa `-Version`; a mano queda «dev», que a propósito no anuncia
versiones nuevas.

**El instalador** — los cinco pasos de una publicación en una orden. Pide Inno
Setup (`choco install innosetup`) y la versión explícita:

```powershell
.\scripts\build-installer.ps1 -Version 0.4.0
```

Sella la versión en los recursos de Windows (`.syso`), arma la carga con
`build-production.ps1`, corre Inno Setup, arma el bundle portable con
`build-portable-bundle.ps1` —del MISMO tag, para que las dos formas de
entregar el producto no queden en versiones distintas— y escribe
`SHA256SUMS-windows` con los dos ejecutables.

**El `.deb` y el seed** van en Linux o WSL: `build-deb.sh --engine <ruta>`
empaqueta el cliente con el daemon como unidad de systemd, y `build-seed.sh`
compila `kanpseed` para amd64 y arm64. `--strict` en el primero convierte el
piso de glibc en corte, que es como lo corre la publicación.

## Lo que comprueba `verify.ps1`, superficie por superficie

Una superficie por job del CI, así el runner y una persona corren el MISMO
fichero, y un paquete nuevo entra a la lista acá y en ningún otro sitio. No
corta en el primer fallo: junta la lista entera y sale rojo al final.

| Superficie | Quién la corre | Qué corre |
|---|---|---|
| `all` (por defecto) | la máquina de desarrollo | build entero, build cruzado a Linux, `go vet`, guardianes de arquitectura, tests de core, registry, daemon e internal, y gofmt |
| `ci-linux` | `ci.yml`, job «core en Linux» | build, vet, guardianes, core y registry con `-race`, la mitad portable del daemon con `-race`, gofmt |
| `ci-windows` | `ci.yml`, job «daemon en Windows» | build entero y los tests de `daemon/` e `internal/`, que son los que no compilan en Linux |
| `ci-ui` | `ci.yml`, job «la UI de Flutter» | `pub get`, `dart format`, `analyze --fatal-infos`, `flutter test` |
| `release-windows` | `release.yml`, job «instalador» | core + guardianes (lo que veta una publicación) y los chequeos de Flutter sin format |
| `release-linux` | `release.yml`, job «paquete-linux» | core + guardianes |
| `release-seed` | `release-seed.yml` | core + registry + guardianes, con `-race` |

Los **guardianes de arquitectura** (`internal/arch`) corren aparte y primero
para que su rojo se distinga: no son un test que falló, son una regla de la
casa violada — pureza de capas, los nombres cerrados que pueden borrar la
cuarentena, el manifiesto de suministro de las DLL, el lockstep del protocolo
con la UI.

Lo que `verify.ps1` NO hace: `-race` en Windows (ese mismo código ya corre con
`-race` en el job de Linux) y arrancar nada — ni salas, ni firewall, ni
consola elevada. Para eso está el banco de abajo.

## Cómo lo usa el CI, workflow por workflow

El YAML es pegamento —eventos, checkouts, toolchains, la versión que sale del
tag, la subida al release— y el trabajo lo hacen los scripts:

- **`ci.yml`**, en cada push: tres jobs, cada uno `verify.ps1 -Surface` con la
  suya. Cero recetas inline.
- **`release.yml`**, al empujar un tag `v*`: el job «instalador» baja las DLL
  con `fetch-third-party.ps1`, compila el motor con `build-engine.ps1 -Repo
  motor -TargetDir motor\target` (el checkout del repositorio del motor vive
  en `motor\`), verifica con `-Surface release-windows`, empaqueta con
  `build-installer.ps1` y cita el changelog con `release-notes.ps1`. El job
  «paquete-linux» compila el motor con el `build-linux.sh` del propio motor,
  verifica con `-Surface release-linux` y empaqueta con `build-deb.sh
  --strict`. Reproducir una publicación a mano es correr esos mismos scripts
  en ese orden.
- **`release-seed.yml`**: verifica con `-Surface release-seed` y arma con
  `build-seed.sh`.

## El banco de medición, que se corre a mano

Ninguno de estos lo llama el CI: son herramientas de medir el producto de
verdad, y su resultado es el log que dejan.

| Script | Qué mide o arma |
|---|---|
| `prepare-stage.ps1` | el banco `C:\kt\stage`: daemon y sondas sueltas, sin producto |
| `build-test-tools.ps1` | `testTools\`: roomprobe con el motor al lado; el motor lo recompila SIEMPRE, y el porqué está en su cabecera |
| `build-measure-clocks.ps1` | binarios con los relojes de compilación acortados, para medir esperas largas |
| `measure-return.ps1` | la vuelta del invitado, 11 escenarios contra el droplet |
| `measure-reset.ps1` | que `--reset` deja la máquina limpia |
| `measure-network-change.ps1` | que el adaptador virtual se repone solo |
| `measure-netcfg.ps1` | rutas de netcfg que una sala normal no toca |
| `measure-engine-end-to-end.ps1` | los cuatro fallos fijos del motor, producto entero |
| `measure-directory.ps1` | el registro de seeds contra el real |
| `canary-two-machines.ps1` | la Protección Kanpachi vista desde otra máquina |
| `clean-engine-rules.ps1` | limpia reglas de firewall que dejó el EasyTier de serie |

`release-notes.ps1` extrae la sección del changelog de una versión, y es lo
que hace fallar una publicación con changelog vacío a propósito. `lib\` no se
corre: son funciones que los `build-*` cargan con dot-source.
