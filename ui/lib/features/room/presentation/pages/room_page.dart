import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_cover.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_spinner.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_status_dot.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_notice.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/copy_button.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// La sala. Es la misma pantalla para el host y para el invitado.
///
/// Que sea la misma es deliberado: lo que cambia entre los dos no es la
/// información sino QUÉ pueden hacer con ella. Dos pantallas separadas
/// acabarían divergiendo, y el invitado dejaría de ver cosas que sí le
/// incumben — qué puertos están abiertos, quién está dentro, por dónde va su
/// tráfico.
class RoomScreen extends StatelessWidget {
  const RoomScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final SessionState session = context.watch<SessionCubit>().state;
    final Room? room = session.room;
    if (room == null) return const SizedBox.shrink();

    final DensityTokens d = context.density;

    return ScreenBody(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          _RoomHeader(room: room),
          SizedBox(height: d.gap),
          if (room.hostLeft && !room.selfIsHost) ...<Widget>[
            AppNotice(
              tone: AppNoticeTone.neutral,
              title: '${room.hostName ?? 'El host'} salió de la sala',
              body: const Text(
                'El juego corría en su PC, así que no hay a qué conectarse '
                'hasta que vuelva. La sala sigue en pie.',
              ),
            ),
            SizedBox(height: d.gap),
          ],
          if (room.network != NetworkHealth.ok) ...<Widget>[
            AppNotice.line(body: Text(room.network.message), pulse: true),
            SizedBox(height: d.gap),
          ],
          LayoutBuilder(
            builder: (BuildContext context, BoxConstraints constraints) {
              final bool wide = constraints.maxWidth >= 640;
              final Widget left = _RoomStatus(room: room, session: session);
              final Widget right = _RoomMembers(room: room);
              if (!wide) {
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    left,
                    SizedBox(height: d.gap),
                    right,
                  ],
                );
              }
              return Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Expanded(child: left),
                  const SizedBox(width: AppSpacing.x7l),
                  Expanded(child: right),
                ],
              );
            },
          ),
        ],
      ),
    );
  }
}

/// Nombre de la sala, código y las acciones del host.
class _RoomHeader extends StatefulWidget {
  const _RoomHeader({required this.room});

  final Room room;

  @override
  State<_RoomHeader> createState() => _RoomHeaderState();
}

class _RoomHeaderState extends State<_RoomHeader> {
  bool _editing = false;
  late final TextEditingController _name =
      TextEditingController(text: widget.room.name);

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  void _commit() {
    final String value = _name.text.trim();
    if (value.isNotEmpty) context.read<SessionCubit>().rename(value);
    setState(() => _editing = false);
  }

  @override
  Widget build(BuildContext context) {
    final Room room = widget.room;

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
                child: _editing
                    ? _NameEditor(controller: _name, onCommit: _commit)
                    : _NameDisplay(
                        name: room.name,
                        canEdit: room.selfIsHost,
                        onEdit: () => setState(() => _editing = true),
                      ),
              ),
            ],
          ),
        ),
        Wrap(
          spacing: AppSpacing.md,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            AppChip(room.code),
            CopyButton(
              label: 'Copiar enlace',
              height: 36,
              value: 'https://kanpachi.accentio.dev/${room.code}',
            ),
            if (room.selfIsHost)
              AppButton(
                label: 'Renovar código',
                variant: AppButtonVariant.ghost,
                height: 36,
                onPressed: () => context
                    .read<ShellCubit>()
                    .showDialog(AppDialog.confirmRenew),
              ),
          ],
        ),
      ],
    );
  }
}

class _NameDisplay extends StatelessWidget {
  const _NameDisplay({
    required this.name,
    required this.canEdit,
    required this.onEdit,
  });

  final String name;
  final bool canEdit;
  final VoidCallback onEdit;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Text(
          name,
          style: context.type.titleLg.copyWith(color: colors.text),
        ),
        if (canEdit) ...<Widget>[
          const SizedBox(width: AppSpacing.sm),
          AppIconButton(
            icon: Icons.edit_outlined,
            tooltip: 'Renombrar la sala',
            width: 30,
            height: 30,
            iconSize: 15,
            danger: true,
            onPressed: onEdit,
          ),
        ],
      ],
    );
  }
}

class _NameEditor extends StatelessWidget {
  const _NameEditor({required this.controller, required this.onCommit});

