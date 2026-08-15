import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_glyphs.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_cover.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_spinner.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_status_dot.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_editable_name.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_notice.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/games/domain/steam_art.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/canary_alarm.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/copy_button.dart';
import 'package:kanpachi_ui/features/session/domain/entities/action_failure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/failure_notice.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/failure_navigation.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// La sala. Es la misma pantalla para el host y para el invitado.
///
/// Que sea la misma es deliberado: lo que cambia entre los dos no es la
/// información sino QUÉ pueden hacer con ella. Dos pantallas separadas
/// acabarían divergiendo, y el invitado dejaría de ver cosas que sí le
/// incumben — qué puertos están abiertos, quién está dentro, por dónde va su
/// tráfico.
class RoomScreen extends StatefulWidget {
  const RoomScreen({super.key});

  @override
  State<RoomScreen> createState() => _RoomScreenState();
}

class _RoomScreenState extends State<RoomScreen> {
  @override
  void initState() {
    super.initState();
    // Al ENTRAR se vuelve a preguntar, **y esta es la única pantalla que pide
    // además la auditoría del firewall**.
    //
    // Quién está dentro lo trae el latido cada dos segundos, y es barato. La
    // auditoría de reglas ajenas no lo es: barre el almacén de reglas entero de
    // Windows por COM, así que se pide donde su aviso se lee y no sesenta veces
    // por minuto en todas partes. Los otros tres disparadores viven en el
    // repositorio, que es quien sabe si cambió el juego o si entró alguien.
    //
    // Va tras el primer fotograma porque emitir estado durante el montaje del
    // widget es pedirle a Flutter que reconstruya algo que todavía se está
    // construyendo.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) context.read<SessionCubit>().recheckForeignRules();
    });
  }

  @override
  Widget build(BuildContext context) {
    final SessionState session = context.watch<SessionCubit>().state;
    final Room? room = session.room;
    if (room == null) return const SizedBox.shrink();

    final DensityTokens d = context.density;

    // Sin los fallos que ya llevaron a su pantalla, igual que en la portada:
    // renovar el código con la credencial vencida navega a escribir la
    // contraseña, y el aviso repetiría acá lo que esa pantalla está pidiendo.
    final ActionFailure? failure =
        screenForFailure(session.failure?.code) == null
        ? session.failure
        : null;

    // **Los avisos bajaron al panel izquierdo, y ya no van de borde a borde.**
    //
    // Antes ocupaban el ancho entero por encima de las dos columnas, así que
    // con dos puestos a la vez la sala empezaba con los miembros fuera de
    // pantalla. Ahora se apilan en su columna y se recorren ahí, con la lista de
    // quién está dentro quieta al lado.
    return ScreenPanels(
      gap: AppSpacing.x7l,
      header: _RoomHeader(room: room),
      left: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          // Encima de todo lo demás, y por delante incluso del host que se fue.
          // El usuario puede no abrir jamás la pantalla de exposición, y una
          // protección que dejó de contener no puede esperar a que vaya a
          // buscarla. La banda se pinta sola cuando no hay alarma.
          if (session.health.kinds.contains(AlertKind.gateLeaking)) ...<Widget>[
            CanaryAlarm(
              alerts: session.health.kinds,
              check: session.health.canary,
              busy: session.isReapplying,
              onReapply: context.read<SessionCubit>().reapplyProtection,
            ),
            SizedBox(height: d.gap),
          ],
          // Detrás de la alarma y delante del resto: la alarma habla de una
          // protección que dejó de contener, esto de una orden que el usuario
          // acaba de dar y no pasó.
          if (failure != null) ...<Widget>[
            FailureNotice(
              failure: failure,
              verbose: session.verbose,
              onDismiss: () => context.read<SessionCubit>().clearFailure(),
            ),
            SizedBox(height: d.gap),
          ],
          if (room.codeLost && room.selfIsHost) ...<Widget>[
            const _CodeLostNotice(),
            SizedBox(height: d.gap),
          ],
          // El reingreso le gana al cartel del host ausente, y no se muestran
          // los dos: el reingreso EXPLICA la pausa, y decir a la vez que el host
          // no está es la contradicción que se veía en pantalla, porque cuando
          // lo dispara el aviso del host, el host sí está.
          if (room.rejoining && !room.selfIsHost) ...<Widget>[
            const AppMessageNotice(message: AppMessages.rejoining, pulse: true),
            SizedBox(height: d.gap),
          ] else if (room.hostLeft && !room.selfIsHost) ...<Widget>[
            AppMessageNotice(message: AppMessages.hostLeft(room.hostName)),
            SizedBox(height: d.gap),
          ],
          if (!AppMessages.connection(room.network).isEmpty) ...<Widget>[
            AppMessageNotice(
              message: AppMessages.connection(room.network),
              pulse: true,
            ),
            SizedBox(height: d.gap),
          ],
          _RoomStatus(room: room, session: session),
        ],
      ),
      right: _RoomMembers(room: room),
    );
  }
}

