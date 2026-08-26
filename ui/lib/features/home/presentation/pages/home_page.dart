import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_cover.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_divider.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/features/games/presentation/widgets/game_views.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/home/presentation/widgets/saved_room_notice.dart';
import 'package:kanpachi_ui/features/home/presentation/widgets/last_room_strip.dart';
import 'package:kanpachi_ui/features/home/presentation/widgets/returning_notice.dart';
import 'package:kanpachi_ui/features/session/domain/entities/action_failure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/displacement.dart';
import 'package:kanpachi_ui/features/games/domain/steam_art.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/last_room.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/quarantine_off_notice.dart';
import 'package:kanpachi_ui/features/session/domain/invite_code.dart';
import 'package:kanpachi_ui/features/session/domain/room_names.dart';
import 'package:kanpachi_ui/features/seed/presentation/ask_to_host.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/failure_notice.dart';
import 'package:kanpachi_ui/features/shell/presentation/ask_trust.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/failure_navigation.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// La portada: sin sala.
///
/// Dos caminos y nada más — entrar con un código que te pasaron, o crear una
/// sala. Lo demás de la pantalla es tu biblioteca y los avisos de salud, que
/// no son acciones sino contexto.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  final TextEditingController _code = TextEditingController();

  /// The invite preview is in flight: the click already happened and the trust
  /// dialog is not up yet. It is what puts the join button in busy, because
  /// that gap is a network round trip to the registry and can take seconds.
  bool _resolvingInvite = false;

  /// El campo del nombre de sala. Su texto es una COPIA del borrador que vive
  /// en la sesión, y las dos direcciones están cableadas: lo que se escribe acá
  /// sube con [SessionCubit.setRoomNameDraft], y lo que cambie en el diálogo de
  /// confianza baja por el oyente de [build]. Ver [SessionState.roomNameDraft].
  ///
  /// **Nace con lo que ya hubiera**, y esa era la mitad que faltaba: el oyente
  /// solo baja los CAMBIOS, así que un borrador escrito antes —en el diálogo
  /// de confianza, que lo edita en cada tecla— dejaba este campo vacío
  /// enseñando su sugerencia, y los dos sitios decían cosas distintas del
  /// mismo dato. Visto en pantalla el 2026-08-18: la portada con la
  /// sugerencia y el diálogo con lo escrito.
  late final TextEditingController _roomName = TextEditingController(
    text: context.read<SessionCubit>().state.roomNameDraft,
  );

  /// Se sortea una vez y se queda. Volver a sortearlo en cada rebuild haría
  /// que el nombre sugerido bailara mientras se escribe el código de al lado.
  late final String _nameHint = RoomNames.suggest();

  /// Al aparecer la portada se le PREGUNTA al daemon si hay sala.
  ///
  /// El marco ya no deja pintar la portada con una sala abierta, pero eso lo
  /// decide con lo último que se supo, y lo último que se supo puede estar
  /// viejo: basta que el latido se haya perdido un rato —el daemon
  /// reiniciándose, la ventana escondida, una llamada que venció— para que la
  /// app crea que no hay sala mientras el daemon está dentro de una. La
  /// portada es justo la pantalla donde esa creencia equivocada hace daño,
  /// porque sus dos botones contestan `busy` y no hay forma de salir.
  ///
  /// Preguntar al aparecer cierra esa ventana: el daemon es la única fuente de
  /// la verdad sobre si hay sala, y cuando conteste que sí, el marco lleva a
  /// la sala sin que nadie toque nada.
  @override
  void initState() {
    super.initState();
    unawaited(context.read<SessionCubit>().refresh());
  }

  @override
  void dispose() {
    _code.dispose();
    _roomName.dispose();
    super.dispose();
  }

  /// **El campo ya no enmascara**, y quitarlo arregló un fallo callado.
  ///
  /// Enmascaraba filtrando al alfabeto y cortando a ocho, y eso destruía cuatro
  /// de las formas que el daemon acepta sin decir nada:
  /// `A7K2-M9QX@seed.ejemplo.com` se quedaba en `A7K2-M9QX`, y
  /// `https://seed.ejemplo.com/A7K2-M9QX` en `HTTP-SKAN`. El del `@` era el
  /// peor: borraba el registro en silencio, y un mismo ID en otro registro es
  /// una sala DISTINTA, así que llevaba a la sala equivocada sin un solo error.
  ///
  /// Lo que quedó es redibujar el botón. Interpretar lo pegado es de
  /// [InviteCode.parse], y decidir si vale, del daemon.
  void _onCodeChanged(String raw) => setState(() {});

  /// Un código PELADO: los ocho caracteres puestos, y sin el servidor.
  ///
  /// Es el único «todavía no vale» que se puede explicar mientras alguien
  /// escribe, y hace falta explicarlo: el botón apagado sobre un código que se
  /// ve completo no dice qué le falta, y lo que le falta es la mitad que decide
  /// a qué sala se entra. El daemon contesta lo mismo con
  /// [FailureCode.seedMissing], y eso llega después de pulsar; esto llega antes.
  bool get _faltaServidor {
    final String t = _code.text.trim();
    if (t.isEmpty || t.contains('@') || t.contains('/')) return false;
    return InviteCode.normalize(t).length == InviteCode.length;
  }

  /// Joins, and only navigates if it joined.
  ///
  /// **Navigating at the same time as asking is what left the app stuck.** A
  /// join that failed put the room screen up with no room behind it: an empty
  /// window with no way back, because every control there needs a room to act
  /// on. The failure notice was drawn underneath and could not be reached.
  Future<void> _join() => _joinWith(_code.text);

  /// The same door for the typed code and for the way back to the last room:
  /// one preview, one trust dialog, one reentry guard. Going back is entering
  /// again, with everything entering asks.
  Future<void> _joinWith(String invite) async {
    if (!InviteCode.isComplete(invite)) return;
    // Reentry guard: Enter on the field fires this too, and a second preview
    // during the first one would stack two trust dialogs.
    if (_resolvingInvite) return;
    // No se entra: se PREGUNTA. Ese registro es la máquina de un tercero, y el
    // diálogo es donde se ve a cuál antes de hablarle. Quien confirma es quien
    // llama a `joinRoom`, así que este camino termina acá.
    //
    // El nombre sale del código PEGADO, y este lado no lo normaliza: el texto
    // viaja tal cual al daemon, que es la frontera de entrada hostil.
    final String seed = InviteCode.seedOf(invite);
    if (seed.isEmpty) return;
    // Se le pregunta al daemon qué hay detrás del código ANTES de enseñar el
    // diálogo, para que la huella de quien hospeda esté ahí cuando aparece.
    // Resolver dentro del diálogo lo dejaría un instante sin ese bloque y con
    // el botón ya pulsable, que es enseñar la decisión antes que el dato del
    // que depende. Que falle no quita el diálogo: ver [TrustRequest.preview].
    // Con el botón en ocupado mientras tanto: la pregunta al registro tarda
    // segundos enteros con un servidor lejos, y un botón que no reacciona al
    // click es un click que se repite. Detectado en vivo el 2026-08-18.
    setState(() => _resolvingInvite = true);
    final PendingInvite? preview;
    try {
      preview = await context.read<SessionCubit>().previewInvite(invite);
    } finally {
      if (mounted) setState(() => _resolvingInvite = false);
    }
    if (!mounted) return;
    // El desplazamiento sale de la vista previa que se acaba de pedir, y no del
    // estado: el daemon lo calcula contra ESTE destino, así que volver a la sala
    // a la que ya se está volviendo no desplaza nada. Con la respuesta general,
    // pulsar «volver» en la última sala pediría permiso para abandonar
    // exactamente lo que se está haciendo.
    _pedirConfianza(
      TrustRequest.joining(seed: seed, code: invite, preview: preview),
      displaces: preview?.displaces,
    );
  }

  /// Same rule as [_join]: the screen moves only once there is a room.
  ///
  /// El nombre no viaja desde acá: lo lleva el borrador de la sesión, y lo que
  /// se pasa es la SUGERENCIA, para cuando nadie escribió nada. El diálogo lo
  /// enseña y se puede cambiar ahí mismo.
  Future<void> _createEmpty() => askToHost(context, suggestedName: _nameHint);

  /// Abre la confirmación que toque, y **esa decisión no se toma acá**.
  ///
  /// La regla vive en [askTrustOrDisplace], que es por donde pasan también los
  /// dos caminos de abrir sala propia. Estaba escrita acá dentro, y por eso
  /// aquellos dos no la tenían.
  void _pedirConfianza(TrustRequest next, {Displacement? displaces}) =>
      askTrustOrDisplace(context, next, preview: displaces);

  /// Baja al campo lo que se haya escrito en el diálogo.
  ///
  /// Con la posición del cursor al final, que es lo que hace que no salte: sin
  /// tocar la selección, Flutter la conserva apuntando a un sitio que en el
  /// texto nuevo puede no existir.
  void _bajarBorrador(String draft) {
    if (draft == _roomName.text) return;
    _roomName.value = TextEditingValue(
      text: draft,
      selection: TextSelection.collapsed(offset: draft.length),
    );
  }

  @override
  Widget build(BuildContext context) {
    final ShellState shell = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;
    final bool canJoin = InviteCode.isComplete(_code.text);

    // Sin los fallos que ya llevaron a su pantalla: pintarlos acá además sería
    // contar dos veces lo mismo, y la segunda en una pantalla que ya no es la
    // que lo resuelve. Ver [screenForFailure].
    final ActionFailure? failure =
        screenForFailure(session.failure?.code) == null
        ? session.failure
        : null;

    return BlocListener<SessionCubit, SessionState>(
      // Solo cuando el borrador cambia, y solo si lo cambió OTRO: el propio
      // campo ya tiene el texto puesto, y reescribirlo en cada tecla movería el
      // cursor al final mientras alguien corrige por el medio.
      listenWhen: (SessionState a, SessionState b) =>
          a.roomNameDraft != b.roomNameDraft,
      listener: (BuildContext _, SessionState st) =>
          _bajarBorrador(st.roomNameDraft),
      // El cuerpo va INCRUSTADO y no en un método que devuelva Widget: la regla
      // de la casa es widget-clase, y un helper acá además obligaría a
      // reconstruir la portada entera con cada tecla del nombre.
      child: ScreenPanels(
        // El fallo va ARRIBA del todo y no flotando sobre la pantalla, que es
        // donde vivía. Flotando abajo tapaba la biblioteca y el botón de crear
        // sala justo cuando había que volver a pulsarlo; acá empuja el formulario
        // hacia abajo, que es el precio, y a cambio se lee entero, se puede
        // desplegar el detalle y se recorre con el resto de los avisos sin que la
        // columna de la derecha se mueva.
        left: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (failure != null) ...<Widget>[
              FailureNotice(
                failure: failure,
                verbose: session.verbose,
                onDismiss: () => context.read<SessionCubit>().clearFailure(),
              ),
              const SizedBox(height: AppSpacing.x5l),
            ],
            // La sala propia que no volvió sola va PRIMERO: es de esta máquina y
            // hay gente esperándola, así que pesa más que volver a la de otro.
            if (session.savedRoom != null && !session.hasRoom) ...<Widget>[
              SavedRoomNotice(
                pending: session.savedRoom!,
                // Reabrir es entrar, así que pasa por la compuerta: esta
                // máquina puede tener a la vez su sala guardada y una vuelta
                // armada a la de otro, y reabrir sin preguntar dejaba la vuelta
                // encendida y dormida.
                onReopen: () {
                  if (askDisplaceFirst(
                    context,
                    DisplaceIntent.resumeSavedRoom,
                  )) {
                    return;
                  }
                  unawaited(context.read<SessionCubit>().resumeSavedRoom());
                },
                onDiscard: () =>
                    context.read<SessionCubit>().discardSavedRoom(),
              ),
              const SizedBox(height: AppSpacing.x5l),
            ],
            if (session.health.returning != null) ...<Widget>[
              ReturningNotice(
                progress: session.progress,
                returning: session.health.returning!,
                seedDown: session.health.seedDown,
                verbose: session.verbose,
                onLeave: () => context.read<SessionCubit>().leave(),
              ),
              const SizedBox(height: AppSpacing.x5l),
            ],
            _JoinAndCreate(
              code: _code,
              roomName: _roomName,
              nameHint: _nameHint,
              canJoin: canJoin,
              // La vuelta manual, solo con la automática apagada y sin nada
              // más ofreciéndose: mientras el daemon vuelve solo lo cuenta
              // ReturningNotice, y la sala propia guardada tiene su aviso.
              lastRoom:
                  session.lastRoom != null &&
                      !session.lastRoom!.autoReturn &&
                      session.health.returning == null &&
                      session.savedRoom == null &&
                      !session.hasRoom
                  ? session.lastRoom
                  : null,
              onReturnLast: () => _joinWith(session.lastRoom!.invite),
              onForgetLast: () => context.read<SessionCubit>().forgetLastRoom(),
              resolvingInvite: _resolvingInvite,
              missingSeed: _faltaServidor,
              daemonDown: session.daemonDown,
              seedDown: session.health.seedDown,
              alerts: session.health.alerts,
              onRoomNameChanged: (String v) =>
                  context.read<SessionCubit>().setRoomNameDraft(v),
              onCodeChanged: _onCodeChanged,
              onJoin: _join,
              onCreate: _createEmpty,
            ),
          ],
        ),
        right: _MyGames(
          games: session.installed,
          artMode: shell.artMode,
          total: session.catalog.length,
        ),
      ),
    );
  }
}

