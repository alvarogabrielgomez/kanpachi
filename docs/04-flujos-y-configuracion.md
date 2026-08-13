# Flujos y configuración

## El flujo del que se une (el caso Santiago)

Objetivo: menos de 3 minutos desde recibir el link del instalador hasta estar dentro de la partida, con cero preguntas en el grupo.

1. Descarga `kanpachi-setup.exe` del link del grupo.
2. Siguiente, siguiente. Un solo UAC. La barra termina y Kanpachi abre solo.
3. La app ya muestra sus juegos detectados arriba, con la biblioteca completa un click más abajo.
4. Pega el código que le pasaron por Telegram: `A7K2-M9QX`. El campo acepta cualquier formato, con o sin guiones, en minúsculas o mayúsculas.

   **Antes de levantar nada se le pregunta al registro si ese código existe**, y sin respuesta suya no se entra. Los dos fallos posibles llegan en menos de un segundo y dicen cosas distintas: que ese código no existe, o sea que hay que pedir uno nuevo, y que el servidor de encuentro no responde, o sea que el código puede estar bien y hay que reintentar en un rato. Ver decisión 33.
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

   **Lo primero que hace es pedirle un código al registro, y sin él no hay sala.** Falla en el primer segundo, antes de levantar el motor y antes de escribir una sola regla, con el texto que dice que el servidor de encuentro no responde y que hay que probar en un rato. Los códigos los emite el registro: uno inventado en esta máquina no le sirve a nadie para entrar. Ver decisión 33. Si el registro ya estaba caído antes de pulsar, la portada lo venía avisando.
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
4. Registra o actualiza el servicio `kanpachi-daemon`, siempre apuntando al `kanpachid.exe` recién copiado en Program Files, como LocalSystem y con arranque automático retrasado. Antes de reemplazar archivos avisa de que eso cierra la sala, detiene ESE servicio y espera hasta 120 segundos; no busca ni detiene carpetas portables. Ver «Instalar encima cierra la sala» más abajo.
   Le escribe además su **descripción**, con `sc description`, que es la columna que alguien lee en `services.msc` cuando va a mirar qué son los servicios de su máquina. Va en inglés, igual que los nombres que los cuatro ejecutables muestran en el Administrador de tareas, y a diferencia del resto de los textos del producto.

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

### Instalar encima cierra la sala, y ahora lo avisa

Actualizar es reinstalar sobre lo que hay: mismo `AppId`, misma carpeta, ficheros con `ignoreversion` y un `sc config` que reimpone la ruta. No hay que desinstalar nada, y `ProgramData\Kanpachi` queda intacto.

Lo que sí pasa, y no se puede evitar, es que **detener el servicio cierra la sala para todos**: se cierran los puertos del juego, se revierten las reglas y el motor cae con el Job. No se reemplaza el binario de un daemon vivo.

Antes de tocar un solo fichero, `PrepareToInstall` lo pregunta con un diálogo que dice exactamente eso, y cancelar deja todo como estaba —«no se reemplazó ningún archivo y la sala sigue abierta». **Solo aparece si el servicio está CORRIENDO**: una instalación nueva, o una encima de un Kanpachi ya detenido, no lo ve. Un clic de más en el caso en que la advertencia no aplica es cómo una advertencia deja de leerse.

El diálogo de Inno que cierra la interfaz —el de `CloseApplications`, que está en `yes` por omisión y por eso no aparece escrito en el `.iss`— no cubre esto: nombra ficheros en uso, no dice que la sala se vaya a cerrar, y el daemon ni siquiera sale ahí porque a ése lo detiene el script antes.

Aceptado el diálogo, sigue lo de siempre: `sc stop` y hasta 120 segundos esperando el estado `STOPPED`, porque el daemon tarda lo que tarde en cerrar la sala y restaurar el firewall. Si no llega, el instalador aborta sin haber copiado nada.

