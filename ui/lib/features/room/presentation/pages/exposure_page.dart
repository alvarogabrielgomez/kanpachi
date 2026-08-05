import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_spinner.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_notice.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/canary_alarm.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/probe_section.dart';
import 'package:kanpachi_ui/features/session/domain/entities/canary.dart';
import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// La pantalla de exposición, montada en el armazón de la app.
///
/// Existe aparte de [ExposurePage] para que aquella no sepa nada de navegación
/// ni de contenedores. Esa separación es la que hace que un test pueda pintarla
/// con un informe ciego sin levantar cubits, y es la razón por la que recibe la
/// medición por parámetro en vez de ir a buscarla.
///
/// Acá se resuelve lo que sí es del armazón: de dónde salen la medición y el
/// sondeo, y por dónde se vuelve.
class ExposureScreen extends StatelessWidget {
  const ExposureScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final SessionState session = context.watch<SessionCubit>().state;
    final SessionCubit cubit = context.read<SessionCubit>();
    final Room? room = session.room;

    return ScreenBody(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          ScreenHeader(
            title: 'Qué tiene abierto tu PC',
            note:
                'Medido en el sistema, no leído de lo que Kanpachi cree haber '
                'aplicado. Las dos cosas pueden no coincidir, y esa diferencia '
                'es lo que esta pantalla existe para enseñar.',
            leading: AppBackButton(
              onPressed: () => context.read<ShellCubit>().go(AppScreen.room),
            ),
          ),
          const SizedBox(height: AppSpacing.x4l),
          ExposurePage(
            load: cubit.exposure,
            probe: cubit.probeHost,
            // Sin sala se pinta igual, porque la exposición de la máquina es
            // cierta con sala y sin ella. Se cuenta como host para no ofrecer
            // un sondeo que no tiene a quién marcar.
            isHost: room?.selfIsHost ?? true,
            hostName: room?.hostName ?? '',
            alerts: session.health.kinds,
            canary: session.health.canary,
            onReapply: cubit.reapplyProtection,
            reapplying: session.isReapplying,
          ),
        ],
      ),
    );
  }
}

/// La pantalla que contesta "¿qué tiene abierto mi PC?".
///
/// # Las dos filas, y por qué la de abajo vale tanto como la lista
///
/// Arriba, lo que Kanpachi abrió y para quién. Abajo, que **todo lo demás del
/// adaptador virtual está cerrado**. La lista sola es cierta y engañosa a la
/// vez: enumera lo propio sin decir nada de la puerta de al lado, que es
/// justamente por donde entra el único agujero que este producto tiene que
/// impedir.
///
/// # Lo que esta pantalla NO hace
///
/// Cuando la medición falla, no enseña la última lista buena. Dice que Kanpachi
/// no pudo leer lo que tiene puesto y deja la lista fuera. Es la misma doctrina
/// que el aviso de auditoría caída: una lista vieja pintada de verde es peor que
/// una pantalla que admite no saber.
class ExposurePage extends StatefulWidget {
  const ExposurePage({
    required this.load,
    required this.probe,
    required this.isHost,
    this.hostName = '',
    this.alerts = const <AlertKind>[],
    this.canary = const CanaryCheck.blind(),
    this.onReapply,
    this.reapplying = false,
    super.key,
  });

  /// De dónde sale la medición. Entra por parámetro y no de un contenedor para
  /// que un test la pueda pintar con un informe ciego sin levantar nada.
  final Future<ExposureReport> Function() load;

  /// El sondeo desde la red, que es la única comprobación de esta pantalla que
  /// puede desmentir a la otra. Ver [ProbeSection].
  final Future<ProbeReport> Function() probe;

  /// En true, esta máquina hospeda y no puede sondearse a sí misma.
  final bool isHost;

  /// Cómo se llama el host, para el botón y para la fila.
  final String hostName;

  /// Las alertas vivas del daemon. La banda de la Protección Kanpachi se
  /// enciende por la del canario, jamás por el veredicto de [canary]. Ver
  /// [CanaryAlarm].
  final List<AlertKind> alerts;

  /// La última ronda, para el detalle de la banda.
  final CanaryCheck canary;

  /// Qué hace el botón de reponer. En null no se pinta la banda, y ese es el
  /// valor por defecto: una pantalla que ofrece reponer sin nadie que lo haga
  /// es un botón que miente.
  final Future<void> Function()? onReapply;

  final bool reapplying;

  @override
  State<ExposurePage> createState() => _ExposurePageState();
}