class _JoinAndCreate extends StatelessWidget {
  const _JoinAndCreate({
    required this.code,
    required this.roomName,
    required this.nameHint,
    required this.canJoin,
    required this.lastRoom,
    required this.onReturnLast,
    required this.onForgetLast,
    required this.resolvingInvite,
    required this.missingSeed,
    required this.daemonDown,
    required this.seedDown,
    required this.alerts,
    required this.onRoomNameChanged,
    required this.onCodeChanged,
    required this.onJoin,
    required this.onCreate,
  });

  final TextEditingController code;
  final TextEditingController roomName;
  final String nameHint;
  final bool canJoin;

  /// La última sala ajena a la que se puede volver, o nulo cuando no hay
  /// ninguna que ofrecer. Va PEGADA debajo del campo de código porque es lo
  /// mismo que ese campo hace: entrar con un código que ya se tiene.
  final LastRoom? lastRoom;
  final VoidCallback onReturnLast;
  final VoidCallback onForgetLast;

  /// La vista previa del código está en vuelo: el botón va en ocupado para que
  /// el click tenga respuesta en el mismo instante, porque lo que sigue es una
  /// vuelta por la red hasta el registro y tarda segundos con uno lejos.
  final bool resolvingInvite;

  /// Lo escrito tiene forma de código y le falta el servidor. Se dice debajo
  /// del campo: un botón apagado sobre ocho caracteres que se ven completos no
  /// cuenta qué falta.
  final bool missingSeed;

