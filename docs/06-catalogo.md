# El catálogo de juegos

## Qué es

El catálogo es la base de conocimiento de Kanpachi: un perfil por juego que describe **qué necesita ese juego para funcionar en una LAN**. Puertos, si descubre partidas por broadcast, cómo se conecta la gente, qué ejecutables lo identifican.

Es lo que convierte a Kanpachi en un producto para alguien que no es técnico. Sin catálogo, la app sería una VPN más donde hay que saber que Zomboid usa 16261 UDP. Con catálogo, eliges "Project Zomboid" de una lista y todo lo demás pasa solo.

## El principio que lo gobierna

**El catálogo es la capa de conocimiento, nunca la capa de política.**

El perfil dice qué necesita el juego. El código decide qué es aceptable conceder. Esa separación es lo que permite compartir perfiles entre personas sin que compartir sea un riesgo.

La consecuencia práctica, escrita como regla: **si un perfil está corrupto, mal escrito o es malicioso, el peor resultado posible es que ese juego no conecte. Nunca que la máquina quede expuesta.**

## Las tres capas

| Capa | Origen | Ubicación | Escribible |
|---|---|---|---|
| **builtin** | Viene en el instalador, verificado por el autor | `Program Files\Kanpachi\builtin.json` | No |
| **mine** | Creado en esta máquina con el creador de perfiles | `ProgramData\Kanpachi\local.json` | Sí |
| **imported** | Importado de un `.json` que compartió alguien | `ProgramData\Kanpachi\local.json` | Sí |

**Precedencia:** `mine` gana sobre `imported`, que gana sobre `builtin`. Un perfil local nunca reemplaza a uno builtin en silencio: si al importar o crear hay colisión de `id`, la UI lo dice y el usuario elige, con el builtin como opción por defecto.

**Confianza visible.** Cada juego en la lista lleva su origen: verificado (builtin), tuyo (mine), o compartido por alguien con el nombre de quien lo verificó (imported). Nadie usa a ciegas un perfil que le llegó por Telegram.

## Los tres momentos, que no hay que confundir

El catálogo toca al juego en tres momentos distintos y ninguno implica vigilar su ejecución. Vale la pena tenerlos separados en la cabeza:

| Momento | Cuándo ocurre | Qué mira | Requiere que el juego esté abierto |
|---|---|---|---|
| **Detectar instalados** | Al abrir la app | Archivos de Steam en disco: `libraryfolders.vdf`, `appmanifest_*.acf` | No. El juego ni se ejecuta |
| **Elegir el juego de la sala** | El usuario hace click en la lista | Nada del sistema. Es una elección manual | No |
| **Modo observación** | Solo dentro del creador de perfiles, disparado por un botón | Una foto de la tabla de sockets del proceso | Sí, lo abre el usuario a propósito |

**La detección de instalados es un atajo, jamás una puerta.** Ordena la lista poniendo arriba lo que encontró. La biblioteca completa del catálogo siempre está accesible con un click, y elegir un juego que la detección no vio funciona exactamente igual, sin advertencias. Motivos por los que la detección falla y son normales: el juego está fuera de Steam, vive en Epic, GOG o Xbox PC, está en una unidad que no se pudo leer, o el manifiesto tiene un formato inesperado. Ninguno de esos casos puede impedir crear una sala.

Fuera del creador de perfiles, **Kanpachi no consulta procesos nunca**. No sabe que abriste un juego, no aplica perfiles solo, no cierra puertos porque el juego se cerró. Elegir el juego abre los puertos, salir de la sala los cierra, y no hay tercera vía.

La decisión de no detectar la ejecución, con sus alternativas descartadas, está en `02-decisiones-de-diseno.md`, punto 13.

## Esquema v2

