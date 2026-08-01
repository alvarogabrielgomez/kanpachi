# Flujos y configuración

## El flujo del que se une (el caso Santiago)

Objetivo: menos de 3 minutos desde recibir el link del instalador hasta estar dentro de la partida, con cero preguntas en el grupo.

1. Descarga `kanpachi-setup.exe` del link del grupo.
2. Siguiente, siguiente. Un solo UAC. La barra termina y Kanpachi abre solo.
3. La app ya muestra sus juegos detectados arriba, con la biblioteca completa un click más abajo.
4. Pega el código que le pasaron por Telegram: `A7K2-M9QX`. El campo acepta cualquier formato, con o sin guiones, en minúsculas o mayúsculas.
5. En segundos aparece la sala: los panas en verde (directo) o ámbar (relay).
6. Abre el juego:
   - Juegos con `lan_discovery` (Minecraft, los clásicos): la partida aparece sola en el menú de LAN.
   - Juegos por IP directa (Zomboid): la UI muestra "Conéctate a 100.87.3.1, puerto 16261" con botón de copiar.
7. Juega. Al salir de la sala o apagar la máquina, todo se cierra solo.

Lo que nunca ve: una terminal, un archivo de configuración, una pregunta del firewall de Windows, una cuenta.

## El flujo del host

1. Abre Kanpachi, botón **Crear sala**.
2. Elige el juego. Arriba aparecen los detectados como instalados, abajo la biblioteca completa del catálogo con buscador. Si la detección no encontró el juego, se elige de la biblioteca y funciona igual. Kanpachi abre los puertos del perfil, solo en la interfaz virtual, solo hacia los miembros presentes.
3. Copia el código con un click y lo pega en Telegram.
4. Arranca el servidor del juego como siempre: el dedicado de Zomboid, "Open to LAN" en Minecraft, lo que el juego pida.
5. La UI dice en texto plano qué está expuesto: "Abierto solo dentro de Kanpachi: 16261-16262 UDP, visible para 4 personas. Tu router sigue cerrado".
6. Si el instalador del juego dejó una regla que lo hace visible en la red de casa, aparece el aviso con la opción de desactivarla mientras dure la sala. Se restaura sola al salir.
7. Al salir de la sala, los puertos se cierran, las reglas ajenas suspendidas vuelven a su estado previo y la interfaz regresa a deny all.

**Nada de esto toca el router.** Todas las conexiones se inician desde adentro hacia afuera, por eso el NAT deja pasar la respuesta sin reenvío de puertos ni UPnP. Nadie escucha en la IP pública de nadie.

Nota de rol: "host" es quien corre el servidor del juego. Cualquier miembro puede crear la sala, las aperturas del perfil aplican en la máquina que declara hospedar.

## Qué hace el instalador, paso a paso

1. Manifiesto `requireAdministrator`: el único UAC de la vida del producto.
2. Copia a `Program Files\Kanpachi\`: daemon, UI, `wintun.dll`, perfiles.
3. Crea `ProgramData\Kanpachi\` con ACL: escritura solo SYSTEM y Administradores.
4. Registra el servicio `kanpachi-daemon`, arranque automático retrasado.
5. Política de recuperación del servicio: reiniciar a los 5 s, 10 s, 30 s.
6. Crea el adaptador Wintun `kanpachi0`, fija su categoría de red en **Privada** y escribe `Category=1` en su perfil del registro. Hacerlo aquí evita el diálogo de "¿quieres que este equipo sea detectable?" a mitad de una partida.
7. Fija la métrica del adaptador: IPv4 en 1, IPv6 en 20, `AutomaticMetric` desactivado en ambas pilas.
8. Aplica el grupo base de reglas: deny all sobre la IP del adaptador, ICMP echo permitido, en los tres perfiles de firewall.
9. Genera el token de la API local en ProgramData.
10. Accesos directos en Menú Inicio y escritorio.
11. Arranca el servicio y abre la UI.

**Ninguno de los pasos 6 a 8 es definitivo.** Windows revierte la métrica, la categoría y las rutas en cada evento de identificación de red, que se dispara al cambiar una IP, conectar o desconectar un adaptador, o en eventos de DHCP. Por eso el servicio se suscribe al Event ID 10000 de `Microsoft-Windows-NetworkProfile/Operational` y reaplica todo cada vez. El instalador solo deja el estado inicial correcto para que la primera sesión funcione sin esperar un evento.

Distribución silenciosa para el grupo: `kanpachi-setup.exe /VERYSILENT /NORESTART`.

**Lo que el instalador jamás hace:** agregar exclusiones de Windows Defender, ni habilitar los grupos de reglas de Detección de redes o Compartir archivos e impresoras. Lo primero es lo que hace el malware, y si el binario necesitara una exclusión el problema sería el binario. Lo segundo abriría SMB en la LAN doméstica del usuario, porque esos grupos se habilitan por perfil de firewall y no por adaptador.

## Qué queda instalado

| Artefacto | Ubicación |
|---|---|
| Binarios y wintun.dll | `Program Files\Kanpachi\` |
| Servicio | `kanpachi-daemon`, automático retrasado |
| Adaptador de red | `kanpachi0` (Wintun), categoría Privada |
| Reglas de firewall | Grupo "Kanpachi", los tres perfiles |
| Datos y logs | `ProgramData\Kanpachi\` |

## Desinstalación

En orden: detener y borrar el servicio, purgar todas las reglas del grupo "Kanpachi", eliminar el adaptador Wintun, borrar ProgramData, borrar Program Files. Criterio de calidad: instalar y desinstalar veinte veces seguidas en una VM sin dejar rastro.

## Configuración del droplet (kanpachi-seed)

Contexto: droplet DigitalOcean NYC3 ya existente, Docker sobre Ubuntu, con Reserved IP. El seed convive con las cargas de Accentio, aislado en su propia red de contenedores.

El archivo vive en `seed/docker-compose.yml` del repositorio y se despliega en `~/apps/kanpachi-seed/`, siguiendo la convención del droplet. **Desplegado y verificado el 2026-07-31.**

### Dos procesos, no uno

```
easytier-core       upstream sin modificar, 11010 TCP y UDP
                    rpc en 127.0.0.1:15888