**Y no existe autoactualización en el cliente.** El seed se actualiza solo con `kanpseed upgrade`; una PC con Kanpachi instalado no. Lo único que hace la app es avisar de que hay versión nueva y llevar a la página de descarga —ver `05-ui.md`—; instalarla es bajar el instalador y ejecutarlo. Es una asimetría deliberada mientras no haya firma de código: ver el punto 4 de `07-futuro.md`.

### Dónde está esto escrito, y qué está medido

Son dos piezas y su estado es distinto:

- **La carga**, `scripts/preparar-carga.ps1`. Compila el daemon con `-trimpath` y `-H windowsgui`, la interfaz en release con su bundle entero, copia `builtin.json`, `Packet.dll`, `wintun.dll` y `WinDivert64.sys`, trae el motor del otro repositorio, y deja un `SHA256SUMS`. **Medido**: 21 ficheros, 72 MB, antes de que entrara el `.sys`. Ojo con de dónde salen esos tres ficheros de `third_party\easytier`: están en `.gitignore` por tamaño, así que en un runner limpio no existen y el workflow los baja del release oficial de EasyTier antes de llamar a este script.
- **El instalador**, `installer/kanpachi.iss`, para Inno Setup 6. **Compilado y ejecutado de verdad.** La primera instalación pública, `v0.1.1`, encontró un servicio previo que apuntaba a `C:\kt\carga`: `sc create` devolvió que ya existía y el script ignoró el código, así que conservó la ruta y el arranque manual. Ahora `sc config` impone ruta, cuenta y tipo en cada instalación y cada comando obligatorio corta el setup si falla. El criterio completo sigue siendo instalar y desinstalar veinte veces en una VM sin dejar rastro.
- **La publicación**, `.github/workflows/release.yml`. Es quien corre las dos piezas de arriba de verdad, en un runner de Windows, con Inno Setup instalado ahí mismo.

Nada de `-ldflags "-s -w"`: quitar los símbolos dispara falsos positivos de Defender sobre binarios de Go, y el binario que se firma tiene que ser el que se probó. Mismo criterio que `release-seed.yml`.

### Dos rutas que dejaron de estar escritas a mano

`WinDivert64.sys` no se copiaba. Este documento lo lista dentro del directorio de instalación desde siempre y la carpeta portable sí lo copiaba, así que de las dos formas de entregar Kanpachi la que se empaqueta era la única a la que le faltaba un fichero. Nadie lo vio porque el instalador nunca se compiló.

