/// El banco de frases de la pantalla de carga, y la escala que las mueve.
///
/// # Por qué existe
///
/// Abrir una sala tarda decenas de segundos con todo funcionando. Una espera
/// muda de ese largo se lee como un cuelgue, y quien no encendió el modo
/// verboso no tiene ninguna otra señal de que algo avanza. Estas frases son esa
/// señal: cambian con los pasos REALES que emite el daemon, así que moverse
/// significa avanzar de verdad.
///
/// # Por qué son tonterías temáticas y no descripciones técnicas
///
/// Porque lo técnico ya está dicho en el panel de detalle, y decirlo dos veces
/// en dos registros distintos no informa a nadie. Estas hablan del juego que se
/// está por jugar, que es lo que la persona tiene en la cabeza mientras espera.
library;

/// Las tres esperas que tienen frases propias.
///
/// Cerrar la sala no es una cuarta: comparte las frases de salir porque es lo
/// mismo desde adentro (se recoge y se apaga), y sólo cambia el rótulo — ver
/// [loadingKicker].
enum LoadingFlow { creating, joining, leaving }

/// El rótulo en versalitas que encabeza la espera.
///
/// [closing] separa cerrar de salir, y es la única distinción que necesita:
/// al host se le cierra la sala para todos, y decir "saliendo" mentiría por
/// omisión sobre lo que les pasa a los demás.
String loadingKicker(LoadingFlow flow, {bool closing = false}) =>
    switch (flow) {
      LoadingFlow.creating => 'CREANDO LA SALA',
      LoadingFlow.joining => 'ENTRANDO A LA SALA',
      LoadingFlow.leaving =>
        closing ? 'CERRANDO LA SALA' : 'SALIENDO DE LA SALA',
    };

/// Cuántos pasos emite el daemon en cada espera, aproximadamente.
///
/// # Por qué es una tabla a mano y no un dato del daemon
///
/// Porque el daemon no sabe cuántos pasos le quedan: los va contando mientras
/// pasan, y algunos dependen de lo que encuentre (un código que el registro no
/// contesta añade uno, un adaptador ajeno añade otros). Un total exacto no
/// existe.
///
/// # Por qué entonces se puede dibujar una barra
///
/// Porque la barra nunca llega al final por su cuenta: se topa en
/// [maxLoadingFraction] y sólo la completa el fin real de la operación. Una
/// espera que emita más pasos de los previstos se queda esperando ahí, que es
/// exactamente lo que está pasando. Lo que la barra promete es "esto avanza",
/// y eso sí es verdad porque cada salto es un paso que ocurrió.
///
/// Medido contra `core/usecase`: crear pasa por la tabla de rutas, la identidad
/// de la red, el motor, el vestíbulo, la contención, el canal, la tarjeta, el
/// registro y el MTU; entrar es bastante más corto; salir y cerrar comparten el
/// desmontaje de `session.go`.
int expectedSteps(LoadingFlow flow) => switch (flow) {
  LoadingFlow.creating => 13,
  LoadingFlow.joining => 8,
  LoadingFlow.leaving => 11,
};

/// Hasta dónde llega la barra con los pasos solos.
///
/// El último tramo lo paga el final de la operación y nadie más. Sin este tope,
/// una espera con más pasos de los previstos enseñaría el 100% mientras sigue
/// trabajando, que es la única forma de que una barra mienta de verdad.
const double maxLoadingFraction = 0.95;

/// Un tópico: el mismo mundo contado en las tres esperas.
///
/// Se elige uno al azar cada vez que aparece la pantalla y se queda hasta que
/// la espera termina. Cambiar de tópico a mitad de una espera rompería lo único
/// que las frases tienen que sostener: que son una sola historia.
class LoadingTopic {
  const LoadingTopic({
    required this.id,
    required this.creating,
    required this.joining,
    required this.leaving,
  });

  final String id;
  final List<String> creating;
  final List<String> joining;
  final List<String> leaving;

  List<String> phrases(LoadingFlow flow) => switch (flow) {
    LoadingFlow.creating => creating,
    LoadingFlow.joining => joining,
    LoadingFlow.leaving => leaving,
  };
}

