import 'dart:async';

import 'package:kanpachi_ui/core/platform/app_log.dart';
import 'package:kanpachi_ui/features/shell/domain/tray_presence.dart';
import 'package:tray_manager/tray_manager.dart';

/// El icono de la bandeja de Windows, con el menú que dibuja el diseño.
///
/// El menú tiene tres entradas y una cuarta que el diseño no dibuja: salir de
/// Kanpachi. El mockup podía permitirse no tenerla; una app real que se
/// esconde en la bandeja y no ofrece cerrarse es una trampa, y la gente acaba
/// matándola por el administrador de tareas.
///
/// # El icono parpadea mientras hay sala
///
/// Alterna entre el icono normal y el mismo con un triángulo de play. Es lo que
/// avisa, sin abrir nada, de que hay una sala funcionando: vale igual siendo
/// anfitrión o invitado, porque lo que se mira es que haya sala.
///
/// **Windows no tiene iconos de bandeja animados.** Se anima repitiendo
/// `Shell_NotifyIcon(NIM_MODIFY)` con otro `hIcon`, que es textualmente lo que
/// documenta Microsoft, y es lo que hace `trayManager.setIcon` por debajo.
///
/// **Y por eso NO se hace lo mismo con el icono de la barra de tareas.** El
/// `setIcon` de `window_manager` carga dos iconos con `LoadImage` y no destruye
/// ninguno, y `WM_SETICON` no se queda con la propiedad. Medido el 2026-08-10:
/// cada icono cargado de archivo cuesta un handle de USER y unos tres de GDI, y
/// solo los libera `DestroyIcon`. Parpadear a este ritmo serían más de siete mil
/// handles por hora contra un tope de diez mil por proceso, o sea la ventana
/// quedándose sin handles a media partida.
class WindowsTray with TrayListener implements TrayPresence {
  bool _puesto = false;
  void Function()? _abrir;
  void Function()? _salirDeLaSala;
  void Function()? _cerrar;
  TrayStatus _estado = const TrayStatus.noRoom();

  static const String _abrirKey = 'abrir';
  static const String _salirSalaKey = 'salir-sala';
  static const String _cerrarKey = 'cerrar';

  static const String _iconoQuieto = 'assets/logo/tray_icon.ico';
  static const String _iconoConPlay = 'assets/logo/tray_icon_play.ico';

  /// Cuánto se queda cada cuadro. **Iguales es el parpadeo simétrico que se
  /// pidió**, y están separados a propósito: subir [_duraConPlay] y bajar
  /// [_duraQuieto] convierte el parpadeo en un latido, que cansa mucho menos en
  /// una partida de tres horas. Cambiarlo es cambiar un número.
  static const Duration _duraConPlay = Duration(milliseconds: 900);
  static const Duration _duraQuieto = Duration(milliseconds: 900);

  Timer? _parpadeo;
  bool _mostrandoPlay = false;
  bool _pintando = false;

  /// Lo último que se pidió pintar y lo último que llegó a pintarse. Cuando
  /// discrepan hay trabajo pendiente. Ver [_pintarIcono].
  String _iconoDeseado = _iconoQuieto;
  String _iconoPintado = _iconoQuieto;

  @override
  Future<void> start({
    required TrayStatus status,
    required void Function() onOpen,
    required void Function() onLeaveRoom,
    required void Function() onQuit,
  }) async {
    _abrir = onOpen;
    _salirDeLaSala = onLeaveRoom;
    _cerrar = onQuit;
    _estado = status;
    if (_puesto) {
      await _pintarMenu();
      _ajustarParpadeo();
      return;
    }
    _puesto = true;
    trayManager.addListener(this);
    // La bandeja va y viene con el daemon, así que esto puede ser un SEGUNDO
    // arranque: lo que quedó pintado la vez anterior ya no existe.
    _iconoDeseado = _iconoQuieto;
    _iconoPintado = _iconoQuieto;
    await trayManager.setIcon(_iconoQuieto);
    await trayManager.setToolTip('Kanpachi');
    await _pintarMenu();
    _ajustarParpadeo();
  }

  @override
  Future<void> update(TrayStatus status) async {
    if (!_puesto || status == _estado) return;
    _estado = status;
    await _pintarMenu();
    _ajustarParpadeo();
  }