/// Los dos altos de la fila de acciones de la cabecera, del diseño.
///
/// La píldora del código va DOS píxeles más baja que los botones, y no es un
/// descuido de la maqueta: lleva menos aire vertical (9 contra 10) porque es un
/// dato y no una acción, y de paso hace que los botones pesen un punto más en
/// la fila. Salen de sumar el padding a la línea de 12,5.
const double _chipHeight = 31;
const double _pillHeight = 33;

/// Nombre de la sala, código y las acciones del host.
class _RoomHeader extends StatefulWidget {
  const _RoomHeader({required this.room});

  final Room room;

  @override
  State<_RoomHeader> createState() => _RoomHeaderState();
}

class _RoomHeaderState extends State<_RoomHeader> {
  late final TextEditingController _name = TextEditingController(
    text: widget.room.name,
  );

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final Room room = widget.room;

    return LayoutBuilder(
      builder: (BuildContext context, BoxConstraints c) {
        // Las acciones no pasan de poco más de la mitad: por debajo de eso
        // envuelven, y así el título nunca se queda sin sitio.
        final double anchoDeAcciones = c.maxWidth * 0.55;
        return Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  const AppKicker('Sala'),
                  SizedBox(
                    height: 44,
                    child: AppEditableName(
                      controller: _name,
                      canEdit: room.selfIsHost,
                      editTooltip: 'Renombrar la sala',
                      onCommit: (String v) =>
                          context.read<SessionCubit>().rename(v),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(width: AppSpacing.x5l),
            // Acotado, y fuera del reparto flex. Un `Wrap` que va de hijo
            // no-flexible de un `Row` se mide con ancho infinito, así que NUNCA
            // envuelve: se queda en una línea, se lleva todo el ancho y deja al
            // título con 19 px. Con un tope sí envuelve, y al no ser flexible se
            // queda en su ancho natural en vez de comerse la mitad de la fila y
            // dejar un hueco entre el título y los botones.
            ConstrainedBox(
              constraints: BoxConstraints(maxWidth: anchoDeAcciones),
              child: Wrap(
                spacing: AppSpacing.md,
                runSpacing: AppSpacing.md,
                alignment: WrapAlignment.end,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: <Widget>[
                  // **El código ES el botón de copiarlo.** Antes eran dos cosas
                  // —una píldora de sólo lectura y, más allá, un botón— y eso
                  // dejaba el gesto obvio (pulsar el código) sin hacer nada. El
                  // icono apagado dice que se puede pulsar sin robarle la
                  // primera lectura al código.
                  CopyButton(
                    // Con su registro pegado, y lo copiado es lo mismo que se
                    // lee. Ver [Room.displayCode]: pelado no identifica una
                    // sala, identifica ocho caracteres que existen en tantas
                    // salas como registros haya.
                    label: room.displayCode,
                    copiedLabel: 'Código copiado',
                    value: room.displayCode,
                    variant: AppButtonVariant.data,
                    height: _chipHeight,
                    horizontalPadding: AppSpacing.xxl,
                    icon: const CopyGlyph(),
                    iconGap: AppSpacing.md,
                    iconAlpha: 0.75,
                  ),
                  // **Acá había un «Actualizar», y se fue.**
                  //
                  // Existía cuando no había temporizador en ninguna capa: sin
                  // él, la única forma de ver quién había entrado era salir de
                  // la pantalla y volver. Eso dejó de ser cierto con el latido
                  // de dos segundos —ver [SessionCubit.watchSession]—, que ya
                  // trae la sala, la salud y los miembros solo. Lo que quedaba
                  // era un botón que adelantaba como mucho dos segundos algo
                  // que iba a pasar igual, ocupando el sitio de las dos
                  // acciones que sí hacen algo. El diseño tampoco lo tiene.
                  //
                  // `SessionCubit.refresh()` sigue viva: la usa el propio
                  // latido y la portada al entrar.
                  //
                  // El enlace viene armado del daemon, con el seed de ESTA sala
                  // y la clave de la tarjeta en el fragmento. Ver [Room.link].
                  if (room.link.isNotEmpty)
                    CopyButton(
                      label: 'Copiar enlace',
                      height: _pillHeight,
                      horizontalPadding: AppSpacing.x3l,
                      textStyle: context.type.labelSm.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                      icon: const LinkGlyph(),
                      iconGap: AppSpacing.md,
                      value: room.link,
                    ),
                  if (room.selfIsHost) const _RenewCodeButton(),
                ],
              ),
            ),
          ],
        );
      },
    );
  }
}