/// Los tópicos, tal cual el banco del archivo de diseño.
const List<LoadingTopic> kLoadingTopics = <LoadingTopic>[
  LoadingTopic(
    id: 'espacio',
    creating: <String>[
      'Encendiendo los motores',
      'Organizando polvo espacial',
      'Presurizando la cabina',
      'Alineando la antena larga',
      'Trazando la ruta de salto',
    ],
    joining: <String>[
      'Pidiendo permiso de atraque',
      'Igualando la órbita',
      'Abriendo la compuerta',
      'Quitándose el casco',
      'Sincronizando relojes',
    ],
    leaving: <String>[
      'Sellando la compuerta',
      'Soltando las amarras',
      'Apagando los motores',
      'Barriendo el polvo espacial',
    ],
  ),
  LoadingTopic(
    id: 'bloques',
    creating: <String>[
      'Generando el terreno',
      'Picando el primer bloque',
      'Encendiendo la antorcha',
      'Armando la mesa de trabajo',
      'Espantando a los creepers',
    ],
    joining: <String>[
      'Cruzando el portal',
      'Buscando una cama libre',
      'Guardando cosas en el cofre',
      'Saludando a los aldeanos',
    ],
    leaving: <String>[
      'Guardando el inventario',
      'Apagando el horno',
      'Tapando el hueco de la mina',
      'Durmiendo hasta el alba',
    ],
  ),
  LoadingTopic(
    id: 'apocalipsis',
    creating: <String>[
      'Atrancando las ventanas',
      'Contando las balas',
      'Revisando el generador',
      'Levantando la cerca',
      'Marcando el mapa del pueblo',
    ],
    joining: <String>[
      'Tocando la puerta despacio',
      'Diciendo el santo y seña',
      'Revisando que no te sigan',
      'Trancando la puerta otra vez',
    ],
    leaving: <String>[
      'Recogiendo tus cosas',
      'Apagando la linterna',
      'Saliendo por el techo',
      'Tapando el rastro',
    ],
  ),
  LoadingTopic(
    id: 'carreras',
    creating: <String>[
      'Calentando los cauchos',
      'Afinando la sexta marcha',
      'Cargando el nitro',
      'Ajustando la suspensión',
      'Cerrando el circuito',
    ],
    joining: <String>[
      'Buscando puesto en la parrilla',
      'Chequeando la presión',
      'Bajando la ventanilla',
      'Esperando la luz verde',
    ],
    leaving: <String>[
      'Entrando a los pits',
      'Apagando el motor',
      'Guardando los cauchos',
      'Firmando la planilla',
    ],
  ),
  LoadingTopic(
    id: 'fabrica',
    creating: <String>[
      'Tendiendo la primera cinta',
      'Alimentando el horno',
      'Trazando las tuberías',
      'Balanceando la línea',
      'Instalando el brazo robot',
    ],
    joining: <String>[
      'Fichando la entrada',
      'Buscando un hueco en la línea',
      'Enchufando tu terminal',
      'Sincronizando los relojes',
    ],
    leaving: <String>[
      'Parando la cinta',
      'Apagando el horno',
      'Guardando los planos',
      'Cerrando el galpón',
    ],
  ),
  LoadingTopic(
    id: 'arena',
    creating: <String>[
      'Abriendo la arena',
      'Barriendo el carril central',
      'Comprando botas',
      'Poniendo los wards',
      'Contando los minions',
    ],
    joining: <String>[
      'Escogiendo campeón',
      'Cargando las runas',
      'Saludando al equipo',
      'Corriendo a tu carril',
    ],
    leaving: <String>[
      'Volviendo a la base',
      'Recogiendo el oro',
      'Quitando los wards',
      'Cerrando el nexo',
    ],
  ),
  LoadingTopic(
    id: 'dados',
    creating: <String>[
      'Armando la mesa',
      'Buscando el dado de veinte',
      'Dibujando la mazmorra',
      'Repartiendo las fichas',
      'Tirando iniciativa',
    ],
    joining: <String>[
      'Presentando tu personaje',
      'Pidiendo una silla al máster',
      'Sumando tus modificadores',
      'Sacando la hoja arrugada',
    ],
    leaving: <String>[
      'Guardando la hoja',
      'Recogiendo los dados',
      'Anotando la experiencia',
      'Cerrando el manual',
    ],
  ),
  LoadingTopic(
    id: 'taberna',
    creating: <String>[
      'Reuniendo al grupo',
      'Reservando mesa en la taberna',
      'Repartiendo pociones',
      'Encendiendo el farol',
      'Marcando el mapa del reino',
    ],
    joining: <String>[
      'Empujando la puerta del salón',
      'Pidiendo hidromiel',
      'Contando quién falta',
      'Sentándote junto al fuego',
    ],
    leaving: <String>[
      'Pagando la cuenta',
      'Apagando el farol',
      'Guardando las pociones',
      'Tomando el camino de vuelta',
    ],
  ),
  LoadingTopic(
    id: 'cazademonios',
    creating: <String>[
      'Puliendo la espada',
      'Afilando las garras',
      'Cargando el revólver',
      'Ensayando la pose',
      'Subiendo el ritmo',
    ],
    joining: <String>[
      'Entrando con estilo',
      'Peinándose antes de pelear',
      'Midiendo al rival',
      'Encadenando el saludo',
    ],
    leaving: <String>[
      'Guardando la espada',
      'Cobrando el trabajo',
      'Colgando el abrigo rojo',
      'Saliendo sin mirar atrás',
    ],
  ),
  LoadingTopic(
    id: 'piratas',
    creating: <String>[
      'Izando las velas',
      'Revisando el catalejo',
      'Repartiendo el ron',
      'Trazando la carta de navegación',
      'Colgando la bandera',
    ],
    joining: <String>[
      'Pidiendo permiso a bordo',
      'Subiendo por la escala',
      'Diciendo la contraseña',
      'Buscando hamaca libre',
    ],
    leaving: <String>[
      'Bajando el bote',
      'Recogiendo el botín',
      'Levando el ancla',
      'Arriando la bandera',
    ],
  ),
  LoadingTopic(
    id: 'futbol',
    creating: <String>[
      'Pintando la cal',
      'Inflando el balón',
      'Armando la alineación',
      'Cortando la grama',
      'Colgando las redes',
    ],
    joining: <String>[
      'Amarrando los tacos',
      'Saliendo del túnel',
      'Saludando al equipo',
      'Estirando antes del pito',
    ],
    leaving: <String>[
      'Cambiando la camiseta',
      'Recogiendo los conos',
      'Apagando las luces',
      'Firmando la planilla',
    ],
  ),
  LoadingTopic(
    id: 'sigilo',
    creating: <String>[
      'Estudiando los planos',
      'Contando los guardias',
      'Desactivando la alarma',
      'Cronometrando la ronda',
      'Aceitando las bisagras',
    ],
    joining: <String>[
      'Entrando por el ducto',
      'Esquivando la cámara',
      'Caminando en puntillas',
      'Copiando la tarjeta',
    ],
    leaving: <String>[
      'Borrando las huellas',
      'Cerrando la caja fuerte',
      'Devolviendo la llave',
      'Saliendo por el techo',
    ],
  ),
  LoadingTopic(
    id: 'detective',
    creating: <String>[
      'Clavando chinches en el corcho',
      'Revelando las fotos',
      'Sirviendo café frío',
      'Tendiendo el hilo rojo',
      'Abriendo el expediente',
    ],
    joining: <String>[
      'Tocando dos veces',
      'Mostrando la placa',
      'Anotando en la libreta',
      'Preguntando por el testigo',
    ],
    leaving: <String>[
      'Cerrando el expediente',
      'Guardando la libreta',
      'Apagando la lámpara',
      'Poniéndose el sombrero',
    ],
  ),
  LoadingTopic(
    id: 'terror',
    creating: <String>[
      'Revisando el pasillo',
      'Cargando la cámara',
      'Cambiando las pilas',
      'Cerrando el sótano',
      'Contando los pasos',
    ],
    joining: <String>[
      'Empujando la puerta',
      'Encendiendo la linterna',
      'Aguantando la respiración',
      'Mirando atrás una vez',
    ],
    leaving: <String>[
      'Corriendo a la salida',
      'Trancando el sótano',
      'Apagando la cámara',
      'Sin mirar atrás',
    ],
  ),
  LoadingTopic(
    id: 'ciberpunk',
    creating: <String>[
      'Soldando el implante',
      'Alquilando ancho de banda',
      'Rompiendo el firewall',
      'Falsificando credenciales',
      'Enfriando la torre',
    ],
    joining: <String>[
      'Enchufando el cable',
      'Pasando el control de red',
      'Cargando tu avatar',
      'Saludando en el canal',
    ],
    leaving: <String>[
      'Desconectando el cable',
      'Borrando los registros',
      'Cerrando el proxy',
      'Quemando la identidad',
    ],
  ),
  LoadingTopic(
    id: 'cocina',
    creating: <String>[
      'Calentando la plancha',
      'Picando cebolla',
      'Afilando el cuchillo',
      'Montando la estación',
      'Probando la salsa',
    ],
    joining: <String>[
      'Amarrando el delantal',
      'Gritando la orden',
      'Buscando puesto en la línea',
      'Lavándose las manos',
    ],
    leaving: <String>[
      'Apagando la hornilla',
      'Fregando las ollas',
      'Guardando la mise en place',
      'Cerrando la cocina',
    ],
  ),
  LoadingTopic(
    id: 'battleroyale',
    creating: <String>[
      'Cargando el avión',
      'Repartiendo paracaídas',
      'Marcando la zona',
      'Soltando las cajas',
      'Cerrando el círculo',
    ],
    joining: <String>[
      'Saltando del avión',
      'Buscando botín',
      'Recogiendo un casco',
      'Marcando en el mapa',
    ],
    leaving: <String>[
      'Guardando el botín',
      'Doblando el paracaídas',
      'Saliendo de la zona',
      'Contando las bajas',
    ],
  ),
];
