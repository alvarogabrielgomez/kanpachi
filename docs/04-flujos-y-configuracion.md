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
8. **Y si se olvida de salir, se cierra igual.** Si el host desaparece veinte minutos, o si el túnel no vuelve en diez, Kanpachi sale de la sala por su cuenta, cierra los puertos y dice por qué en la pantalla de inicio. Cada máquina lo decide sola, sin coordinarse con nadie. Ver decisiones 20 y 26.
9. La próxima vez, "volver a la última sala" entra con un click. El código guardado sigue sirviendo incluso si el host lo renovó, porque el host se lo reparte a los que están dentro.

Lo que nunca ve: una terminal, un archivo de configuración, una pregunta del firewall de Windows, una cuenta.

## El flujo del host

1. Abre Kanpachi, botón **Crear sala**. Pide un nombre para la sala, y nada más: **crear no pide juego**, porque la sala es independiente del juego activo. Nace con red cifrada y cero puertos abiertos, que es un estado válido. Ver decisión 20.
2. Ya dentro, elige el juego. Arriba aparecen los detectados como instalados, abajo la biblioteca completa del catálogo con buscador. Si la detección no encontró el juego, se elige de la biblioteca y funciona igual. Kanpachi abre los puertos del perfil, solo en la interfaz virtual, solo hacia los miembros presentes. Cambiar de juego después no toca la sala: nadie se reconecta ni vuelve a pegar un código.
3. Copia el código con un click y lo pega en Telegram.
4. Arranca el servidor del juego como siempre: el dedicado de Zomboid, "Open to LAN" en Minecraft, lo que el juego pida.
5. La UI dice en texto plano qué está expuesto: "Abierto solo dentro de Kanpachi: 16261-16262 UDP, visible para 4 personas. Tu router sigue cerrado".
6. Si el instalador del juego dejó una regla que lo hace visible en la red de casa, aparece el aviso con la opción de desactivarla mientras dure la sala. Se restaura sola al salir.
7. Al salir de la sala, los puertos se cierran, las reglas ajenas suspendidas vuelven a su estado previo y la interfaz vuelve a quedar en cuarentena, sin ninguna regla de permiso.
8. **Si la máquina se apaga de golpe**, la sala queda sin cerrar. Al volver a abrir Kanpachi, la app lo detecta y pregunta si reabrirla: vuelve con el mismo código, la misma red y el mismo juego, y quien siga esperando reconecta solo. Nunca reabre sola. Ver decisión 2.

**Nada de esto toca el router.** Todas las conexiones se inician desde adentro hacia afuera, por eso el NAT deja pasar la respuesta sin reenvío de puertos ni UPnP. Nadie escucha en la IP pública de nadie.

Nota de rol: "host" es quien corre el servidor del juego. Cualquier miembro puede crear la sala, las aperturas del perfil aplican en la máquina que declara hospedar.

## Qué hace el instalador, paso a paso