```json
{
  "id": "project-zomboid",
  "schema": 2,
  "name": "Project Zomboid",

  "detect": {
    "steam_appid": 108600,
    "executables": ["ProjectZomboid64.exe", "ProjectZomboid32.exe"]
  },

  "host_ports":   [{ "proto": "udp", "range": "16261-16262" }],
  "client_ports": [],
  "lan_discovery": false,

  "system_tweaks": {
    "broadcast_route": false,
    "multicast_route": false,
    "prefer_ipv4": false,
    "directplay": false
  },

  "connect_hint": {
    "kind": "direct_ip",
    "text_es": "En el juego: Join > escribe la IP del host"
  },

  "bind_hint": {
    "file": "Zomboid/Server/servertest.ini",
    "key": "ServerIP",
    "note_es": "Se puede fijar a la IP de Kanpachi para no escuchar en la tarjeta física"
  },

  "verified": {
    "date": "2026-07-31",
    "by": "alvaro",
    "method": "partida real, 4 jugadores",
    "game_version": "41.78"
  }
}
```

Qué alimenta cada campo:

| Campo | Para qué se usa |
|---|---|
| `detect.steam_appid` | Ordenar la lista, poniendo arriba lo que parece instalado. Nunca oculta ni bloquea nada |
| `detect.executables` | Auditoría de reglas de firewall ajenas, y el modo observación del creador |
| `host_ports` | Reglas de firewall en la máquina que hospeda |
| `client_ports` | Reglas en quien se une. **Vacío en la enorme mayoría**, ver abajo |
| `lan_discovery` | **Hoy no lo lee nadie.** Se parsea y se conserva, y ningún adaptador lo consume. Ver abajo |
| `system_tweaks` | Ajustes de Windows que ese juego necesita, aplicados por `netcfg` y revertidos al salir de la sala |
| `connect_hint` | El texto exacto de la pantalla en sala |
| `bind_hint` | Información para el usuario avanzado. Kanpachi jamás edita ese archivo |
| `verified` | Confianza mostrada en la UI, y disparador de revalidación |

`connect_hint.kind` acepta: `direct_ip`, `lan_browser`, `steam_friends`.

`proto` acepta: `udp`, `tcp`, `both`. El tercero existe porque hay juegos que piden el mismo rango en los dos protocolos, y obligar a escribirlo dos veces gastaría dos de los ocho rangos del tope para decir una sola cosa. **No es un protocolo, es escritura:** el código lo expande a dos reglas antes de llegar al Firewall de Windows, que tiene un protocolo por regla y solo uno. La UI lo escribe `TCP/UDP` y lo acepta también así al leer.

`origin` es la excepción de una sola capa. Solo lo escribe `local.json`, que es el único archivo que mezcla perfiles propios con importados, y sirve para que la distinción sobreviva a un reinicio. **En un archivo de intercambio va ausente, y si viene se ignora:** la capa la fija quien carga, jamás el archivo. Sin esa regla, un `.json` que llegara por Telegram podría declararse `mine` y ganarle en precedencia a un builtin verificado.

### `lan_discovery` está declarado y todavía no lo paga nadie

El campo se parsea, sobrevive al viaje de ida y vuelta a disco, y **ningún
adaptador lo lee**. Lo que activaría sería el relay de broadcast UDP del motor,
y esa capacidad está en la lista de las que van siempre apagadas, con su motivo:
capturaría el tráfico de la red de casa del usuario. Encenderla exige antes
decidir cómo se acota a la red virtual, y esa decisión no está tomada.

Lo que sí funciona hoy, y es lo que usan los perfiles clásicos de LAN, es
`system_tweaks.broadcast_route`: una ruta `255.255.255.255/32` sobre el
adaptador virtual, que manda el descubrimiento por la sala en vez de por la
tarjeta física. Cubre el caso de un buscador de partidas que emite a la
dirección de difusión.

Disparador para pagar `lan_discovery`: un juego del catálogo que no aparezca en
su lista de partidas con `broadcast_route` puesto.

### `client_ports` decide la topología, y por eso es el campo más delicado

Es el campo que responde la pregunta "¿Santiago puede alcanzar mi PC?".

| `client_ports` | Topología | Qué pasa |
|---|---|---|
| `[]` | **Estrella** | Los puertos se abren únicamente en el host. Entre invitados no hay nada abierto. Nadie alcanza la máquina de nadie |
| No vacío | **Malla** | Cada invitado abre esos puertos hacia los demás miembros presentes |

**El default seguro es el default del esquema.** Un perfil que no declara `client_ports` no abre nada en los invitados. Para que un juego pase a malla, alguien tiene que escribirlo explícitamente y probarlo.