  /// No se pudo hablar con el servicio. Va PRIMERO, por delante de cualquier
  /// aviso: sin servicio los demás avisos son de una medición que no se hizo.
  final bool daemonDown;

  /// El servidor de encuentro no contesta, así que los dos botones de arriba
  /// van a fallar.
  ///
  /// Decirlo acá es el punto entero: crear y entrar fallan rápido sin registro,
  /// y sin este aviso eso se descubre después de elegir un juego y escribir
  /// ocho caracteres. No lleva acción porque no hay ninguna: se cura solo.
  final bool seedDown;

  /// Los avisos que mandó el daemon. Si no mandó ninguno no se pinta nada, que
  /// es el caso normal en una máquina sana.
  ///
  /// **No hay interruptor para esconderlos.** Lo hubo mientras la portada era
  /// una maqueta, y no puede seguir habiéndolo: un aviso que dice que el
  /// Firewall de Windows está apagado no es una sección decorativa que se
  /// pueda plegar.
  final List<HealthAlert> alerts;
  final ValueChanged<String> onCodeChanged;
  final ValueChanged<String> onRoomNameChanged;
  final VoidCallback onJoin;
  final VoidCallback onCreate;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        const AppKicker('Código de sala'),
        const SizedBox(height: 9),
        AppField(
          controller: code,
          shape: AppFieldShape.pill,
          mono: true,
          // La forma ENTERA en la pista, servidor incluido. Con `A7K2-M9QX`
          // sola, la pista enseñaba justo la mitad que no sirve para entrar a
          // ninguna parte.
          hint: 'A7K2-M9QX@servidor',
          // **Ni filtro de alfabeto ni tope de nueve, y eso era el fallo.** El
          // campo aceptaba `[a-zA-Z0-9-]` y cortaba a nueve caracteres, así que
          // de las seis formas que el daemon entiende no entraba ninguna: el
          // `@`, los puntos, las barras y los dos puntos se caían al escribir y
          // al PEGAR. `A7K2-M9QX@seed.ejemplo.com` quedaba en `A7K2-M9QX`
          // — medido — y un código pelado no vale para entrar, así que el botón
          // no se encendía nunca y no había forma de que se encendiera.
          //
          // El tope es el mismo que mira el daemon antes de tocar el contenido.
          // Interpretar lo pegado es de [InviteCode.parse], y decidir si vale,
          // del daemon.
          maxLength: InviteCode.maxInput,
          onChanged: onCodeChanged,
          onSubmitted: (_) => onJoin(),
          // La vuelta a la última sala va en el PIE del campo, pegada a él:
          // volver es entrar con un código que ya se tiene. Ver
          // [AppField.footer].
          footer: lastRoom == null
              ? null
              : LastRoomStrip(
                  last: lastRoom!,
                  onReturn: onReturnLast,
                  onForget: onForgetLast,
                ),
          trailing: AppButton(
            label: 'Unirse',
            height: 42,
            variant: AppButtonVariant.primaryFlat,
            // Ocupado mientras se resuelve qué hay detrás del código, que es
            // una vuelta por la red: sin esto el click quedaba segundos sin
            // respuesta y se repetía.
            busy: resolvingInvite,
            // Apagado hasta que el código esté completo. Dejarlo encendido
            // llevaría a una espera que ya se sabe que va a fallar.
            onPressed: canJoin ? onJoin : null,
          ),
        ),
        if (missingSeed) ...<Widget>[
          const SizedBox(height: AppSpacing.md),
          Text(
            'A ese código le falta el servidor: los mismos ocho caracteres son '
            'salas distintas en servidores distintos. Pega el enlace entero, o '
            'la forma CÓDIGO@servidor.',
            style: context.type.bodySm.copyWith(color: colors.textMuted),
          ),
        ],
        const SizedBox(height: AppSpacing.x3l),
        const AppLabeledDivider('o'),
        const SizedBox(height: AppSpacing.x3l),
        const AppKicker('Nueva sala'),
        const SizedBox(height: 9),
        AppField(
          controller: roomName,
          shape: AppFieldShape.pill,
          hint: nameHint,
          maxLength: 24,
          // **Sin esto el campo no era el borrador, era un campo suelto.** El
          // parámetro estaba declarado, se pasaba desde la portada y no lo
          // recibía nadie, así que lo escrito acá no salía de este widget: el
          // diálogo de confianza leía un borrador siempre vacío y enseñaba la
          // sugerencia, y quien acababa de escribir el nombre de su sala veía
          // otro. El sentido contrario sí estaba cableado, que es lo que hacía
          // el fallo difícil de ver: cambiarlo en el diálogo sí bajaba acá.
          onChanged: onRoomNameChanged,
          onSubmitted: (_) => onCreate(),
          trailing: AppButton(
            label: 'Crear sala',
            height: 42,
            variant: AppButtonVariant.primaryFlat,
            onPressed: onCreate,
          ),
        ),
        const SizedBox(height: AppSpacing.md),
        Text(
          'La sala se crea vacía: el juego se elige adentro y se puede cambiar.',
          textAlign: TextAlign.center,
          style: context.type.bodySm.copyWith(color: colors.textMuted),
        ),
        if (daemonDown) ...<Widget>[
          const SizedBox(height: AppSpacing.x5l),
          const AppMessageNotice(message: AppMessages.daemonDown),
        ] else ...<Widget>[
          // El registro va por delante de los avisos de salud, y detrás del
          // servicio caído. Es el orden de lo que impide usar Kanpachi: sin
          // servicio no hay nada, sin registro no se puede abrir ni entrar a
          // ninguna sala, y los avisos de salud describen una máquina que sí
          // funciona. Se pintan los dos cuando toca: hablan de cosas distintas.
          if (seedDown) ...<Widget>[
            const SizedBox(height: AppSpacing.x5l),
            const AppMessageNotice(message: AppMessages.seedDown),
          ],
          if (alerts.isNotEmpty) ...<Widget>[
            const SizedBox(height: AppSpacing.x5l),
            _HealthAlerts(alerts: alerts),
          ],
        ],
      ],
    );
  }
}