1. Manifiesto `requireAdministrator`: el único UAC de la vida del producto.
2. Copia a `Program Files\Kanpachi\`: daemon, UI, `wintun.dll`, y `builtin.json`, que va suelto al lado del ejecutable del daemon y no en un subdirectorio.
3. Crea `ProgramData\Kanpachi\` con ACL: escritura solo SYSTEM y Administradores.
4. Registra el servicio `kanpachi-daemon`, arranque automático retrasado.
5. Política de recuperación del servicio: reiniciar a los 5 s, 10 s, 30 s.

   **Y le concede al usuario interactivo `SERVICE_START`, `SERVICE_STOP` y `SERVICE_QUERY_STATUS` sobre este servicio**, con `sc sdset`. Sin esa concesión, hacer doble clic en el acceso directo con Kanpachi cerrado pediría UAC, y el producto promete un solo UAC en toda su vida. Es una concesión mínima y acotada: el usuario puede arrancar y detener este servicio, nada más, y no gana ningún permiso sobre los demás del sistema.

   Lo que **no** se concede es `SERVICE_CHANGE_CONFIG`. El ajuste de "abrir Kanpachi con Windows" cambia el tipo de arranque entre `AUTO_START` y `DEMAND_START`, y eso lo hace el daemon consigo mismo cuando la UI se lo pide por el pipe. El daemon ya es SYSTEM, así que no hay que ampliarle permisos a nadie para tener ese interruptor.
6. Crea el adaptador Wintun `kanpachi0`, fija su categoría de red en **Privada** y escribe `Category=1` en su perfil del registro. Hacerlo aquí evita el diálogo de "¿quieres que este equipo sea detectable?" a mitad de una partida.
7. Fija la métrica del adaptador: IPv4 en 1, IPv6 en 20, `AutomaticMetric` desactivado en ambas pilas.
8. **La cuarentena de base ya no es paso del instalador.** La escribe el DAEMON en cada arranque, con el grupo `Kanpachi-base` y en los tres perfiles: bloqueo de los puertos prohibidos **en las dos direcciones**, sin acotar por dirección ni por adaptador. **No es un deny-all**, y no puede serlo: los bloqueos ganan sobre los permisos sin desempate por especificidad, así que un bloqueo total taparía las reglas del juego activo que crea el propio daemon. Ver decisión 4.

   **Por qué la cuarentena no se acota.** Este paso decía "sobre la IP del adaptador" y era imposible de cumplir: la `/24` de la red se elige **por sala y en tiempo de ejecución**, contra las redes que ya tiene la máquina, así que nadie la conoce de antemano. La razón de fondo es más fuerte que la mecánica, y ya está escrita en `core/domain/policy.go`: **un bloqueo acotado que deja de casar ABRE.** Un permiso acotado que deja de casar cierra, que es el lado correcto de fallar. Por eso el alcance por interfaz va solo en los permisos que crea el daemon, jamás en los bloqueos.

   **El puerto de las reglas es siempre el LOCAL, en las dos direcciones**, y de eso depende que la cuarentena no rompa la máquina. Entrante con puerto local 445 es "nadie llega a MI compartir archivos", que es la protección. Saliente con puerto local 445 cierra ese mismo servicio por el otro lado. Lo que NO hace: **impedir que esta PC sea CLIENTE**. Montar un disco de red, entrar por Escritorio remoto a otra máquina o usar git por SSH salen de un puerto local efímero hacia el 445, el 3389 o el 22 del OTRO, así que ninguna de estas reglas los toca. Bloquear por puerto remoto sí los rompería, y para siempre, porque la cuarentena sigue puesta con Kanpachi apagado.
9. Genera el token de la API local en ProgramData.
10. **Registra el manejador de `kanpachi://`, y le pasa el enlace.** La clave `HKLM\SOFTWARE\Classes\kanpachi` apunta a `kanpachid.exe --show "%1"`. El `"%1"` es el enlace que abrió el navegador, y hasta ahora no estaba: el manejador abría Kanpachi y el código había que pegarlo a mano. Adónde va desde ahí, y por qué al daemon y no a la interfaz, está en la sección del enlace profundo de `03-arquitectura.md`.
11. Accesos directos en Menú Inicio y escritorio, **apuntando al daemon con el parámetro `--show`, y no a la UI**. El daemon es lo que Kanpachi es; la UI son sus mandos. Ver el modelo de procesos en `03`: quien lanza la UI es siempre el daemon, así que un acceso directo a la UI podría dejar mandos abiertos sin nada detrás.

    El parámetro no es adorno. `kanpachid.exe` a secas es lo que arranca el Administrador de servicios; `--show` es lo que le pide a ese servicio que arranque y además enseñe la ventana. Mismo binario, papeles distintos.
12. Arranca el servicio con `--show`, por el paso del acceso directo y no a mano. El servicio lanza la UI con ventana, y con ella aparece el icono de la bandeja. Arrancarlo por las dos vías dejaría un daemon corriendo en silencio y un segundo intento de arranque que no hace nada.