**La estrella es la enorme mayoría.** Los juegos con servidor dedicado o con un host que corre la partida son `jugador → host ← jugador`, y nunca `jugador ↔ jugador`. Project Zomboid, Minecraft, Terraria y Valheim caen todos ahí. La malla es cosa de netcode viejo de paso bloqueado, donde cada cliente simula y habla con todos.

**Qué exige un perfil de malla.** Poner algo en `client_ports` expande el radio de explosión de todos los miembros, no solo del host, así que el listón es más alto:

1. Se justifica en el propio perfil: por qué ese juego no funciona en estrella.
2. Se prueba en partida real, igual que cualquier `verified`, y además se comprueba que **no** funcionaba con `client_ports` vacío. Copiar los puertos del host por si acaso está prohibido.
3. **La UI lo dice antes de entrar**, con las palabras de `05-ui.md`: "Este juego conecta a todos con todos".

Ante la duda, `[]`. Un juego de malla mal clasificado como estrella no conecta, y eso se nota y se corrige. Al revés abre puertos en cuatro máquinas que no los necesitaban, y no se nota nunca.

### Los ajustes de sistema, uno por uno

Windows rompe de varias formas distintas el multijugador sobre un adaptador virtual. Cada juego sufre unas y no otras, por eso son campos del perfil y no ajustes globales.

| Campo | Qué hace | Qué juegos lo piden |
|---|---|---|
| `broadcast_route` | Ruta persistente `255.255.255.255/32` sobre `kanpachi0` | Descubrimiento LAN clásico, era DirectPlay |
| `multicast_route` | Ruta `224.0.0.0/4` | mDNS, SSDP, buscadores de servidores, motores modernos |
| `prefer_ipv4` | Política de prefijo `::ffff:0:0/96 100 4` | Netcode viejo que solo habla IPv4 y se confunde si Windows le entrega un destino IPv6 |
| `directplay` | Habilita el componente legado de Windows | Juegos previos a 2005, aproximadamente |

Tres reglas de manejo:

1. **Se aplican al activar el perfil y se revierten al salir de la sala.** El estado previo se persiste, igual que las reglas de firewall ajenas, para poder restaurar tras una salida sucia.
2. **`prefer_ipv4` no desactiva IPv6.** Solo reordena la selección de destino de RFC 6724 para nombres de doble pila. El transporte del motor sigue usando IPv6 cuando le conviene, que es lo que hace más confiable la perforación de NAT.
3. **En la duda, `false`.** Un ajuste de sistema aplicado sin necesidad es superficie regalada.

### Lo que nunca se toca, por más que un juego lo pida

No existe campo en el esquema para esto, y es deliberado:

- **Detección de redes** y **Compartir archivos e impresoras** del Firewall de Windows. Se habilitan por perfil de firewall, no por adaptador, así que encenderlos para la red virtual los enciende también en la LAN de casa del usuario. Es exactamente el escenario que Kanpachi existe para evitar.
- La ruta por defecto `0.0.0.0/0` o `::/0`, en cualquier forma.
- Exclusiones de antivirus.
- Cualquier archivo de configuración de otro programa. `bind_hint` se muestra, jamás se aplica.

## Invariantes, verificadas al cargar cada perfil

Estas viven en código y no tienen campo equivalente en el JSON, a propósito:

