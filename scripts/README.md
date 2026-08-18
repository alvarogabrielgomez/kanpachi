# El mapa de scripts

Una carpeta con veinte scripts necesita decir cuál se corre para qué. Este es
ese mapa; la receta de cada uno vive en su propia cabecera, y el porqué del
régimen entero en `docs/CLAUDE.md`: lo que se repite se talla en un script, el
CI llama al MISMO script que se usa a mano, y el YAML es pegamento.

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

El orden del día 1 en una máquina limpia: `fetch-third-party.ps1` (las DLL del
motor, que no están en el repositorio por tamaño), `build-engine.ps1` una vez,
y después el `build-*` que toque. Nada más: cada `build-*` compila el daemon y
la interfaz por su cuenta.

## El motor, que vive en otro repositorio

`build-engine.ps1` no sabe compilar Rust: delega en `scripts\build.ps1` del
repositorio `kanpachi-engine`, que es el ÚNICO sitio que conoce esa receta
(MSVC, protoc, libclang, 7-Zip, target dir corto). El binario queda en
`C:\kt\release\`, y ahí lo encuentran solos todos los `build-*`: la lista de
sitios donde un motor compilado puede estar —y el chequeo de que no sea más
viejo que su propio fuente— vive UNA vez, en `lib\engine.ps1`. Un motor que no
aparece corta con el nombre de este script en el error; `-Engine <ruta>` fuerza
uno concreto.

En Linux la receta es `scripts/build-linux.sh` del mismo repositorio del motor,
y `release.yml` llama a esos dos scripts contra su propio checkout (`motor\`),
con el target dir dentro del workspace.

## Lo que corre el CI

`ci.yml` y los tres jobs de publicación no traen recetas propias: llaman a
`verify.ps1 -Surface <la del job>` para comprobar, y a `build-installer.ps1`,
`build-deb.sh --strict`, `build-seed.sh`, `fetch-third-party.ps1` y
`release-notes.ps1` para publicar. Reproducir una publicación a mano es correr
esos mismos scripts en ese orden.

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

`lib\` no se corre: son funciones que los `build-*` cargan con dot-source.