**Ninguno de los pasos 6 a 8 es definitivo.** Windows revierte la métrica, la categoría y las rutas en cada evento de identificación de red, que se dispara al cambiar una IP, conectar o desconectar un adaptador, o en eventos de DHCP. Por eso el servicio se suscribe al Event ID 10000 de `Microsoft-Windows-NetworkProfile/Operational` y reaplica todo cada vez. El instalador solo deja el estado inicial correcto para que la primera sesión funcione sin esperar un evento.

Distribución silenciosa para el grupo: `kanpachi-setup.exe /VERYSILENT /NORESTART`.

### Dónde está esto escrito, y qué está medido

Son dos piezas y su estado es distinto:

- **La carga**, `scripts/preparar-carga.ps1`. Compila el daemon con `-trimpath` y `-H windowsgui`, la interfaz en release con su bundle entero, copia `builtin.json`, `Packet.dll`, `wintun.dll` y `WinDivert64.sys`, trae el motor del otro repositorio, y deja un `SHA256SUMS`. **Medido**: 21 ficheros, 72 MB, antes de que entrara el `.sys`.
- **El instalador**, `installer/kanpachi.iss`, para Inno Setup 6. **Escrito y sin medir**: en la máquina de desarrollo no hay Inno Setup, así que nunca se compiló ni se ejecutó. El criterio de aceptación sigue siendo el de arriba, instalar y desinstalar veinte veces en una VM sin dejar rastro.
- **La publicación**, `.github/workflows/release.yml`. Es quien corre las dos piezas de arriba de verdad, en un runner de Windows, con Inno Setup instalado ahí mismo.

Nada de `-ldflags "-s -w"`: quitar los símbolos dispara falsos positivos de Defender sobre binarios de Go, y el binario que se firma tiene que ser el que se probó. Mismo criterio que `release-seed.yml`.

### Dos rutas que dejaron de estar escritas a mano

`WinDivert64.sys` no se copiaba. Este documento lo lista dentro del directorio de instalación desde siempre y la carpeta portable sí lo copiaba, así que de las dos formas de entregar Kanpachi la que se empaqueta era la única a la que le faltaba un fichero. Nadie lo vio porque el instalador nunca se compiló.