/// «Renovar código», con su rueda mientras el registro contesta.
///
/// En su propia clase, y `const`, para que mire la sesión por su cuenta: la
/// cabecera de la sala donde vive se dibuja una vez y no tiene por qué
/// reconstruirse dos veces por segundo. Ver [AppButton.busy] para por qué el
/// botón se apaga además de girar.
class _RenewCodeButton extends StatelessWidget {
  const _RenewCodeButton();

  @override
  Widget build(BuildContext context) {
    final bool renovando = context.watch<SessionCubit>().state.isRenewingCode;
    return AppButton(
      label: renovando ? 'Renovando…' : 'Renovar código',
      variant: AppButtonVariant.ghost,
      height: 36,
      horizontalPadding: AppSpacing.x3l,
      textStyle: context.type.labelSm,
      busy: renovando,
      onPressed: () =>
          context.read<ShellCubit>().showDialog(AppDialog.confirmRenew),
    );
  }
}

/// La columna izquierda: qué juego hay, qué está abierto y la regla ajena.
class _RoomStatus extends StatelessWidget {
  const _RoomStatus({required this.room, required this.session});

  final Room room;
  final SessionState session;

  @override
  Widget build(BuildContext context) {
    final DensityTokens d = context.density;
    final bool host = room.selfIsHost;
    final List<Widget> blocks = <Widget>[];

    if (session.isBusy) {
      blocks.add(
        _ApplyingCard(work: session.work, game: session.pendingGame?.name),
      );
    } else if (room.game == null) {
      blocks.add(_NoGameCard(host: host, hostName: room.hostName));
      blocks.add(const _ExposureCard.nothingOpen());
    } else {
      blocks.add(_GameCard(room: room));
      blocks.add(_ExposureCard(room: room));
      if (room.foreignRule == ForeignRuleState.open) {
        blocks.add(
          _ForeignRuleNotice(
            kind: room.foreignRuleClass,
            gameName: room.game!.name,
            program: room.foreignRuleProgram,
          ),
        );
      } else if (room.foreignRule == ForeignRuleState.disabled) {
        blocks.add(
          const AppNotice.line(
            tone: AppNoticeTone.done,
            body: Text('Regla desactivada. Se restaura al salir de la sala.'),
          ),
        );
      }
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        for (int i = 0; i < blocks.length; i++) ...<Widget>[
          blocks[i],
          if (i != blocks.length - 1) SizedBox(height: d.gap),
        ],
      ],
    );
  }
}