1. **Puertos prohibidos siempre:** 22, 135, 137, 138, 139, 445, 3389, 3702, 5357, 5358, 5985, 5986. Un perfil que los pida se rechaza entero. Los tres del medio son el descubrimiento de dispositivos de Windows, que publica qué máquina es esta y qué comparte. **1900 y 5353 no están a propósito:** son SSDP y mDNS, y por ahí descubren la partida en la LAN varios juegos del catálogo.
2. **Máximo 8 rangos por perfil.** Un perfil con 40 rangos está mal escrito o es malicioso.
3. **`RemoteAddresses` siempre son los miembros presentes.** No existe forma de expresar "cualquiera".
4. **Solo reglas entrantes.** No hay campo para salientes ni para reenvío.
5. **Nunca permitir por ejecutable.** Las reglas son por puerto, protocolo y dirección. Jamás "permitir todo lo de este exe".
6. **Validación estricta.** Un perfil inválido se rechaza completo. Nunca se carga a medias ni se degrada a algo más permisivo.
7. **`bind_hint` es informativo.** Kanpachi lo muestra y nunca escribe en archivos de otros programas.
8. **`system_tweaks` es un conjunto cerrado.** Solo se aceptan las cuatro claves definidas, todas booleanas. Un perfil con claves desconocidas se rechaza. No existe forma de expresar "ejecuta este comando" ni "habilita este grupo de reglas del firewall". La regla vale para el perfil ENTERO y no solo para ese objeto: cualquier clave que el esquema no defina, en cualquier nivel, rechaza el perfil.
9. **Un perfil que no abre ningún puerto se rechaza.** No describe nada: no hay regla que generar, y elegirlo en la lista sería un juego que se activa y no hace nada. Es un archivo a medio escribir, y decirlo vale más que aceptarlo en silencio.

Cada rechazo queda en el log con la razón, y la UI marca el juego como no disponible con el motivo en lenguaje humano.

Tres detalles de cómo se aplican, que son parte de la invariante y no del formato:

- **Un puerto prohibido se detecta por CONTENCIÓN, no por igualdad.** Un perfil que pida `440-450` abre el 445 igual que si lo hubiera nombrado, y quien escriba un perfil malicioso lo escribiría exactamente así.
- **El tope de 8 rangos cuenta `host_ports` y `client_ports` juntos.** Contarlos por lista lo dejaría en dieciséis por la puerta de atrás.
- **Un perfil es UN objeto JSON y nada detrás.** El decodificador se detiene al cerrar el primero, así que un archivo con `{perfil bueno}{perfil malo}` pasaría entero mostrando solo el primero. Es la forma clásica de colar contenido que el revisor humano no ve.

Y una que vive en el otro lado de la frontera: los puertos prohibidos se vuelven a comprobar **al generar las reglas**, no solo al cargar el perfil. Un perfil solo existe validado, así que esa segunda comprobación no debería poder saltar nunca; existe porque ahí es donde un puerto se abre de verdad, y una invariante de seguridad tiene que vivir también donde ocurre el acto.

## El creador de perfiles

Lo abre "Agregar juego" al final de la lista. La idea: **el usuario no adivina puertos, Kanpachi los observa**.

### Paso 1, elegir el juego

Lista de ejecutables detectados que todavía no tienen perfil, sacada de las bibliotecas de Steam. Más un botón "Buscar el .exe a mano" para juegos fuera de Steam.

### Paso 2, observación

El usuario abre el juego por su cuenta y luego pide la foto. Kanpachi no espera ni vigila que el juego arranque:

```
┌──────────────────────────────────────────────┐
│  Creando perfil: Valheim                     │
│                                              │
│  1. Abre el juego y crea una partida         │
│     como si fueras a jugar con amigos        │
│  2. Vuelve aquí y pulsa el botón             │
│                                              │
│  ┌──────────────────────────────┐            │
│  │   Ya abrí el juego, revisar  │            │
│  └──────────────────────────────┘            │
│                                              │
│  Encontrado:                                 │
│    UDP 2456   escuchando en todas las redes  │
│    UDP 2457   escuchando en todas las redes  │
│    UDP 2458   escuchando en todas las redes  │
│                                              │
│  [Revisar otra vez]      [Guardar perfil]    │
└──────────────────────────────────────────────┘
```

**Cómo funciona por dentro.** Cada pulsación del botón dispara **una consulta puntual** a las tablas de sockets del sistema con `GetExtendedUdpTable` y `GetExtendedTcpTable` de `iphlpapi.dll`, pidiendo el PID dueño de cada entrada. Es la misma información que muestra `netstat -ano`. El usuario puede repetirla las veces que quiera, y los resultados se acumulan mientras el asistente esté abierto.

Límites deliberados:

- **No hay espera de fondo.** El daemon no queda observando a que aparezca un ejecutable. La consulta ocurre cuando el usuario la pide y termina ahí.
- **Solo mientras el asistente está abierto.** Al cerrarlo no queda nada corriendo, ni suscripciones, ni temporizadores.
- **Nunca durante el juego normal.** Fuera de este asistente, Kanpachi jamás consulta procesos.

Filtros que aplica a cada foto:

- Sigue el **árbol de procesos** partiendo del ejecutable elegido, porque muchos juegos arrancan desde un launcher y el servidor es un proceso hijo.
- **Conserva solo lo que escucha en `0.0.0.0`,** que es lo único que necesita regla. Un bind a `127.0.0.1` no sale de la máquina, y uno a la IP de la LAN doméstica (`192.168.1.5`) tampoco va a escuchar en la interfaz virtual haga lo que haga el firewall: una regla para él no arregla nada y sí abre un puerto por gusto.
- **El árbol de procesos es obligatorio, no una mejora.** Sin él la foto trae todos los sockets de la máquina: el navegador, el antivirus, el propio Kanpachi. Si no se pudo armar, se mira solo el proceso elegido.
- Descarta ruido conocido de Steam (27015 a 27030, 27036) salvo que el usuario marque que ese juego sí los usa.
- Agrupa puertos contiguos en rangos.

### Paso 3, descubrimiento LAN

Una sola pregunta, resuelta probando en vez de adivinando:

```
¿El juego encuentra las partidas solo, en un menú de
"LAN" o "Red local"?

  ○ Sí, aparecen solas en una lista
  ○ No, hay que escribir la IP del host
  ○ No estoy seguro, probemos
```

"Probemos" activa el relay de broadcast, pide abrir el menú de partidas del juego y confirmar si aparece algo. El resultado escribe `lan_discovery` y, de paso, el `connect_hint`.

### Paso 4, validación

El perfil nace con `verified: null` y una etiqueta **sin probar** en la UI. Funciona igual, con un aviso: "Este perfil no se ha probado en una partida real".

Al **salir de una sala** donde ese juego estuvo activo y hubo al menos dos personas, Kanpachi pregunta una vez: "¿Funcionó bien el multijugador de Valheim?". Un sí rellena `verified` con fecha, nombre del usuario, método y la versión del juego si se pudo leer del manifiesto de Steam. Un no marca el perfil para revisión y ofrece repetir la observación.

La pregunta se dispara por el evento de salir de la sala, que es una acción del usuario. Kanpachi no sabe si de verdad jugaron, y por eso pregunta en vez de suponer.

Esa es la única forma en que un perfil llega a estar verificado. No se puede marcar a mano.

### Paso 5, guardado

Se escribe en `local.json` con origen `mine`. Aparece de inmediato en la lista de juegos. Sin reiniciar nada.

## Exportar e importar

### El formato

Un archivo plano, legible, sin binarios ni compresión:

```json
{
  "kanpachi_catalog": 1,
  "exported_at": "2026-07-31T14:22:00Z",
  "exported_by": "alvaro",
  "app_version": "1.0.0",
  "profiles": [
    { "id": "project-zomboid", "schema": 2, "...": "..." },
    { "id": "valheim",         "schema": 2, "...": "..." }
  ]
}
```

Nombre sugerido: `kanpachi-catalogo-2026-07-31.json`. Se manda por Telegram como cualquier archivo.

**Sin firma criptográfica, deliberadamente.** Firmar daría una sensación de autoridad que no corresponde: lo que protege al que importa no es la firma, son las invariantes que corren de todos modos. El origen se muestra en la UI y el usuario decide.

### Exportar

Botón "Exportar catálogo" con dos opciones: todo, o solo los perfiles propios. Por defecto, solo los propios, que es lo que tiene sentido compartir. Los builtin ya los tiene el otro.

### Importar

Al abrir un `.json` compartido, antes de escribir nada:

```
┌──────────────────────────────────────────────┐
│  Importar catálogo de Humberto               │
│  3 perfiles en el archivo                    │
│                                              │
│  ☑ Valheim              nuevo                │
│  ☑ Terraria             nuevo                │
│  ☐ Project Zomboid      ya lo tienes         │
│      El tuyo es verificado. Dejarlo.         │
│                                              │
│  ✕ Rust                 rechazado            │
│      Pide el puerto 445, no se permite       │
│                                              │
│  ┌───────────────┐                           │
│  │   Importar    │                           │
│  └───────────────┘                           │
└──────────────────────────────────────────────┘
```