Y los dos scripts tenían por defecto `C:\kt`, que es un directorio de trabajo de UNA máquina. No existe en ninguna otra ni en el runner del CI, de modo que quien clonara el repositorio se llevaba una carga escrita en una ruta que no significa nada. Ahora los defaults salen de la ubicación del script: `dist\carga` dentro del repositorio, y el motor en `..\kanpachi-engine\target\release\`.

## Publicar una versión

Se publica **por tag y solo por tag**. No hay CI en cada push ni en cada pull request, y la razón no es ahorrar minutos: lo que se publica sale de un tag, y cada workflow de publicación corre los tests que gobiernan lo que publica. Un job por push repetiría eso sobre commits que nadie va a publicar. Los chequeos completos siguen existiendo en `ci.yml`, a mano.

**Un solo tag, `v*`, corta la versión de las dos mitades del producto.** Los dos workflows escuchan lo mismo y escriben en el mismo release:

| Workflow | Qué publica | Qué corre antes |
|---|---|---|
| `release.yml` | `kanpachi-setup.exe`, `kanpachi-portable.exe` y `SHA256SUMS-windows` | `./core/...`, `./internal/arch/...`, `flutter analyze` y `flutter test` |
| `release-seed.yml` | `kanpseed` para amd64 y arm64, `index.html`, y `SHA256SUMS-seed-linux` | `./core/...`, `./registry/...` y `./internal/arch/...` |

`internal/arch` entra en los dos porque los dos publican algo que esos guardianes atan: el cliente y la página comparten el alfabeto del invite ID y la forma de la URL, escritos dos veces en dos lenguajes.

**El tag `seed-v*` ya no existe.** Publicaba el seed por su cuenta, con su propia numeración, y eso costaba dos cosas. Una: un release SIN `kanpachi-setup.exe` se llevaba el `latest`, y la URL permanente de la página de descarga quedaba apuntando a una publicación sin instalador. Dos: había que cruzar dos numeraciones para saber qué seed habla con qué cliente. Ahora el seed se publica aunque no haya cambiado nada, y ese release "de más" es lo que compra mirar un droplet, leer `v0.1.0` y saberlo.

**Cada carga trae su propio manifiesto de sumas**, `SHA256SUMS-windows` y `SHA256SUMS-seed-linux`. Con un solo nombre para las dos, el último workflow en terminar pisaba el archivo del otro y dejaba a `install.sh` verificando binarios que no aparecían en él.

El del seed se llamó `SHA256SUMS-linux` hasta que existió un cliente de Linux, y ese nombre le corresponde al cliente: es el que bajan desconocidos desde la página, mientras que el del seed lo baja quien se autohospeda. El cambio deja varado al seed que ya está desplegado, porque el nombre viaja como constante compilada dentro de su binario: ese seed pide `SHA256SUMS-linux`, recibe el manifiesto del cliente, no encuentra su propio nombre dentro y se niega a instalar. **Falla del lado seguro**, que es lo que tiene que hacer, y aun así hay que arreglarlo a mano: volver a correr `install.sh` en el droplet, que baja la versión nueva del script y ya trae el nombre nuevo. Una acción manual, una vez. El día que haya seeds de terceros el camino es otro: publicar el manifiesto del seed con los dos nombres, esperar a que se actualicen, y soltar el viejo en la siguiente.

**Los dos workflows corren en paralelo y ninguno espera al otro.** El de Linux tarda un minuto y el de Windows veinte, así que hay una ventana en la que el release existe sin instalador. Se acepta a conciencia: se cierra sola, y la alternativa era que veinte minutos de Windows retrasaran una carga de Linux que ya estaba lista.

**Un tag creado desde la web de GitHub no dispara `push`.** Medido el 2026-08-06: el tag `v0.1.0` se creó desde la interfaz de releases, su commit ya traía el workflow con `push: tags`, y la API de Actions no registró ninguna corrida. Un `git push` de un tag sí dispara. Por eso los dos workflows escuchan además `release: published`, que es por donde entra ese camino. La consecuencia que hay que tener presente: para un release de la web, GitHub ejecuta el workflow **del commit que apunta el tag**, así que etiquetar un commit anterior a este párrafo vuelve a no disparar nada.

### Actualizar un seed que ya está corriendo

`sudo kanpseed upgrade`, y `--check` para mirar sin instalar. Vuelve a bajar `install.sh` solo si hace falta instalar de cero.

Actualizar no es intercambiar el binario. El seed son cinco cosas que tienen que estar de acuerdo: el binario, la página que sirve, los binarios de EasyTier, las units de systemd y los procesos vivos. `upgrade` hace las cinco, en ese orden, y espera a que el registro responda antes de darse por bueno. Cambiar solo la primera deja una máquina que anuncia una versión y se comporta como otra, y ese desajuste no da error: da un droplet que "va raro".

**Y la segunda mitad la hace el binario NUEVO, que es lo que faltaba.** El pin de EasyTier y el texto de las units son constantes compiladas, así que el proceso que corre `upgrade` —lanzado antes del reemplazo— las lleva de la versión anterior: haciéndolo él mismo escribía la configuración vieja sobre el binario nuevo. Un arreglo que vive en la unit no llegaba nunca en su propia versión.

**Costó un despliegue, el 2026-08-07.** Se agregó `--secure-mode true` a la unit del motor, se publicó, se corrió `upgrade` en el droplet, y la unit quedó sin la bandera: el seed anunciaba la versión con el arreglo y seguía rechazando a todos los invitados. El apaño fue `kanpseed init` a mano, que sí corre con el binario nuevo, y de ahí salió la creencia de que actualizar el seed eran dos comandos.

Ahora `upgrade` reemplaza el binario y la página, y **le cede el resto a `kanpseed reconfigure` ejecutando el binario que acaba de poner**. Un comando, como decía la documentación que era. `reconfigure` queda además disponible a mano: no instala nada ni pregunta nada, lee la configuración que ya hay y devuelve las units a lo que esta versión dice, útil si alguien editó una a mano.

**`kanpseed config` sin argumentos NO reescribe nada, solo muestra.** Reescribe cuando se le cambia un puerto o el dominio, y aun así solo si el valor cambia de verdad. El comando que devuelve las units a lo que dice el binario es `reconfigure`, y hay que decirlo porque la confusión lleva a creer que un despliegue quedó aplicado cuando no se tocó una línea.

El pin de EasyTier viaja dentro del binario nuevo, así que subirlo en un release llega al droplet por esta vía. Para que eso funcione, `/usr/local/lib/kanpachi/easytier.version` guarda qué versión quedó instalada: antes "ya están" se contestaba mirando solo si los archivos existían, de modo que subir el pin no reemplazaba nada.

**El nombre del instalador no lleva la versión.** `kanpachi-setup.exe`, a secas, y eso es lo que hace que `releases/latest/download/kanpachi-setup.exe` sea una URL permanente: GitHub la redirige a la publicación más nueva, así que la página de descarga se actualiza sola al publicar un tag. Con el nombre versionado habría que editar la página en cada publicación, y la página que se edita a mano es la que se queda vieja. La versión viaja dentro del ejecutable, en su `VersionInfo`, y en el título de la publicación. Vale igual para `kanpachi-portable.exe`.

**El portable sale del MISMO tag y del mismo trabajo que el instalador**, con `scripts\build_portable_bundle.ps1` y el motor que se acaba de compilar unos pasos antes, pasado explícito. Son dos formas de repartir el mismo producto: publicarlas por separado dejaría a un amigo con el portable y a otro con el instalado en versiones distintas sin nada que lo diga, y el protocolo entre el daemon y el motor decodifica estricto, así que esa diferencia no es cosmética.

Lo que cuesta y se dice: la interfaz de Flutter se compila **dos veces en release** dentro del mismo trabajo, una para la carga del instalador y otra para el bundle. Son un par de minutos, y a cambio las dos salidas usan la misma receta que se usa a mano, sin una tercera lista de ficheros que mantener sincronizada. La comprobación de "el motor no es más viejo que su código" no corre en el runner, porque busca el repositorio del motor como hermano de este y ahí está en `motor\`; no hace falta, porque se compiló en ese mismo trabajo.

**El motor se compila dentro del workflow**, desde su propio repositorio. No se baja de una publicación suya porque todavía no tiene ninguna: ese repositorio solo compila en cada push. El ref con el que se compiló queda escrito en el cuerpo de la publicación, y eso es lo que hace reconstruible un instalador: sin él, "la versión 0.1.0" no dice con qué motor se armó.

**El cuerpo de la publicación empieza citando el tramo del `CHANGELOG.md` de esa versión**, sacado del archivo y jamás escrito a mano en el release. Escribirlo dos veces es tenerlo distinto dos veces, y el que la gente lee sería el que nadie revisó. **Una versión sin sección en el changelog no se publica: el workflow falla ahí**, con el mensaje de qué falta y cómo se arregla. Es la misma regla de los documentos, con la máquina comprobándola: lo que se deja para el momento de etiquetar es lo que se olvida justo entonces. La corrida manual queda fuera, porque ahí la versión es `0.0.0` y no hay nada que citar. Cómo se escribe una entrada está en `CLAUDE.md`, y por eso el changelog va en inglés: su texto sale tal cual en el release.

**Sin firmar todavía.** Windows enseña el aviso de SmartScreen, y el cuerpo de la publicación lo dice con las palabras exactas que hay que pulsar, en vez de disimularlo. La vía elegida es SignPath Foundation, que firma gratis proyectos de código abierto; qué falta para poder solicitarlo está en `07-futuro.md`.

**La interfaz no tiene un repositorio alternativo para tests.** Los widgets y el cubit hablan con `PipeSessionRepository` y `DaemonClient` reales sobre un transporte de bytes en memoria; atraviesan saludo, codec, nombres de método y mapeo de respuestas sin necesitar un daemon ni Windows. `flutter analyze` y `flutter test` pasan completos y vuelven a bloquear el release si se rompe la costura, incluido `message_lockstep_test.dart`, que ata los enums de las dos puntas del cable.

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

En orden: detener y borrar el servicio, purgar las reglas de **los dos grupos**, "Kanpachi" y "Kanpachi-base", eliminar el adaptador Wintun, borrar ProgramData, borrar Program Files y borrar `Roaming AppData\Accentio Studios\Kanpachi\shared_preferences.json`. Este último no vive junto al bundle: es donde `shared_preferences_windows` guarda nickname, onboarding, tamaño de ventana y ajustes mediante el Application Support de Windows. Criterio de calidad: instalar y desinstalar veinte veces seguidas en una VM sin dejar rastro.

**El desinstalador es el único que borra los dos.** Es la razón por la que conviene que los nombres se parezcan, y también la trampa: la comparación va por igualdad exacta contra cada uno, jamás por prefijo contra "Kanpachi", porque el mismo atajo escrito dentro del daemon borraría la cuarentena en cada arranque.

**Borrar la cuarentena es requisito de producto, no cortesía.** Dejar bloqueos explícitos sobre el 445 y el 3389 en toda la máquina con Kanpachi ya borrado deja al usuario sin compartir archivos ni Escritorio remoto, sin causa visible y sin nada que culpar. Por eso el desinstalador ejecuta la limpieza con el daemon todavía en disco y solo después elimina el servicio y los binarios.

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

Un solo binario, `kanpseed`. Se llama así y no `kanpachi` porque ese nombre lo tiene el cliente de terminal de Linux, que entra y crea salas y es otra cosa. **Todo lo que imprime va en inglés**, como el README que se lee por `ssh`.

| Comando | Para qué |
|---|---|
| `kanpseed init` | instala y configura todo. Idempotente: repetirlo conserva los puertos |
| `kanpseed upgrade` | se pone en la última versión publicada y reinicia. `--check` solo mira. Un comando y no dos: la parte que depende del código nuevo la corre el binario nuevo |
| `kanpseed reconfigure` | reescribe las units como las quiere esta versión y reinicia. Lo llama `upgrade` por dentro. **No toca la página**, que la instalan `init` y `upgrade`: quien reemplace el binario a mano tiene que copiar `index.html` también, o queda un binario nuevo sirviendo una página vieja |
| `kanpseed doctor` | revisa archivos, servicios, puertos, RPC y salud, y dice qué hacer con cada fallo |
| `kanpseed config` | muestra o cambia puertos y dominio, reescribe las units y reinicia. Sin argumentos enseña además si hospedar pide password |
| `kanpseed password` | pide un password para HOSPEDAR en este seed. `--open` lo quita |
| `kanpseed nginx` | repite el bloque del proxy, para no tener que recordar el puerto |
| `kanpseed uninstall` | deja la máquina como estaba |
| `kanpseed serve` | lo arranca systemd. No hace falta a mano |

### Cerrar el seed, y las tres cosas que hay que saber antes

Ver la decisión 34 para el porqué, y `03-arquitectura.md` para dónde vive cada pieza. Lo de acá es lo que le pasa al operador.

**Entrar a una sala nunca pide nada.** El password es para abrir salas, publicar la tarjeta y renovar el código. Un invitado no se entera de que el seed está cerrado.

**El password queda atado al dominio configurado.** Va dentro del hash que manda el cliente, así que tiene que ser el nombre que la gente escribe de verdad. `kanpseed password` se niega si no hay dominio y dice cómo ponerlo. Quien alcance ese seed por otro nombre no va a poder hospedar en él.

**Cambiarlo bota a todos los hosts en el acto**, porque rota la clave que firma los tokens. Vuelven a entrar escribiendo el nuevo. El comando lo dice antes de preguntar, no después.

```
sudo kanpseed password          lo pide enmascarado, dos veces
sudo kanpseed password --open   lo quita, y con él mueren todos los tokens
```

**Enter con el campo vacío es lo mismo que `--open`**, y se dice en el propio prompt: es donde alguien expresa «no quiero password», y mandarlo a leer la ayuda para descubrir la bandera sería un callejón.

**No existe `--password`**, y no va a existir: cualquier usuario de la máquina lee `/proc/<pid>/cmdline`, y el shell guarda historial. Se teclea, o entra por la entrada estándar, que es la forma correcta de automatizarlo con un fichero 0600.

`init` lo ofrece al final, con «no» por defecto, porque abierto es el caso normal: un seed levantado para tres amigos no gana nada con un password, y el roce lo paga cada vez que alguien abre una sala.

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
- **`DynamicUser=yes`, `ProtectSystem=strict`, `CapabilityBoundingSet=` vacío.** Ninguno de los dos necesita capacidades, y el sistema entero les queda en solo lectura. El motor escucha en un puerto público y habla con desconocidos, así que es el que más lo merece.
- **`StateDirectory=kanpseed`, solo en el registro.** Es el único sitio donde escribe, y ahí guardan las salas su vida entre reinicios. Con `DynamicUser=yes` el directorio real vive bajo `/var/lib/private/kanpseed`: systemd lo crea, le ajusta el dueño en cada arranque y le pasa la ruta en `STATE_DIRECTORY`, que es de donde la lee `serve`. **Quitarlo devuelve el fallo de la decisión 33**, que es un reinicio del seed dejando fuera a todo invitado de toda sala abierta.
- **`MemoryMax` y `CPUQuota`.** El droplet comparte casa con Vaultwarden, Logto y varias bases de datos.

### Lo que el motor lleva, y por qué

| Ajuste | Por qué |
|---|---|
| Versión fijada, jamás `latest` | Una actualización sorpresa del motor cambia el comportamiento de la red de todo el grupo sin que nadie lo haya pedido. El instalador verifica su SHA256 antes de colocarlo |
| `--disable-upnp true` | El motor mapea puertos por defecto. Acá no hay router que tocar, y es una invariante del producto |
| `--stun-servers` y `--stun-servers-v6` vacíos | STUN sirve para descubrir el propio NAT, y este nodo tiene IP pública directa. Con los valores por defecto, el droplet **estaba mandando tráfico saliente a servidores STUN de terceros**. Detectado en los logs al desplegar |
| `--no-tun true` | El seed presenta peers, no necesita interfaz virtual. Así el servicio no requiere `NET_ADMIN` y puede correr con capacidades vacías |
| `--secure-mode true` | **Sin ella ninguna sala pasa de una persona.** Un invitado entra con credencial, y una credencial abre con handshake de Noise; el servidor solo atiende ese paquete si él mismo tiene modo seguro, y si no lo tiene contesta `unexpected packet type during handshake: 13` y cierra. Medido el 2026-08-07: 236 rechazos seguidos, y el usuario veía «el adaptador kanpachi0 no tomó la dirección en 30s», que es la consecuencia tres capas más abajo. Ver decisión 31 |
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

## Las herramientas de medición

Viven en `internal/`, no se distribuyen con el instalador y el producto no las puede importar. Son seis sondas de un solo asunto (`fwprobe`, `engineprobe`, `netcfgprobe`, `dirprobe`, `watchprobe`, `kanpctl`) más dos que se usan juntas y conviene leer como una sola cosa.

### `roomprobe`: la sala entera, sin daemon ni instalador

Levanta la sesión de verdad, con los dieciséis puertos cableados igual que `daemon/cmd/kanpachid` y el mismo supervisor. Ofrece un menú para crear sala, entrar, expulsar, cerrar, salir y volver a la última, y una vista que se redibuja sola con los miembros, los plazos y el canario.

**No abre puertos de juego y no lleva catálogo**, a propósito: lo que mide es la sala, o sea la red cifrada, el canal de control, la compuerta y los adaptadores.

**El log es el entregable.** `roomprobe.log` queda junto al ejecutable.

**Lo que roomprobe anotaba y el daemon no, se movió al daemon.** Una sonda que registra más que el producto miente sobre el producto: lo que se diagnostica con ella no se puede diagnosticar en la máquina de quien tiene el fallo. Así que los pasos del diario de cada operación viven en `core/usecase`, y las reglas de firewall salen **con su destinatario** desde el mismo sitio que las escribe. Los dos los tienen ahora las tres formas de correr Kanpachi: instalada, portable y roomprobe.

- **Los pasos del diario** son la misma narración que la pantalla enseña en "ver detalles", que antes no salía del proceso: el ingreso a una sala son doce pasos, y el último que aparece es dónde se atascó.
- **Las reglas con su destinatario**, y no solo cuántas. Un `reglas 1` es compatible con dos realidades opuestas; un `remotos []` en la regla de la sala es el fallo de la v0.1.6 de un vistazo.

Queda **una sola cosa que es de roomprobe y de nadie más**, y es por lo que sigue existiendo: un volcado a demanda, con la tecla `d`, que junta adaptadores leídos del sistema, miembros con su camino, huecos con `puesto` sí o no, NAT, RTT a los seeds y los plazos corriendo. Pide un teclado delante, así que no tiene forma en un servicio.

Se niega a arrancar con el servicio `kanpachi-daemon` vivo, salvo con `-force`: construir la sesión llama a `PurgeOwned`, o sea que le borra las reglas al daemon instalado con la sala de alguien abierta detrás.

### `roombundle`: lo mismo, en un fichero que se manda por chat

`roomprobe.exe` no se vale por sí mismo. Necesita al lado el motor, `wintun.dll` para crear el adaptador y `Packet.dll`, que el motor importa de forma dura. Pedirle a alguien que mantenga cinco ficheros juntos para ayudar diez minutos no funciona.

`roombundle` los empotra con `go:embed`, se eleva una sola vez, los suelta en una carpeta temporal, corre roomprobe esperándolo, y borra la carpeta al terminar. Un `.exe` de unos 49 MB.

Cuatro decisiones que sostienen que funcione:

1. **Se eleva ANTES de extraer.** roomprobe se auto-eleva si no tiene permisos: la copia sin privilegios lanza una elevada y muere en el acto. Un bundle que corriera roomprobe sin estar elevado vería terminar al proceso en un segundo y borraría la carpeta con la copia elevada trabajando dentro.
2. **La carga va detrás de la etiqueta de compilación `bundle`.** `go:embed` solo empotra lo que esté dentro del directorio del paquete, así que el script copia los cinco a `internal/roombundle/carga/` justo antes de compilar y los borra después. Sin la etiqueta el binario compila y se niega a correr diciendo por qué, en vez de producir un bundle vacío que falla recién en la máquina de otra persona.
3. **El log y los datos quedan junto al bundle, no en el temporal.** roomprobe los pone junto a SU ejecutable, que aquí vive en la carpeta que se borra al salir, así que el bundle le pasa `-log` y `-data` apuntando a su propio directorio. Sin eso la limpieza destruiría el log justo cuando alguien lo iba a mandar, y `last-room.json` moriría en cada corrida dejando "volver a la última sala" sin funcionar nunca. La última línea que se ve al cerrar es la ruta del log.
4. **La limpieza tiene tres pasos.** `wintun.dll` y `WinDivert64.sys` quedan tomados por el kernel unos instantes después de que roomprobe termine, así que el primer borrado falla sin que pase nada malo: se reintenta unos segundos, después se apunta con `MoveFileEx(..., MOVEFILE_DELAY_UNTIL_REBOOT)` para que Windows lo borre en el próximo arranque, y si ni eso, se dice la ruta.

**Va sin firmar**, así que SmartScreen lo recibe con "Editor desconocido" y Defender puede quejarse: un ejecutable que suelta otros ejecutables y un driver en el temporal y pide administrador tiene, literalmente, la forma de un dropper. Quien lo reciba pulsa "Más información" y "Ejecutar de todas formas". Lo único que quita ese aviso es un certificado de firma de código. Es el mismo problema que el del instalador, ver `07-futuro.md`.

### `kanpachi-portable`: el PRODUCTO en un fichero que se manda por chat

Lo mismo que `roombundle`, con el producto entero en vez de la sonda: el daemon, la interfaz con todo su bundle de Flutter, el motor, las DLL y el marcador. Unos 78 MB. Del otro lado es doble clic, un UAC, y Kanpachi abierto — **sin instalar nada**: ni servicio, ni arranque con Windows, ni accesos directos, ni ProgramData.

Lo que empotra es la salida de `kanpachi-portable.ps1 -NoArrancar`, o sea la carpeta portable de verdad. Una sola receta, no dos que se desincronizan, por el mismo motivo por el que el instalador copia `{#Carga}\*` en vez de enumerar los veintitantos ficheros de Flutter.

Cinco diferencias con `roombundle`, todas medidas:

1. **Extrae recursivo.** `roombundle` escribe cinco ficheros sueltos; acá hay que preservar `data\flutter_assets\` con sus subdirectorios, y el `go:embed` lleva `all:` porque el patrón normal se salta lo que empieza por `.` o `_`, que dentro de los assets de Flutter existe. Aplanar o perder assets da una interfaz que abre en blanco.
2. **Eleva el manifiesto, no el código.** El `.syso` lleva `requireAdministrator`, así que eleva Windows antes de arrancar. La versión anterior arrancaba sin permisos y se relanzaba: quedaban dos procesos, dos ventanas de consola y dos iconos en la barra de tareas. Se vio al abrirlo.
3. **Sin consola**, enlazado con `-H windowsgui`. La ventana de `roombundle` tiene sentido porque su hijo dura lo que dura una prueba; acá el hijo dura toda la sesión de juego. Un fallo sale por un cuadro de diálogo con la ruta del log dentro.
4. **Solo el log viaja al lado del bundle, los datos no.** `roombundle` manda los dos con `-log` y `-data`. Acá `-data` rompería el contrato del marcador: la interfaz deduce su directorio de datos de dónde está ella, que es el temporal. El precio es que `identity.key` y `last-room.json` mueren en cada corrida.
5. **Comprueba que el motor no esté viejo.** Elige el más reciente de los sitios donde queda compilado, se planta si el binario es anterior al código del motor, y verifica por SHA256 que el que acabó dentro sea el que eligió. La primera versión empaquetó un motor de tres días antes, sin el arreglo de secure mode: el producto habría salido roto y sin nada que lo dijera.

**Va sin firmar**, con el mismo aviso de SmartScreen que `roombundle` y por el mismo motivo.

### Cómo se arman

```powershell
scripts\build_test_tools.ps1 -SinPreguntar     # roomprobe.exe y roombundle.exe en testTools\
scripts\build_portable_bundle.ps1              # dist\kanpachi-portable.exe
```

El segundo acepta `-RecompilarMotor` para compilar el motor desde su repositorio antes de empaquetar, y `-Motor` para forzar una ruta. Las dos pasan igual por la comprobación de antigüedad.

No hace falta consola elevada: solo compila y copia. Deja todo en `testTools\`, que está en `.gitignore`. El script corre además `GOOS=linux go vet ./internal/...` antes de nada, que es la puerta del CI: `roomprobe` la rompió una vez importando `x/sys/windows` sin etiqueta de compilación, y descubrirlo acá cuesta cuatro segundos en vez de un push.

Ninguna de las dos se compila con `-ldflags="-s -w"`, por lo mismo que el resto del proyecto.

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
| **Kanpachi desaparece solo, sin cerrar nada y sin error** | Lo mismo, y contra el daemon YA INSTALADO. **Medido el 2026-08-06:** Defender marcó `kanpachid.exe` como `Trojan:Win32/Bearfoos.A!ml` y lo borró con el proceso corriendo, cincuenta segundos después de arrancar. Se comprueba con `Get-MpThreatDetection`, que da el fichero, el PID y la hora. El log del daemon no dice nada, porque no llega a enterarse |
