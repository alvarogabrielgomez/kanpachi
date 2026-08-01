import 'package:kanpachi_ui/features/shell/domain/tray_presence.dart';
import 'package:tray_manager/tray_manager.dart';

/// El icono de la bandeja de Windows, con el menú que dibuja el diseño.
///
/// El menú tiene tres entradas y una cuarta que el diseño no dibuja: salir de
/// Kanpachi. El mockup podía permitirse no tenerla; una app real que se
/// esconde en la bandeja y no ofrece cerrarse es una trampa, y la gente acaba
/// matándola por el administrador de tareas.
class WindowsTray with TrayListener implements TrayPresence {
  bool _puesto = false;
  void Function()? _abrir;
  void Function()? _salirDeLaSala;
  void Function()? _cerrar;
  TrayStatus _estado = const TrayStatus.noRoom();

  static const String _abrirKey = 'abrir';
  static const String _salirSalaKey = 'salir-sala';
  static const String _cerrarKey = 'cerrar';

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
      return;
    }
    _puesto = true;
    trayManager.addListener(this);
    await trayManager.setIcon('assets/logo/tray_icon.ico');
    await trayManager.setToolTip('Kanpachi');
    await _pintarMenu();
  }

  @override
  Future<void> update(TrayStatus status) async {
    if (!_puesto || status == _estado) return;
    _estado = status;
    await _pintarMenu();
  }

  @override
  Future<void> stop() async {
    if (!_puesto) return;
    _puesto = false;
    trayManager.removeListener(this);
    await trayManager.destroy();
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
          if (_estado.hasRoom)
            MenuItem(key: _salirSalaKey, label: 'Salir de la sala'),
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