Reglas del importador:

1. **Las invariantes corren primero.** Un perfil que las viole se rechaza con motivo visible, y no se puede forzar.
2. **Nada se sobreescribe en silencio.** Colisión de `id` implica elección explícita, con el perfil existente marcado por defecto si está verificado.
3. **`verified` se conserva con su autor.** El perfil queda como "verificado por Humberto", jamás como verificado por ti. La confianza no se hereda.
4. **Origen `imported`,** visible en la lista de juegos.
5. **Selección por perfil.** Se importa lo que se marque, nunca todo o nada.
6. **Esquema desconocido:** un perfil con `schema` mayor al soportado se salta con un aviso de actualizar Kanpachi. Uno con esquema viejo se migra si hay migración definida.

## Qué trae el catálogo de fábrica

Once perfiles, y **ninguno viene verificado**. Verificado significa que alguien
jugó una partida real y lo confirmó al salir de la sala, y `MarkVerified` es un
no-op deliberado sobre un perfil builtin: lo que se escriba en ese bloque queda
congelado para la vida del producto. La insignia se gana jugando.

| Juego | Puertos del host | Cómo se une la gente | Ajustes |
|---|---|---|---|
| Project Zomboid | `udp 16261-16262` | IP directa | |
| Minecraft (Java) | `tcp 25565` | IP directa, con servidor | |
| Age of Empires II: The Conquerors | `tcp 47624`, `tcp/udp 2300-2400`, `udp 6073` | lista de partidas | `broadcast_route`, `directplay` |
| Counter-Strike 1.6 | `udp 27015` | pestaña LAN, o `connect` | `broadcast_route` |
| Valheim | `udp 2456-2458` | IP directa | |
| Terraria | `tcp 7777` | IP directa | |
| Factorio | `udp 34197` | IP directa | |
| Left 4 Dead 2 | `udp 27015` | consola, con `sv_lan 1` | |
| Stardew Valley | `tcp/udp 24642` | IP directa | |
| 7 Days to Die | `tcp/udp 26900`, `udp 26901-26903` | IP directa | |
| Don't Starve Together | `udp 10998-10999` | pestaña LAN, o `c_connect` | |

### Las seis reglas que se aplicaron a los nueve

1. **`client_ports` vacío en todos.** Los nueve son de servidor autoritativo. La
   vara de la malla está unas líneas más arriba y exige haber comprobado que la
   estrella no funcionaba, y de eso no hay evidencia de ninguno. Una malla mal
   clasificada como estrella falla ruidosamente y se corrige; al revés abre
   puertos en cuatro máquinas para siempre.
2. **Los cuatro `system_tweaks` en `false`, con dos excepciones nombradas.** En
   la duda, `false`. Las dos excepciones son los juegos clásicos de LAN, que
   buscan la partida emitiendo a la dirección de difusión y no tienen forma de
   escribir una IP: Age of Empires II lleva `broadcast_route` y `directplay`,
   y Counter-Strike 1.6 lleva `broadcast_route`. Sin ellos la partida no
   aparece en la lista, que es el único camino que esos dos ofrecen.
3. **`lan_discovery: false` en todos**, incluidos los dos de arriba: hoy ese
   campo no lo consume nadie, y prenderlo sería una promesa que el producto no
   cumple. Ver la subsección de más arriba.
4. **`verified` omitido en todos**, por lo de arriba.
5. **`origin` omitido.** La capa builtin lo ignora al cargar, así que escribirlo
   es una afirmación que el archivo no tiene derecho a hacer.
6. **En `detect.executables`, solo nombres propios del juego.** No es cosmético:
   la auditoría de reglas ajenas compara por nombre de archivo contra TODO el
   almacén de reglas de Windows. Un `javaw.exe` ahí dentro clasificaría la regla
   de cualquier programa Java como "la regla del juego", y le ofrecería al
   usuario un botón para desactivarlas.

### El Age of Empires II que entra, y el que no