class _ApplyingCard extends StatelessWidget {
  const _ApplyingCard({required this.work, required this.game});

  final RoomWork work;
  final String? game;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool closing = work == RoomWork.closingGame;
    return AppCard(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.x4l,
        vertical: AppSpacing.x6l,
      ),
      child: Row(
        children: <Widget>[
          const AppSpinner(size: 26, stroke: 2.5),
          const SizedBox(width: AppSpacing.xxl),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(
                  closing
                      ? 'Cerrando el juego…'
                      : 'Aplicando ${game ?? 'el juego'}…',
                  style: context.type.labelLg.copyWith(
                    color: colors.text,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: AppSpacing.xs),
                Text(
                  closing
                      ? 'Cerrando sus puertos. La sala sigue en pie.'
                      : 'Abriendo sus puertos hacia los miembros de la sala.',
                  style: context.type.bodySm.copyWith(color: colors.textMuted),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _NoGameCard extends StatelessWidget {
  const _NoGameCard({required this.host, required this.hostName});

  final bool host;
  final String? hostName;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return AppCard(
      dashed: true,
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.x4l,
        vertical: AppSpacing.x5l,
      ),
      child: Column(
        children: <Widget>[
          Text(
            'Sala sin juego',
            style: context.type.labelLg.copyWith(
              color: colors.text,
              fontWeight: FontWeight.w600,
            ),
          ),
          const SizedBox(height: 7),
          Text(
            'El juego se elige aquí y se puede cambiar cuando quieran.',
            textAlign: TextAlign.center,
            style: context.type.body.copyWith(color: colors.textMuted),
          ),
          const SizedBox(height: AppSpacing.x3l),
          if (host)
            AppButton(
              label: 'Elegir juego',
              onPressed: () =>
                  context.read<ShellCubit>().openGamePicker(fromRoom: true),
            )
          else
            Text(
              '${hostName ?? 'El host'} todavía no eligió juego.',
              style: context.type.label.copyWith(color: colors.textOnChip),
            ),
        ],
      ),
    );
  }
}

class _GameCard extends StatelessWidget {
  const _GameCard({required this.room});

  final Room room;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool host = room.selfIsHost;
    return AppCard(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.x4l,
        vertical: AppSpacing.x3l,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          // La X va arriba (es el `align-self:flex-start` del diseño) pero la
          // portada y sus dos líneas van centradas entre sí. Por eso el par va
          // en su propio Row centrado dentro del Row externo, en vez de un
          // `Align`: un Align aquí cuelga de un scroll sin alto acotado, hace
          // shrink-wrap y su alineación no se nota.
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Expanded(
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.center,
                  children: <Widget>[
                    AppCover.room(
                      imageUrl: SteamArt.portrait(room.game?.steamAppId),
                    ),
                    const SizedBox(width: AppSpacing.xl),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: <Widget>[
                          Text(
                            room.game!.name,
                            style: context.type.gameName.copyWith(
                              color: colors.text,
                            ),
                          ),
                          Text(
                            host
                                ? 'Juego activo · lo hospedas tú'
                                : 'Juego activo · host: ${room.hostName ?? '—'}',
                            style: context.type.bodySm.copyWith(
                              color: colors.textMuted,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
              if (host)
                AppIconButton(
                  icon: Icons.close,
                  tooltip: 'Quitar el juego de la sala',
                  width: 28,
                  height: 28,
                  iconSize: 13,
                  danger: true,
                  outlined: true,
                  onPressed: () => context.read<SessionCubit>().closeGame(),
                ),
            ],
          ),
          const SizedBox(height: AppSpacing.xl),
          _AddressBox(room: room),
          if (host) ...<Widget>[
            const SizedBox(height: AppSpacing.xl),
            Align(
              alignment: Alignment.centerLeft,
              child: AppButton(
                label: 'Cambiar juego',
                variant: AppButtonVariant.ghost,
                height: 34,
                horizontalPadding: 15,
                textStyle: context.type.labelSm,
                onPressed: () =>
                    context.read<ShellCubit>().openGamePicker(fromRoom: true),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// La dirección que se pega dentro del juego.
///
/// El rótulo cambia con el papel: el host la reparte, el invitado la usa. Es
/// el mismo dato y dos verbos distintos, y decir el equivocado manda a la
/// mitad de la sala a hacer lo que no toca.
class _AddressBox extends StatelessWidget {
  const _AddressBox({required this.room});

  final Room room;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.xxl,
        vertical: AppSpacing.xl,
      ),
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        borderRadius: AppRadius.allMd,
      ),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                AppKicker(
                  room.selfIsHost ? 'Pásales esta dirección' : 'Conéctate a',
                  size: AppKickerSize.xs,
                ),
                const SizedBox(height: 5),
                Text(
                  room.gameAddress,
                  style: context.type.mono.copyWith(color: colors.text),
                ),
              ],
            ),
          ),
          CopyButton(
            label: 'Copiar',
            value: room.gameAddress.replaceAll(' ', ''),
            variant: AppButtonVariant.quiet,
            height: 34,
            horizontalPadding: 15,
            textStyle: context.type.labelSm,
          ),
        ],
      ),
    );
  }
}

/// Qué está abierto y hacia quién. Es el bloque por el que existe la app.
class _ExposureCard extends StatelessWidget {
  const _ExposureCard({required this.room}) : _empty = false;

  const _ExposureCard.nothingOpen() : room = null, _empty = true;

  final Room? room;
  final bool _empty;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    if (_empty) {
      return AppCard(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.x4l,
          vertical: AppSpacing.x3l,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            _ExposureTitle(color: colors.textMuted, text: 'Nada abierto'),
            const SizedBox(height: 9),
            Text(
              'La red está en cuarentena. Nadie tiene puertos abiertos.',
              style: context.type.body.copyWith(color: colors.textOnChip),
            ),
            const _VerLoMedido(),
          ],
        ),
      );
    }

