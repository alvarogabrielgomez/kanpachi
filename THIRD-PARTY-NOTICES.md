# Componentes de terceros

Kanpachi se instala junto con software que no escribimos nosotros. Este documento dice cuál es, bajo qué licencia viaja, y dónde está su fuente. La licencia de Kanpachi está en [`LICENSES.md`](LICENSES.md).

Repartir un binario ajeno es *convey* según la sección 0 de la GPLv3, y ocurre igual entre amigos que en una descarga pública. Lo que sigue es lo que eso obliga.

## Qué se reparte, exactamente

| Componente | Dónde va | Licencia |
|---|---|---|
| `kanpachi-engine.exe` / `kanpachi-engine` | Instalador de Windows y `.deb` | **LGPL-3.0-or-later** |
| `wintun.dll` | Instalador de Windows | pendiente de revisión, ver abajo |
| `Packet.dll` | Instalador de Windows | pendiente de revisión, ver abajo |
| `WinDivert64.sys` | Instalador de Windows | pendiente de revisión, ver abajo |

**Lo que NO se reparte, aunque suele suponerse que sí:** `easytier-core` y `easytier-cli`. El cliente no los usa — corre su propio motor — y el seed **los descarga del release oficial de EasyTier** durante su instalación, así que quien los reparte ahí es EasyTier y no nosotros. Ver `registry/setup/easytier.go`.

## El motor, y por qué es una obligación real

`kanpachi-engine` vive en [su propio repositorio](https://github.com/alvarogabrielgomez/kanpachi-engine) bajo **LGPL-3.0-or-later**, y consume [EasyTier](https://github.com/EasyTier/EasyTier) como librería, contra un fork propio. EasyTier es LGPL-3.0 desde el 2025-06-07.

Al ser Rust compilado, el código de EasyTier viaja **dentro** del binario del motor. O sea que repartir el instalador reparte código LGPL, y eso obliga a las tres cosas de esta sección.

### 1. El aviso

Este documento es el aviso. Va además en la pantalla de información de la aplicación, en el instalador antes de instalar, y en el cuerpo de cada publicación.

### 2. Las dos licencias, completas

- [`licenses/LGPL-3.0.txt`](licenses/LGPL-3.0.txt)
- [`licenses/GPL-3.0.txt`](licenses/GPL-3.0.txt)

**Las dos, y no solo la primera.** La LGPL-3.0 está redactada como un conjunto de permisos adicionales sobre la GPLv3 y no se sostiene sola: sin la GPLv3 al lado, la mitad de sus cláusulas apuntan a un texto que no está. El repositorio de EasyTier no incluye la GPLv3, así que las dos copias se descargaron de gnu.org.

### 3. El fuente correspondiente

Se cumple por la sección 6(d) de la GPLv3, que permite señalar dónde está en vez de adjuntarlo:

| Qué | Dónde |
|---|---|
| El motor | <https://github.com/alvarogabrielgomez/kanpachi-engine> |
| El fork de EasyTier, con su `FORK.md` listando cada cambio | <https://github.com/alvarogabrielgomez/EasyTier> |
| EasyTier original, en el tag fijado | <https://github.com/EasyTier/EasyTier/releases/tag/v2.6.4> |

A qué referencia del fork apunta el motor se dice en un solo sitio, la decisión 1 de [`docs/02-decisiones-de-diseno.md`](docs/02-decisiones-de-diseno.md), y hay un test que falla si esa referencia aparece copiada en otro lado.

La LGPL además exige poder **recombinar** el motor con una versión modificada de la librería. Con las dos fuentes públicas y la versión fijada por escrito, cualquiera reconstruye el binario: `scripts/build-linux.sh` en el repositorio del motor es la receta exacta que usa la publicación.

## Las tres bibliotecas de Windows, y lo que falta

`wintun.dll`, `Packet.dll` y `WinDivert64.sys` llegan **dentro del release de EasyTier**, y no son código de EasyTier: son bibliotecas de terceros que EasyTier redistribuye. Cada una tiene términos propios.

**Su licencia todavía no se revisó, y decirlo es más honesto que suponerla.** Lo que hay que contestar, una por una, antes del lanzamiento público:

| Fichero | Origen | Qué hay que contestar |
|---|---|---|
| `wintun.dll` | Wintun, del proyecto WireGuard | Los binarios precompilados llevan términos propios, distintos de los del fuente. ¿Permiten redistribuir dentro de un instalador de terceros? |
| `WinDivert64.sys` | WinDivert | Se distribuye con licencia dual. ¿Cuál de las dos aplica al `.sys` precompilado, y qué obliga? |
| `Packet.dll` | Linaje WinPcap / Npcap | **El más restrictivo de los tres.** Las versiones modernas de Npcap prohíben la redistribución sin licencia comercial. Hay que determinar de qué versión viene el fichero que reparte EasyTier y qué términos lleva |

El resultado de esa revisión puede obligar a sacar alguno del paquete. Es trabajo pendiente e independiente de qué motor se termine usando: los tres seguirían haciendo falta con otro motor que también cree un adaptador virtual en Windows.

Las sumas SHA-256 de los siete ficheros del release de EasyTier, incluidos estos tres, están fijadas en `internal/arch/easytier.sums` y comprobadas por un test, así que lo que se reparte es exactamente lo que se probó.

## El resto de dependencias

Las bibliotecas de Go y de Dart se resuelven al compilar y no se reparten como ficheros aparte; sus licencias son las que declaran `go.mod` y `pubspec.yaml`, todas permisivas. No entran en esta lista porque no hay nada que adjuntar ni a qué apuntar más allá de sus propios repositorios.