kanpachi-registry   nuestro. Invoca easytier-cli contra ese rpc, sirve / y /api
                    publicado como 127.0.0.1:8010 en el host
```

El registro es lo que hace que esto sea `kanpachi-seed` y no una instalación plana de EasyTier, ver decisión 24. Habla con el motor invocando su CLI, o sea como proceso hijo y jamás vinculado, así que no arrastra su LGPL-3.0.

**Cada contenedor con su propio espacio de red**, los dos en `kanpachi-net` con subred fijada en `10.77.7.0/24`. La subred se fija porque la lista blanca del portal RPC la nombra, y no puede depender de lo que Docker asigne ese día.

Compartir espacio de red entre ambos (`network_mode: service:`) fue el primer intento y **hay que no repetirlo**: al reiniciarse el motor, su espacio se destruye y el registro se queda con un socket en un espacio muerto, "Up" para Docker y sin responder, sin un error en los logs. Ver `03-arquitectura.md` para el razonamiento completo.

El registro escucha en `0.0.0.0` dentro del contenedor: publicar un puerto hace DNAT hacia la IP del contenedor y no hacia su loopback, así que un bind a `127.0.0.1` ahí dentro no recibiría nada. Quien restringe es el `127.0.0.1:` del lado del host.

**La imagen del registro se construye SOBRE la de EasyTier.** El registro necesita `easytier-cli` y necesita que corra; copiarlo a una base cualquiera es apostar a que coincidan libc y versión, y partir de su imagen elimina la apuesta. Sigue siendo agregación y no vinculación: son ejecutables que conviven y se invocan como procesos.

```bash
docker compose build          # el contexto es la raíz del repo, no seed/
docker compose up -d
curl -s localhost:8010/healthz
```

El nginx del droplet apunta a `127.0.0.1:8010` con esquema **`http`**: TLS termina en el proxy y hacia atrás va plano por loopback. Público en `https` con Let's Encrypt y Force SSL. Esto último no es cosmético, la página usa `navigator.clipboard` para el botón de copiar y esa API solo existe en contexto seguro.

Ambos contenedores usan `network_mode: bridge` en vez de crear una red propia. Crear una red reescribe reglas de iptables, y en este droplet ya conviven once redes con CrowdSec y npmplus tocando las suyas.

### Lo que no se deduce leyendo el YAML

| Ajuste | Por qué |
|---|---|
| `image: easytier/easytier:v2.6.4` | Versión fijada, jamás `latest`. Una actualización sorpresa del motor cambia el comportamiento de la red de todo el grupo sin que nadie lo haya pedido |
| `--disable-upnp true` | El motor mapea puertos por defecto. Acá no hay router que tocar, y es una invariante del producto |
| `--stun-servers` y `--stun-servers-v6` vacíos | STUN sirve para descubrir el propio NAT, y este nodo tiene IP pública directa. Con los valores por defecto, el droplet **estaba mandando tráfico saliente a servidores STUN de terceros**. Detectado en los logs al desplegar |
| `--no-tun true` | El seed presenta peers, no necesita interfaz virtual. Así el contenedor no pide `NET_ADMIN` ni `/dev/net/tun` |
| `--rpc-portal 127.0.0.1:15888` | Es el panel de control del motor. Queda en el loopback del contenedor y no se publica, o sea solo se alcanza por `docker exec` |
| Sin `--network-name` ni `--network-secret` | **El seed no se une a ninguna sala.** Es lo que garantiza que jamás vea el secreto de una red |
| El registro lee `peer list-foreign` por RPC | De ahí sale el contador de miembros, sin cooperación del host y sin unirse a nada. El mismo JSON confirma que ahí **no hay hostnames**: el nick viaja dentro de la red cifrada. Verificado contra la v2.6.4 con tres clientes en dos redes |
| El registro vive en memoria, con TTL | Sin base de datos y sin disco. Muere con la sala, salvo la llave fijada del host, que dura semanas para que reabrir con el mismo invite ID siga siendo suyo |
| Límite de tasa en `/api` | 40 bits de invite ID son enumerables sin él. Es la defensa que reemplazó a los 60 bits del diseño anterior, ver decisión 24 |
| `cpu_period` y `cpu_quota` explícitos | `cpus: 0.5` se acepta sin error en Compose v2.17 y **no llega al contenedor**: `docker inspect` mostraba `CpuQuota: 0`. Verificado tras el primer despliegue |

Checklist del droplet:

1. **Cloud Firewall de DigitalOcean:** 11010 TCP y UDP entrantes. Verificado alcanzable desde una máquina externa. Ningún otro puerto nuevo.
2. **Semilla en el cliente:** compilar la **Reserved IP**, nunca la IP pública directa del droplet. La reservada se mueve de máquina sin releasear el cliente. Pendiente confirmar cuál de las dos es la reservada.
3. **DNS opcional:** un registro tipo `_seeds.kanpachi.dev` como fuente primaria de semillas, con la IP compilada de respaldo. Dos fuentes siempre.
4. **Actualización manual y deliberada:** subir la versión fijada en el compose, luego `docker compose up -d`. Nada de `pull` a `latest`.
5. **Vigilancia:** una mirada mensual al consumo de transferencia en el panel de DO. El plan incluye 4000 GiB salientes al mes, el rendezvous consume kilobytes, cualquier número grande delata relay intensivo.
6. **Convive con producción.** El droplet corre Vaultwarden, Logto, el blog y varias bases de datos, con el disco al 87% y poca RAM libre. Por eso los límites de memoria y CPU no son opcionales, y por eso el seed vive en su propia red de contenedores sin ver a los demás.
7. **Endurecimiento futuro:** con público, limitar qué redes puede relevar con `--relay-network-whitelist`, poner techo con `--foreign-relay-bps-limit`, y mover el relay de datos a un VPS dedicado.

## Diagnóstico cuando algo falla

Botón **Copiar reporte** en la UI. Genera texto sin datos sensibles: versión, Windows, tipo de NAT, UDP bloqueado o no, RTT a semillas, estado de cada peer. Se pega en el grupo y quien ayuda ve el problema sin veinte preguntas de ida y vuelta.

Lectura rápida:

| Síntoma | Causa probable |
|---|---|
| Peer en ámbar (relay) con lag | NAT simétrico o CGNAT en una de las puntas. El juego funciona, con más latencia |
| "Sin conexión con el servidor de encuentro" | Droplet caído o 11010 bloqueado. Revisar el contenedor y el Cloud Firewall |
| Sala vacía tras pegar el código | Código mal copiado, o versiones de esquema distintas entre clientes |
| Todo en verde y el juego no ve la partida | Perfil del juego: falta `lan_discovery`, falta `broadcast_route`, o el juego usa puertos distintos a los del perfil |
| **Conecta y se cuelga al cargar el mundo** | **MTU.** Los paquetes chicos pasan y los grandes desaparecen en silencio. Típico en PPPoE (1492) o móvil. `Diagnostics` muestra el MTU efectivo |
| **Ayer funcionaba y hoy no, sin cambiar nada** | Windows revirtió métrica o categoría en un evento de identificación de red y el servicio no estaba corriendo para reaplicar |
| **El juego sale por la LAN física en vez de la virtual** | Métrica del adaptador revertida. Debe ser 1 en IPv4 con `AutomaticMetric` desactivado |
| **Nada resuelve y el usuario tiene otra VPN** | Conflicto de rango. `Diagnostics` reporta el `/24` elegido, revisar colisión con `100.64.0.0/10` |
| **El instalador desaparece al descargarlo** | Falso positivo de Defender sobre un binario Go sin firmar, ver `07-futuro.md` |