class _ExposurePageState extends State<ExposurePage> {
  ExposureReport? _report;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    setState(() => _loading = true);
    final ExposureReport r = await widget.load();
    if (!mounted) return;
    setState(() {
      _report = r;
      _loading = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    final ExposureReport? report = _report;
    final Future<void> Function()? reapply = widget.onReapply;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        // ENCIMA del encabezado, y es lo único de esta pantalla que puede
        // desmentir todo lo de abajo. La lista mide lo que esta PC tiene
        // configurado; la alarma dice que un paquete cruzó igual. Debajo se
        // leería después de haber creído la lista.
        if (reapply != null) ...<Widget>[
          CanaryAlarm(
            alerts: widget.alerts,
            check: widget.canary,
            busy: widget.reapplying,
            onReapply: reapply,
          ),
          if (widget.alerts.contains(AlertKind.canaryLeaking))
            const SizedBox(height: AppSpacing.lg),
        ],
        _Header(report: report, busy: _loading, onRefresh: _refresh),
        const SizedBox(height: AppSpacing.md),
        if (report == null)
          const Center(child: AppSpinner())
        else
          _Body(report: report),
        const SizedBox(height: AppSpacing.lg),
        // Va DEBAJO y no arriba a propósito. Lo de arriba es lo que esta PC
        // tiene configurado, y esto es lo que otra máquina alcanza de verdad:
        // leerlo en ese orden es leer primero la promesa y después la
        // comprobación.
        ProbeSection(
          run: widget.probe,
          isHost: widget.isHost,
          hostName: widget.hostName,
        ),
      ],
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({
    required this.report,
    required this.busy,
    required this.onRefresh,
  });

  final ExposureReport? report;
  final bool busy;
  final VoidCallback onRefresh;

  @override
  Widget build(BuildContext context) {
    final ExposureReport? r = report;
    final String when = r == null || !r.measured ? '' : r.measuredLabel;
    return Row(
      children: <Widget>[
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              const AppKicker('LO QUE ESTA PC TIENE ABIERTO'),
              if (when.isNotEmpty)
                Text(
                  'Medido a las $when',
                  style: context.type.bodySm.copyWith(
                    color: context.colors.textMuted,
                  ),
                ),
            ],
          ),
        ),
        AppButton(
          label: busy ? 'Midiendo…' : 'Volver a medir',
          onPressed: busy ? null : onRefresh,
          variant: AppButtonVariant.ghost,
        ),
      ],
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.report});

  final ExposureReport report;

  @override
  Widget build(BuildContext context) {
    // Ciego: el aviso y nada más. Sin lista, porque la única lista que se
    // podría enseñar acá es una que no se midió.
    if (!report.measured) {
      return AppMessageNotice(message: AppMessages.gate(report.gate));
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (report.ports.isEmpty)
          const AppNotice.line(
            tone: AppNoticeTone.neutral,
            body: Text(
              'Ahora mismo no hay ningún puerto abierto por Kanpachi.',
            ),
          )
        else
          AppRowList(
            children: <Widget>[
              for (final ExposedPort p in report.ports) _PortRow(port: p),
            ],
          ),
        const SizedBox(height: AppSpacing.md),
        AppMessageNotice(message: AppMessages.gate(report.gate)),
        if (report.unexpected.isNotEmpty) ...<Widget>[
          const SizedBox(height: AppSpacing.md),
          _UnexpectedNotice(names: report.unexpected),
        ],
      ],
    );
  }
}

class _PortRow extends StatelessWidget {
  const _PortRow({required this.port});

  final ExposedPort port;

  @override
  Widget build(BuildContext context) {
    return AppRow(
      child: Row(
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(port.label, style: context.type.body),
                Text(
                  _who(port),
                  style: context.type.bodySm.copyWith(
                    color: context.colors.textMuted,
                  ),
                ),
              ],
            ),
          ),
          if (!port.applied) const _NotApplied(),
        ],
      ),
    );
  }

  /// Para quién está abierto, en una línea.
  ///
  /// La lista vacía JAMÁS significa cualquiera: el daemon no puede expresar eso
  /// y una regla sin alcance remoto no llega a existir. Si llegara vacía, decir
  /// "nadie" es el lado seguro, porque el puerto no le sirve a nadie.
  static String _who(ExposedPort p) {
    final String quien = p.reachableBy.isEmpty
        ? 'nadie'
        : p.reachableBy.join(', ');
    return p.isControl ? 'Canal de la sala · $quien' : 'Abierto para $quien';
  }
}

/// La marca de un puerto que tenía que estar abierto y no lo está.
///
/// Es la fila que explica por qué un amigo se queda fuera, y sin ella el
/// síntoma es "a mí no me conecta" sin nada que mirar.
class _NotApplied extends StatelessWidget {
  const _NotApplied();

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: AppMessages.portNotApplied().body,
      child: Text(
        'no está puesto',
        style: context.type.bodySm.copyWith(color: context.colors.warn),
      ),
    );
  }
}

class _UnexpectedNotice extends StatelessWidget {
  const _UnexpectedNotice({required this.names});

  final List<String> names;

  @override
  Widget build(BuildContext context) {
    return AppNotice(
      title: 'Hay reglas de Kanpachi que Kanpachi no pidió',
      body: Text(
        'Kanpachi es el único que escribe en su grupo del firewall, así que '
        'estas sobran: ${names.join(', ')}. Suele ser resto de un cierre '
        'sucio, y salir de la sala y volver a entrar las quita.',
      ),
    );
  }
}
