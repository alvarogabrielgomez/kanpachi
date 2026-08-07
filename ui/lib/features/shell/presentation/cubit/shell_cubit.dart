import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';

/// Las pantallas de la app.
///
/// La navegación es un enum y no un router de rutas con URL a propósito: esto
/// es una ventana de escritorio sin barra de direcciones, sin historial que el
/// usuario pueda manipular y sin enlaces profundos más allá del `kanpachi://`
/// que entra por el sistema. Un `go_router` acá añadiría un modelo mental
/// entero para resolver un problema que no tenemos.
enum AppScreen {
  welcome,
  nickname,
  home,
  gamePicker,
  catalog,
  manualGame,
  room,

  /// Lo que esta PC tiene abierto, MEDIDO. Se llega desde la sala, y no está
  /// dentro de ella porque no es información de la sala: es de la máquina, y
  /// sigue siendo cierta sin sala abierta.
  exposure,
  invite,

  /// Los dos ajustes que hay. Se llega por el engranaje del menú de cuenta, y
  /// desde ningún otro sitio: nada de acá hace falta para jugar.
  settings,
}

/// Los diálogos, que se dibujan por encima de la pantalla actual.
enum AppDialog {
  none,
  confirmGame,
  confirmKick,
  confirmRenew,

  /// Cerrar la sala siendo el host, con gente dentro. Se pregunta porque no es
  /// salirse: es terminar la sala para todos los que están jugando.
  confirmClose,

  /// La sala que quedó abierta del arranque anterior: reabrirla o cerrarla.
  ///
  /// Es el único diálogo que se abre SIN que nadie pulse nada: lo dispara el
  /// latido al descubrir el archivo del arranque anterior. Preguntar y no
  /// reabrir sola es la invariante; ver `docs/05-ui.md`.
  resumeRoom,
}

/// Cómo se listan los juegos.
enum GameArtMode { cover, list }

@immutable
class ShellState {
  const ShellState({
    this.screen = AppScreen.welcome,
    this.history = const <AppScreen>[],
    this.dialog = AppDialog.none,
    this.themeMode = ThemeMode.system,
    this.density = AppDensity.balanced,
    this.artMode = GameArtMode.cover,
    this.ambient = true,
    this.accountMenuOpen = false,
    this.pickerCameFromRoom = false,
    this.kickTarget,
  });

  final AppScreen screen;

  /// Por dónde se pasó para llegar a [screen], de lo más viejo a lo más nuevo.
  ///
  /// Sin esto, la flecha de volver era un destino escrito a mano en cada
  /// pantalla, y ese destino se equivocaba en cuanto se llegaba desde otro
  /// sitio: un host dentro de su sala que abre un enlace de invitación pulsaba
  /// «Cancelar» y aterrizaba en la portada, o sea que volver lo sacaba de la
  /// sala en la que estaba. Volver es una operación sobre el CAMINO, no sobre
  /// la pantalla, y por eso el camino tiene que existir.
  final List<AppScreen> history;

  final AppDialog dialog;
  final ThemeMode themeMode;
  final AppDensity density;
  final GameArtMode artMode;

  /// El fondo en movimiento. Se puede apagar: corre durante horas mientras
  /// alguien juega, y a quien le molesta el movimiento le molesta de verdad.
  final bool ambient;

  final bool accountMenuOpen;

  /// De dónde se abrió el selector de juego. Cambia el título, el texto y a
  /// dónde vuelve la flecha: no es lo mismo elegir juego PARA crear una sala
  /// que cambiar el de una sala que ya existe y tiene gente dentro.
  final bool pickerCameFromRoom;

  final Member? kickTarget;

  ShellState copyWith({
    AppScreen? screen,
    List<AppScreen>? history,
    AppDialog? dialog,
    ThemeMode? themeMode,
    AppDensity? density,
    GameArtMode? artMode,
    bool? ambient,
    bool? accountMenuOpen,
    bool? pickerCameFromRoom,
    Member? kickTarget,
    bool clearKickTarget = false,
  }) => ShellState(
    screen: screen ?? this.screen,
    history: history ?? this.history,
    dialog: dialog ?? this.dialog,
    themeMode: themeMode ?? this.themeMode,
    density: density ?? this.density,
    artMode: artMode ?? this.artMode,
    ambient: ambient ?? this.ambient,
    accountMenuOpen: accountMenuOpen ?? this.accountMenuOpen,
    pickerCameFromRoom: pickerCameFromRoom ?? this.pickerCameFromRoom,
    kickTarget: clearKickTarget ? null : (kickTarget ?? this.kickTarget),
  );
}

/// Navegación y preferencias de presentación.
///
/// Está separado de `SessionCubit` porque son dos cosas con dueños distintos:
/// la sesión la manda el daemon y sobrevive a que se cierre la ventana; esto
/// es de la ventana y sólo de la ventana.
class ShellCubit extends Cubit<ShellState> {
  ShellCubit({AppScreen initial = AppScreen.welcome})
    : super(ShellState(screen: initial));

