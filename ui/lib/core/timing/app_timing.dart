/// Todos los relojes de la ventana, en un solo sitio.
///
/// Cada latido, cada plazo y cada rebote de esta cara se declara acá y en
/// ningún otro lado, por lo mismo que su gemelo de Go, `core/timing`: repartidos
/// no se pueden comparar, y compararlos es lo que hace falta para no romper
/// nada. [kProgressBeat] solo tiene sentido contra el plazo de la operación que
/// acompaña, y [kSessionBeat] solo tiene sentido sabiendo que `status` no toca
/// el candado de la sesión.
///
/// # Qué NO va acá
///
/// **Las animaciones.** Cuánto dura un rebote, un fundido o el latido de un
/// punto es sensación de interfaz, y ya tiene su sitio único en `AppMotion`,
/// dentro de los tokens del sistema de diseño. Mezclarlas dejaría dos ficheros
/// de duraciones sin una regla que diga cuál mira uno.
///
/// La frontera es para qué sirve el número: si acota una ESPERA por algo que
/// pasa fuera de esta ventana, va acá; si describe cómo se mueve un píxel, va
/// en los tokens.
///
/// # Por qué no puede ser el mismo fichero que el de Go
///
/// Porque son dos lenguajes. Los dos que sí describen la misma cosa desde los
/// dos lados son [kDefaultTimeout] con su tabla y `timing.PipeDefaultTimeout`
/// del cliente de Go: el mismo pipe, la misma paciencia, escrita dos veces
/// porque no hay forma de compartirla. Están anotados el uno en el otro.
library;

/// El presupuesto por omisión de una llamada al daemon: lo que tarda algo
/// contestado de memoria o con una escritura chica a disco.
///
/// Los métodos que legítimamente tardan más llevan el suyo en `kMethodTimeouts`,
/// que vive junto a los nombres de los métodos porque está indexado por ellos:
/// traerlo acá obligaría a este fichero a importar una feature, que es la
/// dirección equivocada.
const Duration kDefaultTimeout = Duration(seconds: 10);

/// Cada cuánto la ventana le pregunta el estado al daemon.
///
/// # Por qué se puede preguntar tan seguido
///
/// Porque `status` no toca el candado de la sesión: lee la copia publicada, que
/// existe justo para esto. Así que el latido sigue contestando mientras una
/// creación de sala tiene la sesión tomada durante un minuto.
const Duration kSessionBeat = Duration(seconds: 2);

/// Cada cuánto se piden los pasos de una operación larga.
///
/// Rápido para que un paso que cae a mitad de la espera se vea mientras todavía
/// significa algo, y lento para que una creación de noventa segundos cueste un
/// par de cientos de viajes por un pipe local en vez de miles.
const Duration kProgressBeat = Duration(milliseconds: 400);

/// Cuánto se espera tras el último evento de redimensionado antes de escribir
/// el tamaño de la ventana.
///
/// Arrastrar un borde dispara un evento por fotograma, así que escribir en cada
/// uno serían cien escrituras a disco por un solo arrastre. Esperar a que el
/// arrastre pare lo convierte en una, y medio segundo está muy por debajo de lo
/// que tarda cualquiera en cerrar la ventana después de redimensionarla.
const Duration kWindowSizeSettle = Duration(milliseconds: 500);

/// El plazo de cada paso de la consulta de versión contra GitHub.
///
/// Corto, y aplicado a CADA paso. Alguien está esperando esta respuesta, y una
/// lenta vale menos que el socket que mantiene abierto.
const Duration kUpdateCheckTimeout = Duration(seconds: 5);
