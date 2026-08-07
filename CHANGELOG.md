# Changelog

Lo que cambió en cada versión de Kanpachi, para quien lo usa.

El formato es [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/) y las versiones son [SemVer](https://semver.org/lang/es/). Cómo se mantiene, y por qué se mantiene así, está en [docs/CLAUDE.md](docs/CLAUDE.md): **una línea por entrada, en imperativo, con el enlace a su commit**, y escrita en el mismo commit que el cambio.

Esto cuenta lo que le pasa a la persona que juega. El porqué de cada decisión vive en `docs/02-decisiones-de-diseno.md`, y el detalle mecánico en el mensaje del commit enlazado.

## Unreleased

### Changed

- Audita las reglas ajenas del firewall en cuatro momentos y no cada dos segundos: al entrar a la sala, al cambiar de juego, al entrar alguien nuevo, y cada dos minutos ([3d31a5c](https://github.com/alvarogabrielgomez/kanpachi/commit/3d31a5c))

### Fixed

- Deja de perder el servicio a ratos: el latido ya no se solapa consigo mismo ni encola tras el barrido del firewall ([3d31a5c](https://github.com/alvarogabrielgomez/kanpachi/commit/3d31a5c))
- Lleva a la sala en vez de a la portada al cerrar un enlace de invitación con una sala abierta ([46bd095](https://github.com/alvarogabrielgomez/kanpachi/commit/46bd095))
- Pregunta al daemon si hay sala cada vez que aparece la portada, en vez de fiarse de lo último que supo ([46bd095](https://github.com/alvarogabrielgomez/kanpachi/commit/46bd095))

## [0.1.3] - 2026-08-07

### Fixed

- Acepta a los invitados en el seed: sin modo seguro, una credencial se rechazaba en el primer paquete y ninguna sala pasaba de una persona ([c64a2cb](https://github.com/alvarogabrielgomez/kanpachi/commit/c64a2cb))
- Conecta al invitado con el host de forma directa, en vez de relevarlo todo por el servidor ([c6dfadc](https://github.com/alvarogabrielgomez/kanpachi-engine/commit/c6dfadc), en `kanpachi-engine`)
- Devuelve la flecha de volver a la pantalla anterior de verdad, en vez de a la portada ([0be74b9](https://github.com/alvarogabrielgomez/kanpachi/commit/0be74b9))
- Impide quedarse en la portada con una sala abierta, que hacía parecer que no había ninguna ([0be74b9](https://github.com/alvarogabrielgomez/kanpachi/commit/0be74b9))
- Deja al usuario en su sala cuando salir de ella falla, en vez de mandarlo a una portada que no acepta nada ([0be74b9](https://github.com/alvarogabrielgomez/kanpachi/commit/0be74b9))

## [0.1.2] - 2026-08-07

### Fixed

- Separa el Kanpachi instalado del portable: cada uno con su canal, su token y su ventana ([8dec62f](https://github.com/alvarogabrielgomez/kanpachi/commit/8dec62f))
- Acepta los enlaces de invitación tal como los entrega Windows, con la barra final que añade el navegador ([6436de1](https://github.com/alvarogabrielgomez/kanpachi/commit/6436de1))
- Borra las preferencias de la interfaz al desinstalar, que Flutter guarda fuera de Program Files ([6436de1](https://github.com/alvarogabrielgomez/kanpachi/commit/6436de1))
- Termina el aviso del instalador cuando el servicio no se deja detener ([6d3e85e](https://github.com/alvarogabrielgomez/kanpachi/commit/6d3e85e), [0e69580](https://github.com/alvarogabrielgomez/kanpachi/commit/0e69580))

## [0.1.1] - 2026-08-06

### Added

- Ofrece reabrir la sala que quedó del arranque anterior, en vez de perderla ([7af511e](https://github.com/alvarogabrielgomez/kanpachi/commit/7af511e))

### Fixed

- Conserva las salas registradas al recargar la página del seed ([3c67f5b](https://github.com/alvarogabrielgomez/kanpachi/commit/3c67f5b))
- Publica el instalador también cuando el release se crea desde la web de GitHub ([981bead](https://github.com/alvarogabrielgomez/kanpachi/commit/981bead))

## [0.1.0] - 2026-08-06

Primera versión publicada.

### Added

- Crea una sala, reparte su código y abre solo los puertos del juego elegido ([c81c0bf](https://github.com/alvarogabrielgomez/kanpachi/commit/c81c0bf))
- Entra a una sala pegando el código, o abriendo un enlace `kanpachi://` desde el navegador ([7a8539e](https://github.com/alvarogabrielgomez/kanpachi/commit/7a8539e))
- Enseña lo que tu PC tiene abierto, medido en el sistema y no leído de lo que Kanpachi cree ([7a47467](https://github.com/alvarogabrielgomez/kanpachi/commit/7a47467))
- Deja cancelar la espera de una sala, deshaciendo lo que alcanzó a hacer ([8dbd9e4](https://github.com/alvarogabrielgomez/kanpachi/commit/8dbd9e4))
- Trae una carpeta portable, que se copia y se ejecuta sin instalar nada ([250b3d5](https://github.com/alvarogabrielgomez/kanpachi/commit/250b3d5))
- Recuerda tu nombre y el tamaño de la ventana ([01fb7e5](https://github.com/alvarogabrielgomez/kanpachi/commit/01fb7e5), [68a543a](https://github.com/alvarogabrielgomez/kanpachi/commit/68a543a))
- Publica el instalador firmando cada versión con un tag ([e4fd252](https://github.com/alvarogabrielgomez/kanpachi/commit/e4fd252))

[0.1.3]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.3
[0.1.2]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.2
[0.1.1]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.1
[0.1.0]: https://github.com/alvarogabrielgomez/kanpachi/releases/tag/v0.1.0
