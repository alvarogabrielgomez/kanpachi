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

## Qué agrega Kanpachi sobre usar EasyTier a secas

El motor de red es EasyTier, sin modificar, corriendo como proceso hijo. Alguien puede razonablemente preguntar por qué no instalar EasyTier y ya. Esta sección responde eso con la guía que circula por internet para "asegurar" una partida con EasyTier, punto por punto, porque cada consejo de esa guía es un sitio donde Kanpachi ya decidió por el usuario.

| El consejo que circula | Qué exige del usuario | Qué hace Kanpachi |
|---|---|---|
| "Usa un nombre de red y un secreto largos y aleatorios" | Generar dos strings, guardarlos, y pasárselos a cada amigo por un canal seguro | **No hay nombre ni secreto escribibles.** La identidad de la red real son 16 + 32 bytes aleatorios que genera el host, y no derivan de ningún string que alguien pueda teclear. El fallo típico, `cs16` con secreto `1234`, es estructuralmente imposible |
| "No repartas el secreto por el chat" | Disciplina, cada vez | **El secreto no viaja.** Lo que se reparte es un invite ID de 8 caracteres que sirve para llegar a un vestíbulo público, y la credencial que emite el host no tiene campo donde poner el secreto. Quien entró nunca tuvo con qué volver por su cuenta |
| "Limita las reglas del firewall a las IPs virtuales de tus amigos" | Escribir reglas a mano en Windows, por juego, y editarlas cada vez que entra o sale alguien | La interfaz virtual nace en cuarentena. Se abren **solo** los puertos del perfil del juego activo, **solo** en la máquina del host, y **solo** hacia las IPs de los miembros presentes. Se recalcula entero en cada cambio de miembros o de juego. `FirewallRule` ni siquiera tiene forma de expresar "cualquiera" |
| "Que no se cuelen nodos nuevos" | Vigilar la consola | Entrar a la red real exige una credencial que emite el host. El código lleva al vestíbulo, que es público y desechable a propósito, y de ahí no se pasa sin que el host emita |
| "Saca a alguien cambiando el secreto" | Cambiar el secreto y **mudar a todos** a la red nueva, o sea cortar la partida | Dos controles independientes: revocar una credencial saca a uno en un segundo sin tocar el código, y renovar el código cierra la puerta sin tocar a los presentes. La partida no se entera |
| "No uses los nodos públicos compartidos" | Acordarse | Kanpachi apunta a su propio seed y arranca el motor con `--no-listener`, `--disable-upnp` y el portal RPC fijado a loopback. Hay un test que falla si alguien saca esas banderas |

Y tres cosas que la guía ni menciona porque no son configurables en EasyTier:

- **Cuarentena por defecto**, que es lo contrario de una LAN plana. En EasyTier, estar en la red significa alcanzarse en todos los puertos, y esa es exactamente la propiedad que este producto existe para no tener.
- **El catálogo de juegos.** Nadie tiene que saber que Project Zomboid habla UDP 16261-16262. El perfil lo dice, y hay puertos que **ningún** perfil puede pedir, con 445 y 3389 a la cabeza.

  **El catálogo es editable, y el techo de un perfil trucho es acotado a propósito:** lo peor que consigue es que un miembro presente de esa sala alcance un puerto tuyo por el túnel, jamás exposición a internet, y para eso te hace falta importarlo, entrar a su sala, tener algo escuchando ahí, y no mirar la pantalla que lo lista. La cadena entera, con lo que la corta en cada paso, está en `03-arquitectura.md`.
- **Que se cierre solo.** Los cortes automáticos de la decisión 26 no existen en un motor de red: nadie cierra tus puertos porque el host se fue hace veinte minutos.

**Lo honesto también:** todo esto lo puede hacer una persona con paciencia y conocimiento, a mano, cada vez. El producto no inventa una capacidad nueva, quita el "cada vez" y el "con conocimiento".

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
| **kanpachi-seed** | Nodo de rendezvous que presenta a los peers entre sí | Droplet Linux, systemd |
| **kanpseed** | El binario del seed: servidor, CLI e instalador a la vez. Resuelve invite IDs, guarda tarjetas cifradas que no puede leer, cuenta miembros leyendo el RPC de EasyTier, y sirve la página de invitación renderizada | Droplet, servicio de systemd junto a EasyTier |
| **kanpachi-catalog** | Perfiles JSON de juegos: puertos, descubrimiento LAN, ejecutables y verificación. Viene en el instalador, se amplía con el creador de perfiles y se comparte exportando un `.json` plano | Embebido más `ProgramData` |

```
┌─────────────────────────── PC del jugador ───────────────────────────┐
│                                                                      │
│   kanpachi-ui  (Flutter, sin privilegios)                            │
│        │  named pipe + token                                         │
│   kanpachi-daemon  (servicio, elevado)                               │
│        ├── kanpachi-core  (identidad, catálogo, política)            │
│        ├── kanpachi-engine (proceso hijo) ─ adaptador Wintun (kanpachi0) │
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

1. **Seguro por defecto.** La interfaz virtual nace en cuarentena: sin una sola regla de permiso, y con los puertos de SMB, RDP y compañía bloqueados a propósito en las dos direcciones. Cada apertura es explícita, por perfil de juego, solo hacia miembros presentes, y se revierte sola. El router jamás se toca: todas las conexiones se inician desde adentro, así que no hay reenvío de puertos, no hay UPnP y nada queda escuchando en tu IP pública. Kanpachi además avisa si el propio juego dejó una regla que te expone en tu red de casa.
2. **Si se puede detectar, no se pregunta.** Ruta de Steam, juegos instalados, MTU, rango de IPs: todo se resuelve solo. La configuración manual no existe como concepto. La contracara igual de importante: **lo detectado nunca limita al usuario**. La detección ordena y sugiere, jamás filtra ni bloquea, porque toda detección falla alguna vez.
3. **El código es un ticket, y el host tiene la cerradura.** El código de sala no es el secreto de la red: el host lo canjea por una credencial temporal. Eso le da tres controles reales sin servidor de por medio, expulsar a alguien, renovar el código y cerrar la sala. El secreto de la red de una sala lo genera el host, es aleatorio, y no vive en ningún servidor. Sin cuentas y sin contraseñas.
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
