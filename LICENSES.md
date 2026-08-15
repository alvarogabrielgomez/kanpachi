# Licencias de Kanpachi

Qué licencia cubre cada parte de este repositorio, y dónde está cada texto. Lo que **no** es nuestro y viaja dentro del producto está en [`THIRD-PARTY-NOTICES.md`](THIRD-PARTY-NOTICES.md).

## Lo de este repositorio

Copyright © 2026 Accentio Studios.

| Parte | Licencia | Texto |
|---|---|---|
| Todo el código y la documentación: `core/`, `daemon/`, `registry/`, `ui/`, `invite/`, `installer/`, `packaging/`, `scripts/`, `docs/` | **AGPL-3.0-or-later** | [`LICENSE`](LICENSE) |
| El catálogo de juegos, `daemon/adapter/catalog/jsonfile/builtin.json` y los perfiles que se compartan por `.json` | **CC0-1.0** | <https://creativecommons.org/publicdomain/zero/1.0/legalcode> |

### Por qué AGPL y no GPL

Porque una parte de Kanpachi **es un servicio de red**: el seed, que cualquiera puede levantar y publicar en internet. El §13 de la AGPL es exactamente ese caso — quien corra un seed modificado para otras personas tiene que ofrecerles su fuente.

Eso no es una preferencia de estilo. [`kanpachi-seed.md`](kanpachi-seed.md) describe en prosa lo que un seed hostil podría hacer distinto, y la licencia es lo que convierte esa advertencia en una obligación: un seed modificado que espía sigue siendo posible, y ahora además está incumpliendo su licencia. Con GPL-3.0 no debería nada a nadie.

### Por qué el catálogo va aparte, y en dominio público

Un perfil de juego es un dato, no un programa: dice qué puertos necesita el juego y nada más. Es la única parte del proyecto donde alguien puede aportar sin tocar código privilegiado, y ponerle una licencia con obligaciones a una lista de puertos sería un peaje sin nada que proteger.

## Lo que vive en otros repositorios

Ninguno de estos se enlaza con el código de acá — el daemon lanza el motor como **subproceso** y el registro habla con `easytier-core` por RPC — así que su licencia no alcanza a lo de arriba.

| Repositorio | Qué es | Licencia |
|---|---|---|
| [kanpachi-engine](https://github.com/alvarogabrielgomez/kanpachi-engine) | El motor de red, que consume EasyTier como librería | LGPL-3.0-or-later |
| [EasyTier fork](https://github.com/alvarogabrielgomez/EasyTier) | Upstream con dos llamadas al firewall borradas | LGPL-3.0-or-later |

El binario del motor **sí se reparte** con el instalador de Windows y con el `.deb`, y eso trae obligaciones. Están en [`THIRD-PARTY-NOTICES.md`](THIRD-PARTY-NOTICES.md).

## Los textos completos

Los tres se descargaron de gnu.org, no se transcribieron:

| Fichero | De dónde |
|---|---|
| [`LICENSE`](LICENSE) | <https://www.gnu.org/licenses/agpl-3.0.txt> |
| [`licenses/LGPL-3.0.txt`](licenses/LGPL-3.0.txt) | <https://www.gnu.org/licenses/lgpl-3.0.txt> |
| [`licenses/GPL-3.0.txt`](licenses/GPL-3.0.txt) | <https://www.gnu.org/licenses/gpl-3.0.txt> |

La LGPL-3.0 son 165 líneas porque está redactada como un conjunto de permisos adicionales **sobre** la GPLv3, así que no se sostiene sola y las dos tienen que viajar juntas.

## Si haces un fork

La AGPL te deja hacerlo. Lo que además hay que tocar es lo que dice **quién publicó ese binario**, y vive en un fichero por lenguaje: `internal/brand/brand.go` y `ui/lib/core/brand.dart`. La sección «Forking: Where The Branding Lives» del [README](README.md) lo explica entero, incluido lo que **no** se puede cambiar sin partir la compatibilidad entre salas.