Y los dos scripts tenían por defecto `C:\kt`, que es un directorio de trabajo de UNA máquina. No existe en ninguna otra ni en el runner del CI, de modo que quien clonara el repositorio se llevaba una carga escrita en una ruta que no significa nada. Ahora los defaults salen de la ubicación del script: `dist\carga` dentro del repositorio, y el motor en `..\kanpachi-engine\target\release\`.

## Publicar una versión

Se publica **por tag y solo por tag**. No hay CI en cada push ni en cada pull request, y la razón no es ahorrar minutos: lo que se publica sale de un tag, y cada workflow de publicación corre los tests que gobiernan lo que publica. Un job por push repetiría eso sobre commits que nadie va a publicar. Los chequeos completos siguen existiendo en `ci.yml`, a mano.

| Tag | Qué publica | Qué corre antes |
|---|---|---|
| `v*` | `kanpachi-setup.exe` y su `SHA256SUMS` | `./core/...` y `./internal/arch/...` |
| `seed-v*` | `kanpseed` para amd64 y arm64, `index.html`, y sus sumas | `./core/...`, `./registry/...` y `./internal/arch/...` |

`internal/arch` entra en los dos porque los dos publican algo que esos guardianes atan: el cliente y la página comparten el alfabeto del invite ID y la forma de la URL, escritos dos veces en dos lenguajes.

**El nombre del instalador no lleva la versión.** `kanpachi-setup.exe`, a secas, y eso es lo que hace que `releases/latest/download/kanpachi-setup.exe` sea una URL permanente: GitHub la redirige a la publicación más nueva, así que la página de descarga se actualiza sola al publicar un tag. Con el nombre versionado habría que editar la página en cada publicación, y la página que se edita a mano es la que se queda vieja. La versión viaja dentro del ejecutable, en su `VersionInfo`, y en el título de la publicación.

**El motor se compila dentro del workflow**, desde su propio repositorio. No se baja de una publicación suya porque todavía no tiene ninguna: ese repositorio solo compila en cada push. El ref con el que se compiló queda escrito en el cuerpo de la publicación, y eso es lo que hace reconstruible un instalador: sin él, "la versión 0.1.0" no dice con qué motor se armó.

**Sin firmar todavía.** Windows enseña el aviso de SmartScreen, y el cuerpo de la publicación lo dice con las palabras exactas que hay que pulsar, en vez de disimularlo. La vía elegida es SignPath Foundation, que firma gratis proyectos de código abierto; qué falta para poder solicitarlo está en `07-futuro.md`.

**El job de la interfaz de `ci.yml` falla hoy.** `ui/test` referencia `FakeSessionRepository`, que salió de `lib/`, así que ni `flutter analyze` sobre `test/` ni `flutter test` compilan. Por eso la publicación no lo llama: bloquearía toda entrega. Se deja declarado y sin disparador automático en vez de recortarle el alcance, porque bajar el listón para que pase es como se pierde `message_lockstep_test.dart`, que es el guardián que ata los enums de las dos puntas del cable.

## La carpeta portable, y el script que la arma

Hay una segunda forma de repartir Kanpachi además del instalador: una carpeta que se copia y funciona, sin nada que registrar. Es lo que cabe en un ZIP y lo que se lleva una llave USB. Qué la define y qué cuesta está en `03-arquitectura.md`; acá está cómo se produce.

```
.\scripts\kanpachi-portable.ps1                     arma .\Kanpachi y lo arranca
.\scripts\kanpachi-portable.ps1 debug               daemon de consola a la vista, interfaz en debug
.\scripts\kanpachi-portable.ps1 -Salida D:\x -NoArrancar   solo armar, para comprimir
```

Una sola orden hace lo que antes eran seis pasos a mano: compilar el daemon, compilar la interfaz, copiar el catálogo y las DLL, traer el motor del otro repositorio, escribir el marcador y arrancarlo todo con los permisos que necesita. El modo por omisión es producción.

| | `prod` | `debug` |
|---|---|---|
| Interfaz | release | debug, compilada contra el pipe de consola |
| Daemon | daemon portable, sin consola, log a fichero | `--console` en una terminal elevada, log a la vista |
| Quién abre la interfaz | el daemon | el script |

**En `debug` el daemon va por `cmd.exe /k` y no directo**, y hace falta: el binario está enlazado con `-H windowsgui`, así que no crea consola propia y lo que hace es engancharse a la del padre. Arrancado con elevación no hay padre con consola, y todo el log iría a ningún sitio, que es justo lo que ese modo existe para evitar.

**En `debug` la interfaz la arranca el script y no el daemon**, porque el modo consola no hospeda la interfaz a propósito. Quien usa `--console` tiene una terminal delante; levantarle una ventana en cada arranque taparía el caso que el producto de verdad tiene que resolver, que es el daemon lanzándola él.

Lo que el script hace antes de compilar y conviene saber: **detiene lo que estuviera corriendo de ESA carpeta**, filtrando por ruta y no por nombre, para no tumbar un Kanpachi instalado que no tiene nada que ver. Sin eso, Windows tiene el `.exe` bloqueado y `go build` falla con un acceso denegado que no menciona nada de esto. Un daemon portable corre elevado, así que detenerlo desde una terminal sin elevar no se puede: el script lo dice con esas palabras en vez de dejar el fallo de compilación a secas.

Y **conserva `kanpachi-data\`** entre compilaciones, salvo con `-Limpio`. Ahí dentro está la llave de esta instalación, y tirarla en cada build convertiría cada compilación en un equipo nuevo para quien ya jugó contigo.

## Modo desarrollo

El daemon corre como aplicación de consola, sin reinstalar el servicio. **Exige una consola elevada**, por dos motivos que se comprobaron a mano y no se dedujeron: el nombre del pipe vive bajo `ProtectedPrefix\Administrators`, que Windows no deja crear a un proceso sin elevar, y aceptar una conexión exige crear la instancia siguiente del pipe, cosa que el descriptor solo permite a SYSTEM y a los administradores.

```
.\scripts\preparar-stage.ps1
C:\kt\stage\kanpachid.exe --console -data C:\ruta\a\datos
C:\kt\stage\kanpctl.exe -data C:\ruta\a\datos status
C:\kt\stage\kanpctl.exe -data C:\ruta\a\datos -no-token status
```

`preparar-stage.ps1` sigue siendo el banco de pruebas con las sondas dentro, y `kanpachi-portable.ps1 debug` es lo otro: la carpeta que se reparte, compilada en depuración. Para medir un adaptador suelto sirve el stage; para ver el producto entero funcionando, la carpeta portable.

**`go run` no alcanza para nada que abra una sala**, y conviene saberlo antes de
perder una tarde: el daemon busca el motor y el catálogo al lado de su propio
ejecutable, y bajo `go run` ese sitio es un directorio temporal de compilación.
Arranca igual, avisa de que no hay catálogo, y al levantar la red falla sin
decir que el motor no estaba donde miró. `preparar-stage.ps1` deja los binarios,
`builtin.json` y las DLL juntos, que es la única forma de correr en desarrollo
lo mismo que se instala. El motor viene del otro repositorio y se copia a mano;
el script avisa cuando falta y no falla por eso.

El daemon imprime el nombre del pipe y el token al arrancar. La segunda llamada tiene que ser rechazada, y al salir con Ctrl+C el archivo `api.token` desaparece: rota una vez por vida del proceso, así que uno que le sobreviva no abre nada y solo sería un secreto muerto en disco.

**El modo consola usa otro nombre de pipe**, y eso no es cosmético: con el mismo, un proceso sin privilegios ocuparía el nombre de producción arrancando nuestro propio binario con `--console`, que es el squatting sin escribir un okupa. La bandera `--pipe` permite un nombre cualquiera y solo se lee en modo consola, para poder ejercer el saludo y los topes sin un UAC por cada prueba.

**El directorio de datos no lo crea el daemon.** Lo crea el instalador con su ACL, y esa ACL es la mitad de la protección del token; crearlo por accidente desde el daemon la perdería en silencio. Para probar a mano hay que crearlo antes o pasar `-data`. La única excepción es la carpeta portable, donde no hay instalador que lo cree y la alternativa sería que no arrancara nunca; queda dicho en `03-arquitectura.md` con lo que eso cuesta.

**Lo que el instalador jamás hace:** agregar exclusiones de Windows Defender, ni habilitar los grupos de reglas de Detección de redes o Compartir archivos e impresoras. Lo primero es lo que hace el malware, y si el binario necesitara una exclusión el problema sería el binario. Lo segundo abriría SMB en la LAN doméstica del usuario, porque esos grupos se habilitan por perfil de firewall y no por adaptador.

## Qué queda instalado

| Artefacto | Ubicación |
|---|---|
| Binarios y wintun.dll | `Program Files\Kanpachi\` |
| Servicio | `kanpachi-daemon`, automático retrasado |
| Adaptador de red | `kanpachi0` (Wintun), categoría Privada |
| Reglas de firewall | Grupo "Kanpachi" para la sala y "Kanpachi-base" para la cuarentena, los tres perfiles |
| Datos y logs | `ProgramData\Kanpachi\` |

## Desinstalación

En orden: detener y borrar el servicio, purgar las reglas de **los dos grupos**, "Kanpachi" y "Kanpachi-base", eliminar el adaptador Wintun, borrar ProgramData, borrar Program Files. Criterio de calidad: instalar y desinstalar veinte veces seguidas en una VM sin dejar rastro.

**El desinstalador es el único que borra los dos.** Es la razón por la que conviene que los nombres se parezcan, y también la trampa: la comparación va por igualdad exacta contra cada uno, jamás por prefijo contra "Kanpachi", porque el mismo atajo escrito dentro del daemon borraría la cuarentena en cada arranque.

**Borrar la cuarentena es requisito de producto, no cortesía.** Dejar bloqueos explícitos sobre el 445 y el 3389 en toda la máquina con Kanpachi ya borrado deja al usuario sin compartir archivos ni Escritorio remoto, sin causa visible y sin nada que culpar. El desinstalador todavía no existe: está en `07-futuro.md` con su disparador.

### Hasta que exista, el comando de soporte

Consola de PowerShell **elevada**:

```powershell
Get-NetFirewallRule -Group 'Kanpachi-base' | Remove-NetFirewallRule
```

Igualdad exacta del grupo, **jamás un comodín**. `Kanpachi` es prefijo de `Kanpachi-base`, así que un `-Group 'Kanpachi*'` se lleva también las reglas de la sala, y un `-DisplayName` parcial se lleva lo que ni siquiera es de Kanpachi.

Ese comando NO es para el uso normal: el daemon vuelve a escribir la cuarentena en el arranque siguiente, que es exactamente lo que tiene que hacer. Sirve para desinstalar a mano y para diagnosticar, con el servicio ya detenido.

## Configuración del droplet (kanpachi-seed)

Contexto: droplet DigitalOcean NYC3 ya existente, Ubuntu con systemd, con Reserved IP. El seed convive con las cargas de Accentio, que sí viven en Docker.

### Una sola ejecución

```bash
curl -fsSL https://raw.githubusercontent.com/accentiostudios/kanpachi/main/seed/install.sh | sudo sh
```

Baja el binario, verifica su SHA256, y le cede el trabajo a `kanpseed init`, que elige puertos, coloca EasyTier, escribe los servicios y **imprime el puerto interno** que hay que poner en el proxy inverso.

Sin Docker. El razonamiento está en `03-arquitectura.md`: se implementó con contenedores primero y se descartó por evidencia.

```
kanpseed-engine.service     easytier-core, 11010 TCP y UDP
                            rpc en 127.0.0.1:15888