    final Room r = room!;
    return AppCard(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.x4l,
        vertical: AppSpacing.x3l,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          _ExposureTitle(
            color: colors.ok,
            text: 'Abierto solo dentro de Kanpachi',
          ),
          const SizedBox(height: 9),
          Text.rich(
            TextSpan(
              children: <InlineSpan>[
                TextSpan(
                  text:
                      '${r.game!.portsLabel}, visible para '
                      '${r.members.length} personas.\n',
                ),
                TextSpan(
                  text: 'Tu router sigue cerrado. Internet no ve nada.',
                  style: TextStyle(
                    color: colors.text,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ),
            style: context.type.body.copyWith(color: colors.textOnChip),
          ),
          const SizedBox(height: AppSpacing.xl),
          Divider(color: colors.border, height: 1),
          const SizedBox(height: AppSpacing.xl),
          Text(
            r.selfIsHost
                ? 'El juego corre en tu PC. Los demás solo alcanzan estos '
                      'puertos, nada más de tu máquina.'
                : 'Solo te conectas con el host. Los demás jugadores no '
                      'alcanzan tu PC.',
            style: context.type.bodySm.copyWith(color: colors.textMuted),
          ),
          const _VerLoMedido(),
        ],
      ),
    );
  }
}

/// El enlace a la medición de verdad.
///
/// Va en las DOS caras de la tarjeta, incluida la de "nada abierto", y eso es
/// deliberado: esta tarjeta dice lo que Kanpachi PIDIÓ, y la pantalla de al lado
/// dice lo que el sistema TIENE. La diferencia entre las dos es justo lo que hay
/// que poder mirar, y "nada abierto" es la afirmación que más falta hace poder
/// desmentir.
class _VerLoMedido extends StatelessWidget {
  const _VerLoMedido();

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        const SizedBox(height: AppSpacing.xl),
        Divider(color: context.colors.border, height: 1),
        const SizedBox(height: AppSpacing.md),
        Align(
          alignment: Alignment.centerLeft,
          child: AppButton(
            label: 'Ver lo que tu PC tiene abierto',
            height: 38,
            variant: AppButtonVariant.ghost,
            onPressed: () => context.read<ShellCubit>().go(AppScreen.exposure),
          ),
        ),
      ],
    );
  }
}

