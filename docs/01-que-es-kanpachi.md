# Kanpachi

**Pegas un código, eliges el juego, juegas.**

Kanpachi es una LAN virtual privada para jugar entre amigos. Crea una red cifrada de igual a igual entre las máquinas de una sala, abre únicamente los puertos del juego elegido y mantiene cerrado todo lo demás. Sin cuentas, sin configuración, sin abrir puertos en el router, sin exponer nada a internet.

Es el sucesor espiritual de Hamachi con criterios de 2026: cifrado WireGuard, conexiones directas P2P, aislamiento por defecto y un instalador que deja todo listo con un solo UAC.

## El nombre

El kanpachi es un pez de la misma familia que el hamachi, más grande. La referencia se sostiene sola.

## Por qué existe

La historia real: el grupo quería jugar Project Zomboid y todas las opciones sobre la mesa eran malas. Meterse todos en la tailnet de una persona expone cada máquina a las demás en todos los puertos, con las ACLs controladas por el dueño de esa red. Abrir un puerto en el router de cada casa asusta, con razón, a quien no es técnico. Los servicios tipo Hamachi crean una LAN plana sin ningún control, con límites de usuarios y clientes cerrados.

Kanpachi toma la comodidad de Hamachi y le corrige el modelo de seguridad: **la red existe, la exposición no**. Cada miembro solo alcanza los puertos del juego activo en la máquina del host, nada más.

## Qué NO es

- No es una VPN de privacidad. No enruta tu tráfico de internet, no oculta tu IP, no toca nada fuera de la sala.
- No es acceso remoto. No hay escritorio remoto, ni transferencia de archivos, ni consola.
- No es un launcher. No parchea juegos, no interfiere con Steam.

## Lo que Kanpachi no sabe

Esta sección existe porque es fácil suponer lo contrario. Kanpachi vive **completamente fuera del juego**:

- **No detecta que abriste un juego.** No vigila procesos, no sabe qué estás ejecutando ahora ni en ningún momento. Abrir Zomboid no dispara absolutamente nada.
- **No sabe nada de lo que pasa dentro.** No engancha procesos, no inyecta código, no lee memoria, no sabe si hay partida en curso, quién juega ni cuánto.
- **No abre puertos solo.** Los puertos se abren porque el usuario eligió un juego en la lista, y se cierran porque salió de la sala. Siempre explícito, siempre visible en pantalla.
- **No manda telemetría.** Cero datos salientes en el modo privado.

Lo único que Kanpachi mira del sistema, y solo cuando corresponde:

| Qué mira | Cuándo | Cómo |
|---|---|---|
| Juegos instalados | Al abrir la app | Archivos en disco de Steam. Ningún juego se ejecuta |
| Reglas de firewall existentes | Al activar un perfil | Consulta al Firewall de Windows por ruta de ejecutable. No mira procesos vivos |
| Puertos abiertos de un proceso | Solo dentro del creador de perfiles, cuando el usuario lo pide | Una foto puntual de la tabla de sockets, igual que `netstat` |

Ese último es un asistente opt-in que corre una vez en la vida de un juego nuevo y termina cuando el usuario lo cierra. Nunca queda de fondo.

## Las partes

| Componente | Qué es | Dónde corre |
|---|---|---|
| **kanpachi-core** | Librería con toda la lógica: identidad, catálogo, política de firewall, interfaz del motor de red | Dentro del daemon |
| **kanpachi-daemon** | Servicio de Windows privilegiado: adaptadores de red y firewall, API local, supervisión del motor | Windows, como servicio |
| **kanpachi-ui** | Aplicación de escritorio Flutter, sin privilegios | Windows, sesión del usuario |
| **kanpachi-seed** | Nodo de rendezvous que presenta a los peers entre sí | Droplet Linux, Docker |
| **kanpachi-invite** | Página estática de invitación: abre la app con el código por `kanpachi://`, o lleva a la descarga. El código viaja en el fragmento, el servidor nunca lo recibe | Droplet, HTML estático |
| **kanpachi-catalog** | Perfiles JSON de juegos: puertos, descubrimiento LAN, ejecutables y verificación. Viene en el instalador, se amplía con el creador de perfiles y se comparte exportando un `.json` plano | Embebido más `ProgramData` |

```
┌─────────────────────────── PC del jugador ───────────────────────────┐
│                                                                      │
│   kanpachi-ui  (Flutter, sin privilegios)                            │
│        │  named pipe + token                                         │
│   kanpachi-daemon  (servicio, elevado)                               │
│        ├── kanpachi-core  (identidad, catálogo, política)            │
│        ├── easytier-core (proceso hijo) ── adaptador Wintun (kanpachi0) │
│        └── Windows Firewall  (reglas etiquetadas "Kanpachi")         │
└──────────────────────────────│───────────────────────────────────────┘
                               │
              internet         │  P2P directo con los demás peers
                               │  (el tráfico del juego nunca pasa
                               │   por el servidor)
                               ▼
                    kanpachi-seed  (droplet NYC)
                    solo presenta a los nodos entre sí
```

## Principios

1. **Seguro por defecto.** La interfaz virtual nace con deny all en ambas direcciones. Cada apertura es explícita, por perfil de juego, solo hacia miembros presentes, y se revierte sola. El router jamás se toca: todas las conexiones se inician desde adentro, así que no hay reenvío de puertos, no hay UPnP y nada queda escuchando en tu IP pública. Kanpachi además avisa si el propio juego dejó una regla que te expone en tu red de casa.
2. **Si se puede detectar, no se pregunta.** Ruta de Steam, juegos instalados, MTU, rango de IPs: todo se resuelve solo. La configuración manual no existe como concepto. La contracara igual de importante: **lo detectado nunca limita al usuario**. La detección ordena y sugiere, jamás filtra ni bloquea, porque toda detección falla alguna vez.
3. **El código es un ticket, y el host tiene la cerradura.** El código de sala no es el secreto de la red: el host lo canjea por una credencial temporal. Eso le da tres controles reales sin servidor de por medio, expulsar a alguien, renovar el código y cerrar la sala. Nada viaja a ningún servidor, no hay cuentas ni base de datos.
4. **Nada de fuera surte efecto sin confirmación dentro.** Un código que llega por un enlace abre la app y muestra qué recibió, jamás entra solo a una sala. Siempre, sin "recordar esta elección".
5. **Privado hoy, abrible mañana.** Pasar de "solo panas" a público es un cambio de configuración y presupuesto, no un rewrite. La arquitectura ya lo contempla.

## Estado y alcance de la v1

- Cliente **solo Windows**. Es donde juega todo el grupo. Linux vive como servidor (el seed) y queda como posible cliente futuro.
- Catálogo inicial de unos 5 juegos, los que el grupo juega, cada uno probado en partida real.
- Distribución privada: el instalador se comparte por el grupo, con hash publicado en el chat.
- Sin firma de código, sin autoupdate, sin macOS, sin móvil. Todo eso vive en `07-futuro.md` con su disparador definido.

## Los documentos

- `02-decisiones-de-diseno.md`: cada decisión importante, sus alternativas y por qué.
- `03-arquitectura.md`: componentes por dentro, interfaces, modelo de amenazas.
- `04-flujos-y-configuracion.md`: la experiencia del jugador, del host y la configuración del droplet.
- `05-ui.md`: las dos pantallas, estados, textos y errores.
- `06-catalogo.md`: el catálogo de juegos, el creador de perfiles y el formato de exportación.
- `07-futuro.md`: qué se difirió, qué lo activaría y qué se decidió no hacer.