  final TextEditingController controller;
  final VoidCallback onCommit;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 360,
      child: AppField(
        controller: controller,
        maxLength: 24,
        autofocus: true,
        textStyle: context.type.titleLg,
        onSubmitted: (_) => onCommit(),
        trailing: AppIconButton(
          icon: Icons.check,
          tooltip: 'Guardar',
          width: 32,
          height: 32,
          iconSize: 15,
          danger: true,
          onPressed: onCommit,
        ),
      ),
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
      blocks.add(_ApplyingCard(work: session.work, game: session.pendingGame?.name));
    } else if (room.game == null) {
      blocks.add(_NoGameCard(host: host, hostName: room.hostName));
      blocks.add(const _ExposureCard.nothingOpen());
    } else {
      blocks.add(_GameCard(room: room));
      blocks.add(_ExposureCard(room: room));
      if (room.foreignRule == ForeignRuleState.open) {
        blocks.add(_ForeignRuleNotice(gameName: room.game!.name));
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
                  style: context.type.labelLg
                      .copyWith(color: colors.text, fontWeight: FontWeight.w600),
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
            style: context.type.labelLg
                .copyWith(color: colors.text, fontWeight: FontWeight.w600),
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
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              const AppCover.room(),
              const SizedBox(width: AppSpacing.xl),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      room.game!.name,
                      style: context.type.gameName.copyWith(color: colors.text),
                    ),
                    Text(
                      host
                          ? 'Juego activo · lo hospedas tú'
                          : 'Juego activo · host: ${room.hostName ?? '—'}',
                      style:
                          context.type.bodySm.copyWith(color: colors.textMuted),
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
                  small: true,
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
          ),
        ],
      ),
    );
  }
}

/// Qué está abierto y hacia quién. Es el bloque por el que existe la app.
class _ExposureCard extends StatelessWidget {
  const _ExposureCard({required this.room}) : _empty = false;

  const _ExposureCard.nothingOpen()
      : room = null,
        _empty = true;

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
                  text: '${r.game!.portsLabel}, visible para '
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
        ],
      ),
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

/// La regla que dejó el propio juego en el firewall.
///
/// Es el aviso más importante de la app: con ella puesta, el juego es
/// alcanzable desde la red de casa y desde toda la sala SIN pasar por el
/// control de Kanpachi, así que expulsar a alguien no lo tapa. Kanpachi no la
/// borra por su cuenta — no es suya — pero sí ofrece desactivarla mientras
/// dure la partida y devolverla al salir.
class _ForeignRuleNotice extends StatelessWidget {
  const _ForeignRuleNotice({required this.gameName});

  final String gameName;

  @override
  Widget build(BuildContext context) {
    final SessionCubit session = context.read<SessionCubit>();
    return AppNotice(
      title: null,
      body: Text.rich(
        TextSpan(
          children: <InlineSpan>[
            TextSpan(
              text: 'El propio $gameName dejó una regla en tu firewall. ',
              style: TextStyle(
                color: context.colors.text,
                fontWeight: FontWeight.w600,
              ),
            ),
            const TextSpan(
              text: 'Con ella el juego es alcanzable desde tu red de casa y '
                  'por toda la sala, sin pasar por el control de Kanpachi: '
                  'expulsar a alguien no lo tapa. Podemos desactivarla '
                  'mientras juegas y devolverla al salir.',
            ),
          ],
        ),
      ),
      actions: <Widget>[
        AppButton(
          label: 'Desactivar mientras juego',
          variant: AppButtonVariant.primaryFlat,
          height: 34,
          onPressed: () => session.resolveForeignRule(disable: true),
        ),
        AppButton(
          label: 'Dejar así',
          variant: AppButtonVariant.ghost,
          height: 34,
          onPressed: () => session.resolveForeignRule(disable: false),
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
          children: <Widget>[
            for (final Member member in room.members)
              AppRow(child: _MemberRow(member: member, host: room.selfIsHost)),
          ],
        ),
        const SizedBox(height: AppSpacing.xxl),
        AppButton(
          label: 'Salir de la sala',
          variant: AppButtonVariant.quiet,
          height: 46,
          onPressed: () {
            context.read<SessionCubit>().leave();
            context.read<ShellCubit>().go(AppScreen.home);
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
            onPressed: () => context.read<ShellCubit>().askKick(member),
          ),
      ],
    );
  }
}
