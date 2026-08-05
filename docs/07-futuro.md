# Futuro

Formato: cada bloque dice qué lo activa, qué implica y qué ya quedó preparado. Nada de esto entra al código antes de su disparador. Esta lista es la defensa contra el scope creep.

## 1. Lanzamiento público

**Disparador:** decisión explícita de abrirlo fuera del grupo.

Lo que se activa, en orden:

1. **Firma de código.** Certificado de una CA tradicional a nombre de Accentio, del orden de 400 a 900 USD al año. Azure Trusted Signing quedó descartado: la elegibilidad se limita a EE. UU., Canadá, UE y Reino Unido, Brasil queda fuera. Sin firma, SmartScreen mata la conversión de desconocidos.

   **El problema empieza antes que SmartScreen.** Los binarios compilados en Go sin firmar disparan falsos positivos del modelo de aprendizaje automático de Defender con frecuencia, típicamente como `Trojan:Win32/Wacatac.*!ml`, y el archivo se pone en cuarentena solo. Es un problema conocido y reportado por el propio equipo de Go de Microsoft. Un daemon Go que además crea adaptadores de red y toca el firewall es un candidato de manual.

   Mitigaciones, en orden de efectividad:
   - Firmar. Es la única solución de fondo.
   - **Evitar `-ldflags="-s -w"`.** Quitar la tabla de símbolos aumenta mucho la probabilidad de detección; el binario queda más grande y deja de parecer empaquetado.
   - Enviar el binario al portal de análisis de malware de Microsoft antes de cada release, para que la detección se retire.
   - Publicar hashes y el enlace al repositorio desde la misma página de descarga.

   **MSIX y Microsoft Store quedan descartados, por técnica y no por política.** El premio habría sido grande: las apps publicadas en la Store las firma Microsoft, así que SmartScreen y los falsos positivos de Defender desaparecen de un plumazo. El bloqueo es que **MSIX no soporta drivers**, es una limitación oficial de Microsoft, y Kanpachi necesita crear un adaptador de red virtual, o sea Wintun, o sea un driver. Sin adaptador no hay interfaz que el juego pueda usar. Los servicios sí se soportan en MSIX desde Windows 10 2004, el driver no. El camino híbrido que existe (driver por MSI, aplicación por MSIX) deja dos empaquetados, dos firmas y el doble de superficie de fallo para llegar al mismo sitio. Anotado aquí para que no reaparezca como idea nueva en seis meses.
   Para el modo privado con el grupo, el falso positivo de Defender ya aplica hoy: si el instalador desaparece al descargarlo, la causa es esa y no un error del instalador.

2. ~~**Página de invitación.**~~ **Movida a la v1.** Dejó de ser futuro: la forma URL (`kanpachi.accentio.dev/A7K2M9QX`) resuelve a una página real que sirve el registro del seed, con el patrón de Discord. Diseño en `05-ui.md`, decisiones 17, 24 y 25. Sin esa página, la forma URL es peor que el código pelado, porque promete algo que no existe.

3. **Relay dedicado.** El relay de datos se muda del droplet de Accentio a un VPS aparte (~5 USD/mes). Razones: reputación de IP ante abuso, aislamiento del trabajo de clientes, y que el riesgo pase a ser una línea de gasto separada. Límite de banda por sala desde el primer día (~2 Mbps: sobra para juegos, mata el uso como túnel de archivos).
4. **Autoupdate firmado.** Obligatorio con público: un fallo de seguridad no se corrige pidiéndole a desconocidos que reinstalen.
5. **Checksums publicados** en la landing y enlace visible al repositorio, para que el binario sin firmar sea verificable.
6. **Licencia formal.** Cliente en AGPL o GPLv3 (nadie lo toma y lo cierra), catálogo en CC0 (la comunidad manda perfiles por PR, ese es el foso real del producto).
7. **Endurecer el seed:** limitar qué redes puede relevar, métricas de consumo por red, rotación de la lista de semillas vía DNS.

## 2. Identidad remota y salas persistentes

**Disparador:** el grupo quiere salas que existan **sin nadie dentro**, o hace falta moderación.