/// Los avisos de salud de la máquina.
///
/// Kanpachi los mira porque su promesa depende de ellos: se apoya en el
/// Firewall de Windows para que nadie de la sala alcance el resto de tu PC, y
/// no puede cumplirla si está apagado. Y avisa de un puerto abierto en el
/// router aunque no sea cosa suya, porque es justo lo que Kanpachi existe para
/// no tener que hacer.
///
/// **El texto no está acá y la lista tampoco.** El texto lo trae `AppMessages`,
/// que es el único sitio del programa donde se escribe copy de aviso, y la lista
/// la manda el daemon, que es el único que puede medir lo que hay. Esta clase
/// solo las apila.
///
/// El orden es el del daemon, que las produce por gravedad. Reordenarlas acá
/// sería que la pantalla opine sobre algo ya decidido donde se puede medir.
///
/// Se pintan **por la cadena del cable** y no por la clase, y esa elección es la
/// que impide perder un aviso: una clave que esta versión de la UI no conozca
/// sale igual con el mensaje de reserva y con el detalle del daemon pegado. Se
/// pierde el copy bueno, no se pierde el aviso.
class _HealthAlerts extends StatelessWidget {
  const _HealthAlerts({required this.alerts});

  final List<HealthAlert> alerts;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: <Widget>[
        for (int i = 0; i < alerts.length; i++) ...<Widget>[
          if (i > 0) const SizedBox(height: AppSpacing.lg),
          AppMessageNotice(
            message: AppMessages.alertFromWire(
              alerts[i].wire,
              detail: alerts[i].detail,
            ),
            titleStyle: context.type.strongSm,
            actions: alerts[i].kind == AlertKind.quarantineOff
                ? const <Widget>[CloseQuarantineButton()]
                : null,
          ),
        ],
      ],
    );
  }
}