class _ExposureTitle extends StatelessWidget {
  const _ExposureTitle({required this.color, required this.text});

  final Color color;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: <Widget>[
        AppStatusDot(color: color),
        const SizedBox(width: 9),
        Flexible(
          child: Text(
            text,
            style: context.type.strongSm.copyWith(color: context.colors.text),
          ),
        ),
      ],
    );
  }
}

/// El registro perdió la entrada de una sala que sigue funcionando.
///
/// El botón renueva solo el código: conserva la red real, el juego y a todos
/// los que ya están dentro. Crear otra sala para llegar al mismo resultado
/// cortaría la partida sin necesidad.
class _CodeLostNotice extends StatelessWidget {
  const _CodeLostNotice();

  @override
  Widget build(BuildContext context) {
    return AppMessageNotice(
      message: AppMessages.codeLost,
      actions: <Widget>[
        AppButton(
          label: 'Renovar el código',
          variant: AppButtonVariant.primaryFlat,
          height: 34,
          horizontalPadding: 15,
          onPressed: context.read<SessionCubit>().renewCode,
        ),
      ],
    );
  }
}

/// La regla de firewall que Kanpachi NO creó.
///
/// Es el aviso más importante de la app, y desde que el daemon clasifica lo que
/// encuentra son DOS avisos y no uno.
///
/// Con una regla del juego puesta, el juego es alcanzable desde la red de casa
/// y desde toda la sala sin pasar por el control de Kanpachi, así que expulsar
/// a alguien no lo tapa. Molesta, se ofrece arreglar, y se puede dejar así.
///
/// Con una de CONTROL REMOTO puesta, lo alcanzable no es el juego: es el
/// escritorio. Quien tenga el código entra a la sala, y el código no es un
/// secreto ni hay baneo. Esa no se puede dejar así, y por eso el segundo botón
/// no dice "dejar así" sino que cambia de juego: la sala no se abre con eso
/// puesto.
///
/// El copy vive en el catálogo y no acá. Antes estaba escrito dentro de este
/// widget, que es justo lo que el catálogo existe para impedir.
class _ForeignRuleNotice extends StatelessWidget {
  const _ForeignRuleNotice({required this.kind, this.gameName, this.program});

  final RuleClass kind;
  final String? gameName;

  /// El ejecutable al que apunta la regla. Va como detalle y jamás sustituye al
  /// cuerpo del mensaje.
  final String? program;

  @override
  Widget build(BuildContext context) {
    final SessionCubit session = context.read<SessionCubit>();
    final bool blocks =
        kind == RuleClass.remoteControl || kind == RuleClass.onOurAdapter;
    // Escribir en el firewall por COM tarda alrededor de un segundo. Sin esto
    // el botón se ve muerto todo ese rato, y lo que hace la gente entonces es
    // pulsarlo otra vez.
    final bool trabajando =
        context.watch<SessionCubit>().state.work == RoomWork.resolvingForeign;

    return AppMessageNotice(
      message: AppMessages.foreignRule(
        kind,
        gameName: gameName,
        detail: program,
      ),
      pulse: blocks,
      actions: <Widget>[
        AppButton(
          label: trabajando
              ? 'Desactivando…'
              : (blocks
                    ? 'Desactivarlo mientras juego'
                    : 'Desactivar mientras juego'),
          variant: AppButtonVariant.primaryFlat,
          height: 34,
          horizontalPadding: 15,
          onPressed: trabajando
              ? null
              : () => session.resolveForeignRule(disable: true),
        ),
        // La salida de la de control remoto NO es equivalente a la del juego.
        // "Dejar así" abriría la sala con el escritorio del host alcanzable por
        // cualquiera que tenga el código, así que ahí la única salida sin
        // desactivar nada es no abrirla.
        if (!blocks)
          AppButton(
            label: 'Dejar así',
            variant: AppButtonVariant.ghost,
            height: 34,
            horizontalPadding: 15,
            onPressed: trabajando
                ? null
                : () => session.resolveForeignRule(disable: false),
          ),
      ],
    );
  }
}