  @override
  Future<void> stop() async {
    if (!_puesto) return;
    _puesto = false;
    // El temporizador PRIMERO. Al revés, un cuadro en vuelo pintaría sobre un
    // icono que `destroy` ya se llevó.
    _pararParpadeo();
    trayManager.removeListener(this);
    await trayManager.destroy();
  }

  /// Arranca o para el parpadeo según haya sala. Idempotente.
  void _ajustarParpadeo() {
    final bool debeParpadear = _puesto && _estado.hasRoom;
    if (debeParpadear == (_parpadeo != null)) return;
    if (!debeParpadear) {
      _pararParpadeo();
      // Se vuelve al icono quieto, que es lo que dice «no hay sala».
      unawaited(_pintarIcono(_iconoQuieto));
      return;
    }
    _programarSiguiente();
  }

  void _pararParpadeo() {
    _parpadeo?.cancel();
    _parpadeo = null;
    _mostrandoPlay = false;
  }

  /// Un temporizador de UNA vez por cuadro, y no `Timer.periodic`, para que los
  /// dos cuadros puedan durar distinto sin cambiar la forma de esto.
  void _programarSiguiente() {
    final Duration cuanto = _mostrandoPlay ? _duraConPlay : _duraQuieto;
    _parpadeo = Timer(cuanto, () async {
      if (_parpadeo == null || !_puesto) return;
      _mostrandoPlay = !_mostrandoPlay;
      await _pintarIcono(_mostrandoPlay ? _iconoConPlay : _iconoQuieto);
      // Se encadena DESPUÉS de pintar, no en paralelo: así un cuadro lento
      // atrasa el siguiente en vez de amontonarse con él.
      if (_parpadeo != null && _puesto) _programarSiguiente();
    });
  }

  /// Pinta un cuadro, y **no descarta el último que le pidieron**.
  ///
  /// Descartarlo era un fallo de verdad: `setIcon` cruza a C++ y vuelve, así que
  /// si la sala se acaba mientras un cuadro está en vuelo, el repintado al icono
  /// quieto caía y la bandeja se quedaba con el play puesto sin sala detrás. En
  /// vez de soltarlo se anota como deseado y el bucle converge.
  ///
  /// **Un fallo para el parpadeo en vez de repetirse**: si `setIcon` empieza a
  /// fallar, seguir intentándolo cada segundo llena el registro con la misma
  /// línea hasta que alguien cierre la app.
  Future<void> _pintarIcono(String ruta) async {
    _iconoDeseado = ruta;
    if (!_puesto || _pintando) return;
    _pintando = true;
    try {
      while (_puesto && _iconoPintado != _iconoDeseado) {
        final String objetivo = _iconoDeseado;
        await trayManager.setIcon(objetivo);
        _iconoPintado = objetivo;
      }
    } on Object catch (e, s) {
      AppLog.error('pintando el icono de la bandeja', e, s);
      _pararParpadeo();
    } finally {
      _pintando = false;
    }
  }

  Future<void> _pintarMenu() async {
    await trayManager.setContextMenu(
      Menu(
        items: <MenuItem>[
          // La primera entrada es el estado, no una acción: va deshabilitada
          // para que se lea y no se pulse.
          MenuItem(label: _estado.line, disabled: true),
          MenuItem.separator(),
          MenuItem(key: _abrirKey, label: 'Abrir Kanpachi'),
          // Se deshabilita mientras se sale, y no se esconde. Esconderla
          // movería las entradas de abajo justo cuando el menú está abierto y
          // el ratón encima, así que el clic siguiente caería en otra cosa.
          if (_estado.hasRoom)
            MenuItem(
              key: _salirSalaKey,
              label: 'Salir de la sala',
              disabled: _estado.leaving,
            ),
          MenuItem.separator(),
          MenuItem(key: _cerrarKey, label: 'Salir de Kanpachi'),
        ],
      ),
    );
  }

  /// Clic izquierdo abre la ventana. Es lo que hace todo Windows, y esperar un
  /// menú ahí sería pelearse con el reflejo de todo el mundo.
  @override
  void onTrayIconMouseDown() => _abrir?.call();

  @override
  void onTrayIconRightMouseDown() {
    trayManager.popUpContextMenu();
  }

  @override
  void onTrayMenuItemClick(MenuItem menuItem) {
    switch (menuItem.key) {
      case _abrirKey:
        _abrir?.call();
      case _salirSalaKey:
        _salirDeLaSala?.call();
      case _cerrarKey:
        _cerrar?.call();
    }
  }
}