Ojo con el disparador, que cambió. "Sobrevivir a un reinicio" ya no lo dispara, porque eso ya está: la decisión 2 dejó al host reabriendo su sala con el mismo código y el mismo juego después de un apagón, y a los invitados con "volver a la última sala". Lo que sigue faltando es que la sala exista mientras **nadie** la sostenga, y eso sí necesita un servidor.

Se implementa un `RemoteRoomProvider` contra un backend pequeño (el kanpachi-rooms que se eliminó de la v1 renace aquí, con propósito real). Las cuentas siguen sin ser obligatorias: una sala persistente puede ser un código reservado, nada más.

**Lo que hay que resolver antes, y no es técnico:** hoy la sala tiene un dueño con una máquina, y su firewall es el que contiene la exposición. Una sala sin nadie dentro no tiene esa máquina, así que hay que decidir quién sostiene la cuarentena mientras no hay host. Sin esa respuesta, esto no se implementa.

## 3. Más plataformas

| Plataforma | Costo real | Nota |
|---|---|---|
| Linux cliente | Bajo: implementación nftables de `netfw` (~150 líneas), paquete deb, unidad systemd, matriz de pruebas propia | La interfaz ya existe. Steam Deck cuenta como Linux |
| macOS | Alto: cuenta Apple Developer, notarización, System Extension para el TUN | Solo con demanda real |
| Móvil | Muy alto y dudoso: VpnService en Android es viable, el caso de uso gamer casi no existe | Revisar solo si el producto cambia de naturaleza |

## 4. Catálogo v3

**Disparador:** juegos que el esquema v2 no describe.

El v2 ya cubre capas, creador de perfiles, verificación por uso e intercambio por `.json` (ver `06-catalogo.md`). Lo que queda para más adelante:

- Variantes por modo de juego: server dedicado y "Open to LAN" con puertos distintos dentro del mismo perfil.
- Fuentes fuera de Steam: Epic, GOG, Xbox PC. Cada una con su detección propia.
- Catálogo comunitario por PR, con `verified` obligatorio y prueba documentada. Es la única parte del proyecto donde alguien puede aportar sin tocar código privilegiado.
- Índice remoto opcional de perfiles, con actualización sin releasear la app.

## 5. Motor

**Disparador:** EasyTier limita algo concreto (rendimiento, una plataforma, un bug estructural).

El puerto `EnginePort` deja tres salidas sin tocar el producto: actualizar EasyTier, migrar a un control plane Headscale envuelto, o motor propio sobre wireguard-go (solo con recursos de sobra, es el camino de meses). Peer relays propios e IPv6 entran en esta misma conversación.

## 6. La sinergia con Statio

La más valiosa de esta lista. Statio hoy depende de Tailscale como canal exclusivo entre GitHub Actions y el agente: un tercero controla el plano de control, los precios y las cuentas de los clientes. Las piezas de Kanpachi son exactamente las que quitan esa dependencia:

- `identity/` con derivación local: enrollment de agentes sin cuentas de terceros.
- El seed: rendezvous propio sobre infraestructura propia.
- El puerto `EnginePort`: el mismo túnel, con Tailscale vía tsnet hoy en Statio, con motor propio mañana.

Si un día Kanpachi se archiva como juguete, este es el valor que queda.

## 7. Métricas

Solo con lanzamiento público, siempre opt-in, nunca en el modo privado. Lo mínimo que responde preguntas reales: tasa de directo contra relay, versión, tipo de NAT agregado. Jamás qué juega quién ni con quién.

## 8. Host headless en Linux, con asistente de terminal

**Disparador:** alguien del grupo quiere hospedar desde un VPS que ya paga, en vez de desde su PC. El caso típico es un servidor de Minecraft que ya corre ahí de todos modos.

**Qué es:** un binario de Kanpachi para Linux, sin interfaz gráfica, con un asistente interactivo en la terminal que hace el mismo recorrido que la UI de Flutter. Se abre, elige el juego de la biblioteca, y muestra el código. Ese código se pasa por Telegram y lo usan clientes Windows normales, que no se enteran de nada. Para ellos la sala es idéntica a una creada desde Windows.

