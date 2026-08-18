import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';
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

  /// A qué registro le pide sala esta máquina.
  ///
  /// Es una pantalla propia y no un campo dentro de ajustes porque se llega a
  /// ella desde el diálogo de confianza, con «Cambiar registro», en mitad de
  /// abrir una sala. Y porque va a crecer: el paso 2 es el password que un
  /// registro cerrado pide para hospedar.
  seed,

  /// La contraseña que un registro cerrado pide para HOSPEDAR.
  ///
  /// Se llega a ella cuando abrir una sala falla con [FailureCode.seedPassword],
  /// y desde ningún otro sitio. Preguntarla antes le cobraría a todo el mundo el
  /// roce de un caso que casi ningún registro tiene.
  seedPassword,
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

  /// Dejar de volver a la sala anterior, para entrar a otra o abrir una propia.
  ///
  /// Va ANTES del de confianza: primero se pregunta por lo que se pierde y
  /// después por la máquina a la que se le va a hablar.
  confirmDisplace,

  /// Confiar en un registro, antes de abrir una sala o de entrar a una.
  ///
  /// Es UNO para los dos momentos porque es la misma decisión: hablarle a la
  /// máquina de un tercero, que ve tu IP pública y por la que pasa todo el
  /// mundo de esa sala. Cambia de quién sale el nombre, y no qué se decide: al
  /// crear sale del registro que esta máquina tiene configurado, y al entrar
  /// del código que te pegaron.
  trustSeed,

  /// Apagar la cuarentena de base. Apagar SÍ se confirma y encender no:
  /// poner protección no necesita permiso, quitarla conviene leerla una vez.
  confirmQuarantineOff,
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
    this.portable = false,
    this.kickTarget,
    this.trust,
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

  /// Si esta copia de Kanpachi es la portable.
  ///
  /// **Llega inyectado y no se lee acá**, y eso no es ceremonia: la respuesta
  /// vive en un marcador en disco que conoce `PipeNames`, que es infra, y una
  /// pantalla que importa infra rompe el candado de `import_purity_test`. Lo
  /// resuelve `main()`, que ya lo pregunta para otra cosa, y lo entrega al
  /// registrar el cubit.
  ///
  /// Es un hecho de la MÁQUINA y no del estado: no cambia mientras la ventana
  /// vive, así que nace con el cubit y nadie lo escribe después. Lo usa la
  /// pantalla de configuración para no ofrecer «abrir con Windows», que en
  /// portable no tiene servicio que arrancar.
  final bool portable;

  final Member? kickTarget;

  /// Qué se está por hacer contra qué registro, mientras el diálogo pregunta.
  ///
  /// Nulo salvo con [AppDialog.trustSeed] arriba. Va en el estado y no en un
  /// callback guardado porque este estado es inmutable y comparable: una
  /// función dentro rompería la igualdad y con ella el redibujado.
  final TrustRequest? trust;

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
    TrustRequest? trust,
    bool clearTrust = false,
  }) => ShellState(
    portable: portable,
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
    trust: clearTrust ? null : (trust ?? this.trust),
  );
}

/// Lo que el diálogo de confianza necesita saber para preguntar y para actuar.
@immutable
class TrustRequest {
  const TrustRequest.hosting({
    required this.seed,
    required this.suggestedName,
    this.replace = false,
  }) : joining = false,
       code = '',
       preview = null;

  const TrustRequest.joining({
    required this.seed,
    required this.code,
    this.preview,
    this.replace = false,
  }) : joining = true,
       suggestedName = '';

  /// El registro que se va a usar. Nunca vacío: sin nombre no hay nada que
  /// enseñar, y un diálogo que pregunta por una máquina sin nombre no pregunta
  /// nada.
  final String seed;

  /// Entrar, contra abrir. Cambia el texto entero y qué se hace al confirmar.
  final bool joining;

  /// El código pegado, TAL CUAL. Solo con [joining].
  final String code;

  /// Con qué nombre se abre si nadie escribió ninguno. Solo sin [joining].
  ///
  /// **No es el nombre**, es el respaldo. El nombre lo lleva el borrador de la
  /// sesión, que se edita en el campo de la portada Y dentro de este diálogo, y
  /// es el mismo dato en los dos: cambiarlo en uno mueve el otro mientras se
  /// mira. Ver [SessionState.roomNameDraft].
  final String suggestedName;

  /// Lo que el daemon ya sabe de ese código, resuelto ANTES de preguntar.
  ///
  /// Es de dónde sale la huella del host y lo que la libreta dice de ella. Null
  /// cuando no se pudo resolver, y entonces el diálogo pregunta igual por el
  /// servidor: no haber podido comprobar quién hospeda no quita la decisión que
  /// este diálogo existe para tomar.
  final PendingInvite? preview;

  /// Que ya se confirmó dejar atrás lo que estorbaba.
  ///
  /// Viaja hasta el daemon, que es quien decide de verdad: sin esto rechaza, con
  /// esto sale de lo anterior y entra, todo bajo el mismo candado. La pantalla no
  /// calcula si hace falta — se lo dijo el daemon en el estado. Ver
  /// `domain.Displacement`.
  final bool replace;

  @override
  bool operator ==(Object other) =>
      other is TrustRequest &&
      other.seed == seed &&
      other.joining == joining &&
      other.code == code &&
      other.suggestedName == suggestedName &&
      other.preview == preview &&
      other.replace == replace;

  @override
  int get hashCode =>
      Object.hash(seed, joining, code, suggestedName, preview, replace);
}

/// Navegación y preferencias de presentación.
///
/// Está separado de `SessionCubit` porque son dos cosas con dueños distintos:
/// la sesión la manda el daemon y sobrevive a que se cierre la ventana; esto
/// es de la ventana y sólo de la ventana.
class ShellCubit extends Cubit<ShellState> {
  ShellCubit({AppScreen initial = AppScreen.welcome, bool portable = false})
    : super(ShellState(screen: initial, portable: portable));

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

  void closeDialog() => emit(
    state.copyWith(
      dialog: AppDialog.none,
      clearKickTarget: true,
      clearTrust: true,
    ),
  );

  /// Abre el diálogo de confianza con lo que se está por hacer.
  void askTrust(TrustRequest req) =>
      emit(state.copyWith(dialog: AppDialog.trustSeed, trust: req));

  /// Pregunta antes de dejar atrás la sala a la que se estaba volviendo.
  ///
  /// Lleva el MISMO [TrustRequest] que se va a usar después, y no un callback:
  /// este estado es inmutable y comparable, y una función dentro rompería la
  /// igualdad. Al confirmar, el diálogo lo pasa a [askTrust] con `replace`
  /// puesto, que es lo único que cambia entre haber preguntado y no.
  void askDisplace(TrustRequest next) =>
      emit(state.copyWith(dialog: AppDialog.confirmDisplace, trust: next));

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