El perfil es el de **The Conquerors**, el de 1999, que es el que tiene LAN de
verdad: DirectPlay, la partida del host anunciada por difusión, y la lista de
partidas como único camino de unión.

**La Definitive Edition no sirve con este perfil, y no hay ninguno que le
sirva.** Su multijugador va entero por el emparejamiento en línea, y la opción
"LAN" de su navegador de partidas no llega a abrirse sin conexión a los
servidores del juego. Un perfil suyo abriría puertos que no hacen nada y
mostraría una pista de conexión falsa, con la insignia de venir de fábrica
encima. Por eso el nombre del perfil dice cuál es, y su pista lo repite.

Este es además el único perfil que ejercita `system_tweaks.directplay`, o sea
que Kanpachi enciende esa característica de Windows mientras dure la sala y la
devuelve como estaba al salir. El libro que guarda el estado previo es el mismo
de los demás ajustes.

### Dónde estos perfiles pueden estar mal

Se escribe acá porque un puerto equivocado no falla en ningún sitio: el juego
sencillamente no conecta, y nada apunta al perfil.

- **Minecraft**: el 25565 es del servidor dedicado. "Abrir para LAN" desde el
  menú de pausa liga un puerto distinto cada vez, y un perfil no puede expresar
  eso, así que la pista dice que hace falta un servidor.
- **Stardew Valley**: la guía oficial del juego dice TCP y UDP en el 24642, y va
  así. El transporte del juego es UDP, de modo que el TCP puede sobrar.
- **7 Days to Die**: el 26900 va en los dos protocolos, y del 26901 al 26903
  solo UDP. Los puertos del panel web y de telnet quedan fuera a propósito.
- **Don't Starve Together**: el 10999 es el mundo de arriba y el 10998 es el
  segundo fragmento, que es el que usan las cuevas. Van los dos porque una
  partida con cuevas y solo el primero se queda a medias.
- **Left 4 Dead 2**: el puerto es seguro, la vía de unión es la incómoda. Sobre
  una LAN virtual lo fiable es la consola de desarrollador.
- **Counter-Strike 1.6**: sus ejecutables son los del motor GoldSrc, así que la
  auditoría de reglas ajenas también señalará las de Half-Life y las de otros
  juegos del mismo motor. Es una excepción angosta y consciente a la regla 6,
  y su coste es una pregunta que el usuario puede contestar, jamás una acción
  automática.
- **Age of Empires II**: sus puertos son los de DirectPlay, y no están medidos
  sobre el adaptador virtual. Es el perfil con más piezas nuevas de los once,
  así que también es el primer candidato a que algo salga distinto de lo
  escrito.

## Dónde viven los archivos

```
Program Files\Kanpachi\
  builtin.json          de solo lectura, se reemplaza al actualizar la app

ProgramData\Kanpachi\
  local.json            perfiles propios e importados
  local.json.bak        respaldo de la escritura anterior
```

**Los dos van sueltos, sin subdirectorio `catalog\`.** El builtin se busca al
lado del ejecutable del daemon, y el local en el directorio de datos, que son
los dos sitios que el adaptador ya conoce sin que nadie se los tenga que
configurar.

El daemon carga builtin y local al arrancar, aplica precedencia, valida todo y publica la lista efectiva por la API. Un `local.json` corrupto se ignora entero, con aviso en la UI, y Kanpachi sigue funcionando con los builtin. Nunca se queda sin catálogo.

## Ciclo de vida de un perfil

1. **Nace** con el creador de perfiles, sin verificar.
2. **Se verifica** tras una partida real con dos personas o más.
3. **Se comparte** exportando y mandando el `.json`.
4. **Se revalida** cuando un parche del juego cambia puertos. Si `verified.game_version` no coincide con la versión instalada, la UI sugiere revisar sin bloquear nada.
5. **Asciende a builtin** cuando el autor lo incluye en el instalador de la siguiente versión. Ese es el camino natural: lo que el grupo prueba y comparte termina viniendo de fábrica.

Ese último punto es lo que hace que el catálogo mejore solo con el uso, y es la única parte del proyecto donde otra gente puede aportar sin tocar código privilegiado.