**Por qué es barato, y es la prueba de que la arquitectura sirve.** Si `core/` se mantiene puro, este caso reusa sin tocar nada: `identity/`, `catalog/`, `policy/` y `EnginePort`. Lo que cambia es la periferia, que es justo donde debe estar el costo:

| Pieza | Qué hay que escribir |
|---|---|
| `netfw` | Implementación nftables del mismo puerto, ver punto 3 |
| `netcfg` | Equivalente con `iproute2`, sin los problemas de reidentificación de Windows |
| Entrada | Asistente de terminal en vez de Flutter, sobre el mismo protocolo JSON-RPC |
| Transporte | Socket Unix en vez de named pipe |

**Una decisión que se toma hoy y lo habilita gratis:** el protocolo de la API local se define aparte de su transporte. El named pipe es una implementación, no el contrato. Eso ya está reflejado en `03-arquitectura.md` y no cuesta nada mientras solo exista Windows.

**Es la excepción explícita a "el usuario nunca abre una terminal".** Esa regla protege al jugador, que no tiene por qué saber qué es una terminal. Quien administra un VPS ya vive en una, y para esa persona la terminal es la interfaz natural. Personas distintas, contextos distintos. La regla sigue intacta para el cliente Windows, que es el producto.

**Lo que NO cambia:** el modelo de identidad, el catálogo, las invariantes de puertos, el rol de host de la decisión 20. Un host en Linux es un host, con las mismas reglas.

## 9. Fijar la clave pública del seed

**Disparador:** preocupación concreta por un intermediario en la conexión al servidor de encuentro, o el lanzamiento público.

El modo seguro del motor permite que el cliente fije la clave pública del seed, o sea comprobar que se está hablando con el servidor esperado y no con un impostor en el medio. Refuerza la decisión 16, donde hoy la mitigación de "pegar un código puede conectarte al servidor de un desconocido" es únicamente la tarjeta de confirmación en pantalla.

**Por qué no entra ya.** El seed solo ve networkIDs opacos e IPs públicas, así que un intermediario ahí no descifra tráfico ni entra a la sala. El beneficio es real y acotado, y el costo es distribuir y rotar una clave fijada, que es infraestructura que hoy no existe.

Nota: **las credenciales temporales ya no viven en este documento.** Se probaron contra los binarios y pasaron a ser el mecanismo central de la decisión 2, con la expulsión de la decisión 22 y el canal de la decisión 23 encima.

## 10. Comprobar el router desde el seed

El canario de la decisión 28 contesta "¿la Protección Kanpachi está conteniendo la
sala?". Hay otra pregunta distinta, y es la que la gente tiene más metida en la
cabeza: **"¿mi router tiene algo abierto hacia internet?"**. Es el miedo con el
que nació el producto.

Esa la podría contestar el seed, que está en internet y no en la sala, y **no
necesita a nadie más**: ni un segundo miembro, ni coordinación. El host le pide
que le marque de vuelta, y si el seed llega, algo está reenviando puertos.

**Qué lo frena.** Con CGNAT la IP pública **no es del usuario**: la comparte con
vecinos. Marcarle a esa IP puede ser marcarle a la máquina de otra persona, que
no pidió nada y no está en ninguna sala. Y CGNAT domina en LatAm, que es el caso
central del producto, así que no es un borde raro: es la mayoría.

**Qué lo activaría.** Una forma de acotar el sondeo a la máquina que lo pide, sin
escanear una IP compartida. Ideas sin evaluar: que el host abra un oyente
efímero y el seed solo confirme haber llegado a ESE número con un valor que solo
el host conoce, lo cual sigue tocando la IP del vecino aunque no revele nada de
él; o detectar CGNAT primero (el motor ya reporta el tipo de NAT en `NetCheck`) y
ofrecer la comprobación solo cuando la IP sea de verdad del usuario.

Hasta que eso esté resuelto, no se hace. Escanear la IP de un tercero no es algo
que este producto pueda hacer aunque el resultado fuera útil.

## 11. El desinstalador, que tiene que borrar los DOS grupos de firewall