/// Los juegos detectados en esta PC.
class _MyGames extends StatelessWidget {
  const _MyGames({
    required this.games,
    required this.artMode,
    required this.total,
  });

  final List<Game> games;
  final GameArtMode artMode;
  final int total;

  @override
  Widget build(BuildContext context) {
    final ShellCubit shell = context.read<ShellCubit>();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            const AppKicker('Tus juegos'),
            const Spacer(),
            // El mismo widget que usan el selector y la biblioteca. Estaba
            // duplicado en línea, así que arreglar los iconos en un sitio
            // dejaba los otros dos atrás.
            GameArtToggle(value: artMode, onChanged: shell.setArtMode),
          ],
        ),
        const SizedBox(height: 9),
        AppRowList(
          footer: AppRow(
            background: context.colors.surfaceSunken,
            onTap: () => shell.openGamePicker(fromRoom: false),
            child: Text(
              'Ver toda la biblioteca ($total)',
              style: context.type.label.copyWith(
                fontSize: 13,
                color: context.colors.accent,
              ),
            ),
          ),
          children: <Widget>[
            for (final Game game in games)
              AppRow(
                // Abre ESE juego, que es lo que un atajo significa.
                //
                // Llevaba al selector, o sea que pulsar Doom y pulsar «Ver toda
                // la biblioteca» hacían lo mismo: la lista de arriba estaba
                // pintada como si fuera accionable y no lo era, y quien la
                // usaba terminaba buscando en una lista larga el juego que
                // acababa de tocar. Es el MISMO camino que el selector, con la
                // misma confirmación de puertos detrás.
                onTap: () {
                  context.read<SessionCubit>().proposeGame(game);
                  shell.showDialog(AppDialog.confirmGame);
                },
                child: _GameRow(
                  game: game,
                  showCover: artMode == GameArtMode.cover,
                ),
              ),
          ],
        ),
      ],
    );
  }
}

class _GameRow extends StatelessWidget {
  const _GameRow({required this.game, required this.showCover});

  final Game game;
  final bool showCover;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      children: <Widget>[
        if (showCover) ...<Widget>[
          AppCover.thumb(imageUrl: SteamArt.portrait(game.steamAppId)),
          const SizedBox(width: AppSpacing.xl),
        ],
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                game.name,
                // 14 y no 13,5: la lista de la portada va un punto por encima
                // de las del selector y el catálogo, que sí son 13,5. Local a
                // esta fila, que es privada de la pantalla.
                style: context.type.label.copyWith(
                  fontSize: 14,
                  color: colors.text,
                ),
              ),
              Text(
                game.portsLabel,
                style: context.type.monoXs.copyWith(color: colors.textMuted),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