kanpseed-registry.service   kanpseed serve, sirve / y /api
                            127.0.0.1:<el puerto que eligió el instalador>
```

### El CLI

Un solo binario, `kanpseed`. Se llama así y no `kanpachi` porque ese nombre queda reservado para el cliente de terminal de Linux, que entrará y creará salas y es otra cosa.

| Comando | Para qué |
|---|---|
| `kanpseed init` | instala y configura todo. Idempotente: repetirlo conserva los puertos |
| `kanpseed doctor` | revisa archivos, servicios, puertos, RPC y salud, y dice qué hacer con cada fallo |
| `kanpseed config` | muestra o cambia puertos y dominio, reescribe las units y reinicia |
| `kanpseed nginx` | repite el bloque del proxy, para no tener que recordar el puerto |
| `kanpseed uninstall` | deja la máquina como estaba |
| `kanpseed serve` | lo arranca systemd. No hace falta a mano |

### El puerto interno se elige y se imprime

`init` busca el primer puerto libre desde el 8010 comprobándolo **con un bind de verdad**, no leyendo una tabla. Si el 8010 está tomado sigue por el 8011, y así hasta el 8099. Al terminar lo imprime en una caja, porque es el dato que hay que llevar al proxy inverso.

Lo que esa comprobación **no** puede saber: un puerto libre ahora mismo puede estar reservado en la configuración de otro servicio que esté apagado. Por eso se imprime en vez de darse por obvio, y por eso `kanpseed nginx` lo repite cuando haga falta.

El puerto del motor, el 11010, es distinto: **va compilado en el cliente**, así que moverlo obliga a publicar una versión nueva. Si está ocupado, `init` avisa y pregunta antes de seguir.

### El proxy inverso

Esquema **`http`** hacia `127.0.0.1:<puerto>`: el TLS termina en el proxy y hacia atrás va plano por loopback. Público en `https` con Let's Encrypt y **Force SSL**, que no es cosmético: la página usa `navigator.clipboard` para el botón de copiar y esa API solo existe en contexto seguro.

`X-Forwarded-For` tampoco es opcional. El límite de tasa del registro cuenta por IP, y sin esa cabecera todas las visitas parecen una sola, o sea que el límite se convertiría en una denegación de servicio contra todo el mundo a la vez.

### Lo que systemd aporta y hay que no deshacer

- **`WatchdogSec=30s` con `sd_notify`.** `Restart=always` solo actúa cuando el proceso muere; el latido cubre el proceso vivo pero colgado. Verificado con un `SIGSTOP`: systemd lo reinició a los 29 segundos.
- **`BindsTo=` del registro hacia el motor.** Si el motor se detiene, el registro se detiene con él y vuelve con él, en vez de quedarse sirviendo páginas sin contador para siempre.
- **`DynamicUser=yes`, `ProtectSystem=strict`, `CapabilityBoundingSet=` vacío.** Ninguno de los dos procesos escribe en disco ni necesita capacidades. El motor escucha en un puerto público y habla con desconocidos, así que es el que más lo merece.
- **`MemoryMax` y `CPUQuota`.** El droplet comparte casa con Vaultwarden, Logto y varias bases de datos.

### Lo que el motor lleva, y por qué

| Ajuste | Por qué |
|---|---|
| Versión fijada, jamás `latest` | Una actualización sorpresa del motor cambia el comportamiento de la red de todo el grupo sin que nadie lo haya pedido. El instalador verifica su SHA256 antes de colocarlo |
| `--disable-upnp true` | El motor mapea puertos por defecto. Acá no hay router que tocar, y es una invariante del producto |
| `--stun-servers` y `--stun-servers-v6` vacíos | STUN sirve para descubrir el propio NAT, y este nodo tiene IP pública directa. Con los valores por defecto, el droplet **estaba mandando tráfico saliente a servidores STUN de terceros**. Detectado en los logs al desplegar |
| `--no-tun true` | El seed presenta peers, no necesita interfaz virtual. Así el servicio no requiere `NET_ADMIN` y puede correr con capacidades vacías |
| `--rpc-portal 127.0.0.1` | Es el panel de control del motor. Solo loopback, y `doctor` comprueba que no responda desde fuera |
| Sin `--network-name` ni `--network-secret` | **El seed no se une a ninguna sala.** Es lo que garantiza que jamás vea el secreto de una red |
| El registro lee `peer list-foreign` | De ahí sale el contador de miembros, sin cooperación del host. El mismo JSON confirma que ahí **no hay hostnames**: el nick viaja dentro de la red cifrada |
| Límite de tasa en `/api` | 40 bits de invite ID son enumerables sin él. Es la defensa que reemplazó a los 60 bits del diseño anterior, ver decisión 24 |

Checklist del droplet:

1. **Cloud Firewall de DigitalOcean:** 11010 TCP y UDP entrantes. Verificado alcanzable desde una máquina externa. Ningún otro puerto nuevo.
2. **Semilla en el cliente:** compilar la **Reserved IP**, nunca la IP pública directa del droplet. La reservada se mueve de máquina sin releasear el cliente. Pendiente confirmar cuál de las dos es la reservada.
3. **DNS opcional:** un registro tipo `_seeds.kanpachi.dev` como fuente primaria de semillas, con la IP compilada de respaldo. Dos fuentes siempre.
4. **Actualización manual y deliberada:** subir la versión fijada en `registry/setup/easytier.go` con su SHA256 nuevo, publicar un release, y volver a ejecutar el instalador. Nada de `latest`.
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