Hoy no hay desinstalador, y la cuarentena de base la escribe el daemon en cada
arranque, con un método que solo agrega. O sea que hay una forma de ponerla y
ninguna de quitarla.

**Es requisito de producto, no cortesía.** Dejar bloqueos explícitos sobre el 445
y el 3389 en toda la máquina con Kanpachi ya borrado deja al usuario sin
compartir archivos ni Escritorio remoto, **sin causa visible y sin nada que
culpar**. El síntoma aparece semanas después, en una máquina donde el producto ya
no está instalado, así que nadie va a relacionarlo.

Lo que tiene que hacer, en este orden: detener y borrar el servicio, purgar los
grupos `Kanpachi` y `Kanpachi-base` **por igualdad exacta de cada nombre y jamás
por prefijo**, eliminar el adaptador Wintun, borrar ProgramData y borrar Program
Files.

**Disparador:** cualquier instalador que se distribuya fuera de la máquina de
desarrollo. Es el mismo trabajo, no se puede hacer uno sin el otro.

Mientras tanto, `04-flujos-y-configuracion.md` lleva el comando de PowerShell
exacto para quitarla a mano.

## 12. El permiso de ICMP echo en la cuarentena de base

La cuarentena lo prometía "para que el diagnóstico funcione" y **no se escribe**.
Ninguna función depende de él: el sondeo de MTU manda el ping hacia AFUERA, que
la salida no bloquea, y la latencia de un miembro la mide el motor por su propio
protocolo.

El costo de tenerlo no era pequeño: sería la única regla de la cuarentena que
ABRE en vez de cerrar, sin acotar, contestando el ping en toda red a la que la
máquina se conecte, para siempre y con Kanpachi apagado.

**Disparador, y son dos a la vez:** un caso de uso concreto que necesite que esta
máquina CONTESTE un ping, y una forma de acotarlo que no lo convierta en nada
cuando el adaptador virtual no existe. La segunda es la difícil, y es la misma
razón por la que los bloqueos de la cuarentena tampoco se acotan: un alcance que
deja de casar convierte un permiso en un cierre y un bloqueo en nada.

## 13. Una alerta para un miembro que miente demostrablemente

`CanaryMismatch` es el veredicto de una ronda donde el canario NO fue tocado y
algún miembro informó que sí llegó. El host tiene una prueba propia de que ese
informe es falso.

Hoy llega al log, al `CanaryView` y a la pantalla, y **no se vuelve un
`AlertKind`**. El motivo es que también sale de una carrera inocente entre un
informe y el cierre del canario, y una alerta que se enciende por una carrera le
enseña al usuario a ignorar las alertas. Es la misma doctrina ya escrita para
`AlertAuditFailed`.

**Disparador:** que los `CanaryMismatch` se midan y se vea que no vienen de la
carrera, o sea que se repitan sobre el mismo remitente en rondas seguidas. Ahí
deja de ser ruido y pasa a ser una señal sobre una persona concreta, y ahí hace
falta decidir antes qué se le ofrece al usuario: nombrarlo no sirve de nada si la
única acción posible es expulsar, porque expulsar no impide volver y el código de
invitación no es un secreto. Ver decisión 8.

## 14. Lo que se decidió NO hacer

Escrito para resistir la tentación:

- **Detectar la ejecución de juegos:** ni para aplicar perfiles solo, ni para sugerirlos con un banner. Las dos variantes exigen un servicio elevado vigilando qué programas abre el usuario, y abrir puertos sin que nadie lo pida contradice la promesa del producto. Descartado en firme, ver decisión 13.
- **Compartir archivos:** cambia el perfil de riesgo del producto entero y el consumo del relay.
- **Chat y voz:** Telegram y Discord existen y ya están abiertos al lado del juego.
- **Panel web de administración:** una superficie de ataque a cambio de nada, en un producto sin cuentas.
- **Exit node / enrutar internet:** convierte una LAN de juegos en una VPN de anonimato, otro producto, otros problemas legales.

Cada "y si también..." nuevo se anota aquí con fecha y se evalúa contra tres preguntas: ¿mueve la aguja del caso de uso central?, ¿quién lo mantiene?, ¿qué superficie agrega?
