import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:kanpachi_ui/core/platform/app_preferences.dart';
import 'package:window_manager/window_manager.dart';

/// Remembers how big the window was, so the next run opens the same size.
///
/// # Why it is a widget and not two lines in `main`
///
/// Because remembering is not something that happens once at startup: it
/// happens every time the window is resized, for as long as the window exists.
/// That needs a listener with a lifetime, and a widget is what the framework
/// gives lifetimes to. Put in `main` it would be a listener nobody ever
/// removes.
///
/// # What it does NOT remember, and why
///
/// The position. See [AppPreferences.windowSize]: the size is a choice about
/// the app, the position is a choice about a moment — which monitor was
/// plugged in, what else was open. Restoring it is how a window comes back
/// half off screen after unplugging a display.
class WindowSizeMemory extends StatefulWidget {
  const WindowSizeMemory({
    required this.child,
    required this.preferences,
    super.key,
  });

  final Widget child;

  /// Where the size is written. Null in tests, which have no window to
  /// measure: then this is a passthrough.
  final AppPreferences? preferences;

  @override
  State<WindowSizeMemory> createState() => _WindowSizeMemoryState();
}

class _WindowSizeMemoryState extends State<WindowSizeMemory>
    with WindowListener {
  /// How long after the last resize event the size is written.
  ///
  /// Dragging an edge fires a resize per frame, so writing on each one would
  /// be a hundred disk writes for one drag. Waiting for the drag to stop turns
  /// that into a single one, and half a second is well under how long anybody
  /// takes to close the window after resizing it.
  static const Duration _settle = Duration(milliseconds: 500);

  Timer? _pending;

  @override
  void initState() {
    super.initState();
    if (widget.preferences != null) windowManager.addListener(this);
  }

  @override
  void dispose() {
    _pending?.cancel();
    if (widget.preferences != null) windowManager.removeListener(this);
    super.dispose();
  }

  /// Los DOS avisos de redimensionado, y hacen falta los dos.
  ///
  /// `onWindowResized` llega al soltar el ratón, o sea con `WM_EXITSIZEMOVE`,
  /// que solo existe cuando el arrastre lo hizo una persona. Medido: cambiar el
  /// tamaño desde fuera con `SetWindowPos` no guardaba nada. `onWindowResize`
  /// llega en cada paso del cambio, venga de donde venga, y el retardo de abajo
  /// convierte esa ráfaga en una sola escritura.
  @override
  void onWindowResize() => _remember();

  @override
  void onWindowResized() => _remember();

  /// Restaurar también cuenta: es cuando la ventana vuelve a tener un tamaño
  /// que tiene sentido guardar, y no llega como redimensionado.
  @override
  void onWindowUnmaximize() => _remember();

  /// Maximising counts too, and it is the case that would look like a bug.
  ///
  /// Windows reports a maximised window at the size of the screen, so without
  /// thinking about it, maximising and closing would reopen a window the size
  /// of the monitor but NOT maximised — which is the same thing to look at and
  /// impossible to un-maximise. So a maximised window writes nothing: what
  /// gets kept is whatever size it had before.
  @override
  void onWindowMaximize() => _pending?.cancel();

  void _remember() {
    _pending?.cancel();
    _pending = Timer(_settle, () async {
      if (await windowManager.isMaximized()) return;
      final Size size = await windowManager.getSize();
      await widget.preferences?.setWindowSize(size);
    });
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