  /// Cuántas pantallas se recuerdan hacia atrás.
  ///
  /// El tope existe porque nada poda un historial que crece durante una sesión
  /// de horas. Diez es más de lo que cualquier camino real usa: la app tiene
  /// once pantallas y [_apilado] desenrolla los ciclos en vez de apilarlos.
  static const int _maxHistorial = 10;

  void go(AppScreen screen) {
    // Ir a donde ya se está no es navegar. Sin esto, el latido que reafirma la
    // sala metería una copia de `room` detrás de `room`, y volver desde la
    // siguiente pantalla se quedaría a mitad de camino.
    if (screen == state.screen) {
      emit(state.copyWith(dialog: AppDialog.none, accountMenuOpen: false));
      return;
    }
    emit(
      state.copyWith(
        screen: screen,
        history: _apilado(screen),
        dialog: AppDialog.none,
        accountMenuOpen: false,
      ),
    );
  }

  /// Vuelve a la pantalla anterior DE VERDAD.
  ///
  /// Antes cada flecha escribía su destino a mano, y ese destino solo acertaba
  /// cuando se había llegado por el camino que quien la escribió tenía en la
  /// cabeza. El caso que lo delató: host dentro de su sala, le llega un enlace
  /// de invitación, la pantalla del enlace se pone delante, pulsa «Cancelar»
  /// para no salirse de lo suyo, y la flecha lo dejaba en la portada.
  ///
  /// Sin historial se vuelve a la portada, que es el único sitio que siempre
  /// existe. El suelo de `_visibleScreen` decide si con sala abierta esa
  /// portada es habitable.
  void back() {
    final List<AppScreen> h = state.history;
    if (h.isEmpty) {
      emit(
        state.copyWith(
          screen: AppScreen.home,
          dialog: AppDialog.none,
          accountMenuOpen: false,
        ),
      );
      return;
    }
    emit(
      state.copyWith(
        screen: h.last,
        history: h.sublist(0, h.length - 1),
        dialog: AppDialog.none,
        accountMenuOpen: false,
      ),
    );
  }

  /// Se acabó la sala: a la portada, y el camino que llevaba a ella se borra.
  ///
  /// Podar es la mitad que importa. Un historial con `room` dentro después de
  /// cerrar la sala hace que la siguiente flecha de volver apunte a una
  /// pantalla que ya no se puede dibujar, y el suelo la rebota a la portada:
  /// una flecha que no hace nada visible es peor que una que no está.
  void leftRoom() => emit(
    state.copyWith(
      screen: AppScreen.home,
      history: state.history
          .where(
            (AppScreen s) => s != AppScreen.room && s != AppScreen.exposure,
          )
          .toList(),
      dialog: AppDialog.none,
      accountMenuOpen: false,
    ),
  );

  /// Abre el selector recordando de dónde vino.
  void openGamePicker({required bool fromRoom}) => emit(
    state.copyWith(
      screen: AppScreen.gamePicker,
      history: _apilado(AppScreen.gamePicker),
      dialog: AppDialog.none,
      pickerCameFromRoom: fromRoom,
      accountMenuOpen: false,
    ),
  );

  /// El historial que deja navegar a [siguiente].
  ///
  /// Volver a una pantalla que ya está en el camino lo DESENROLLA hasta ella en
  /// vez de apilar otra copia. Sin eso, sala → ajustes → sala → ajustes crece
  /// sin fin y volver dos veces desde ahí devuelve a una sala que sigue siendo
  /// la misma, que es un historial contando un viaje que nadie hizo.
  List<AppScreen> _apilado(AppScreen siguiente) {
    final int previo = state.history.indexOf(siguiente);
    if (previo >= 0) return state.history.sublist(0, previo);

    final List<AppScreen> h = <AppScreen>[...state.history, state.screen];
    if (h.length <= _maxHistorial) return h;
    return h.sublist(h.length - _maxHistorial);
  }

  void showDialog(AppDialog dialog) => emit(state.copyWith(dialog: dialog));

  void closeDialog() =>
      emit(state.copyWith(dialog: AppDialog.none, clearKickTarget: true));

  void askKick(Member member) =>
      emit(state.copyWith(dialog: AppDialog.confirmKick, kickTarget: member));

  void toggleAccountMenu() =>
      emit(state.copyWith(accountMenuOpen: !state.accountMenuOpen));

  void closeAccountMenu() => emit(state.copyWith(accountMenuOpen: false));

  void setArtMode(GameArtMode mode) => emit(state.copyWith(artMode: mode));

  void setDensity(AppDensity density) => emit(state.copyWith(density: density));

  void setAmbient({required bool enabled}) =>
      emit(state.copyWith(ambient: enabled));

  void cycleTheme() => emit(
    state.copyWith(
      themeMode: switch (state.themeMode) {
        ThemeMode.system => ThemeMode.light,
        ThemeMode.light => ThemeMode.dark,
        ThemeMode.dark => ThemeMode.system,
      },
    ),
  );

  void cycleDensity() => emit(
    state.copyWith(
      density: switch (state.density) {
        AppDensity.airy => AppDensity.balanced,
        AppDensity.balanced => AppDensity.dense,
        AppDensity.dense => AppDensity.airy,
      },
    ),
  );
}