/// Quién está dentro, por dónde va su tráfico y cuánto tarda.
class _RoomMembers extends StatelessWidget {
  const _RoomMembers({required this.room});

  final Room room;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        AppKicker('En la sala · ${room.members.length}'),
        const SizedBox(height: 9),
        AppRowList(
          // 16 y no 14: es la lista más grande de la app y con el radio de las
          // listas de juegos se lee apretada.
          radius: AppRadius.allXl,
          children: <Widget>[
            for (final Member member in room.members)
              AppRow(
                child: _MemberRow(member: member, host: room.selfIsHost),
              ),
          ],
        ),
        const SizedBox(height: AppSpacing.xxl),
        // **El host CIERRA, el invitado SALE, y no es la misma acción.**
        //
        // El botón decía "Salir de la sala" para los dos, y para el host era
        // mentira por omisión: cuando el host se va, la sala se acaba para
        // todos. El daemon ya lo hacía —avisa a cada miembro, cierra los
        // puertos, restaura las reglas ajenas, suelta la compuerta, revierte
        // los ajustes del adaptador, cierra el canal y baja la red— y lo único
        // que faltaba era decirlo antes de hacerlo.
        AppButton(
          label: room.selfIsHost ? 'Cerrar la sala' : 'Salir de la sala',
          variant: AppButtonVariant.quiet,
          height: 44,
          textStyle: context.type.strong,
          onPressed: () {
            // Con gente dentro se pregunta, y solo entonces. Un host solo en
            // su sala cerrándola no le está haciendo nada a nadie, y un
            // diálogo ahí es una pulsación de más en el caso normal.
            if (room.selfIsHost && room.members.length > 1) {
              context.read<ShellCubit>().showDialog(AppDialog.confirmClose);
              return;
            }
            // Sin navegar acá: cuando la sala se vaya de verdad, el oyente del
            // marco lleva a la portada y poda el camino que llevaba a ella. Un
            // «salir» que falla tiene que dejar al usuario en su sala.
            context.read<SessionCubit>().leave();
          },
        ),
      ],
    );
  }
}

class _MemberRow extends StatelessWidget {
  const _MemberRow({required this.member, required this.host});

  final Member member;
  final bool host;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final Color dot = switch (member.path) {
      PeerPath.self => colors.textMuted,
      PeerPath.relay => colors.warn,
      PeerPath.direct => colors.ok,
      // Apagado y no ámbar: ámbar es relay, o sea "la red va lenta", y de este
      // miembro lo que no se sabe es por dónde llega, no que llegue mal.
      PeerPath.unconfirmed => colors.textMuted,
    };
    return Row(
      children: <Widget>[
        AppStatusDot(color: dot),
        const SizedBox(width: AppSpacing.lg),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                member.displayName,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: context.type.label.copyWith(color: colors.text),
              ),
              Text(
                member.meta,
                style: context.type.monoXxs.copyWith(color: colors.textMuted),
              ),
            ],
          ),
        ),
        // Expulsar sólo lo ve el host, y nunca sobre sí mismo.
        if (host && !member.isSelf)
          AppButton(
            label: 'Expulsar',
            variant: AppButtonVariant.ghost,
            height: 28,
            horizontalPadding: 11,
            textStyle: context.type.labelSm.copyWith(fontSize: 11.5),
            onPressed: () => context.read<ShellCubit>().askKick(member),
          ),
      ],
    );
  }
}
