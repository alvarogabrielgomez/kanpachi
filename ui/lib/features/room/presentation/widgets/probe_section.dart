import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_spinner.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_notice.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';

/// La comprobación que sale a la red, y la única que puede desmentir al resto
/// de la pantalla.
///
/// # Por qué vale más que la lista de arriba
///
/// Porque la lista lee lo que ESTA PC tiene configurado, preguntándole a la
/// misma Windows que aplica las reglas. Un filtro que dejó de casar, una regla
/// del sistema que nadie miró, o un programa que abrió su propio hueco, se ven
/// todos igual desde dentro: verde. Esto marca de verdad, desde otra máquina.
///
/// # Las dos caras, y por qué el host no tiene botón
///
/// El invitado marca al host y ve el resultado. El host no se puede sondear a sí
/// mismo: el tráfico a la propia dirección no atraviesa ningún firewall, así que
/// diría que está todo abierto en una máquina blindada. Su cara de esta sección
/// es la frase que necesita, que es pedírselo a alguien.
class ProbeSection extends StatefulWidget {
  const ProbeSection({
    required this.run,
    required this.isHost,
    this.hostName = '',
    super.key,
  });

  /// Corre el sondeo. Entra por parámetro y no de un contenedor para que un
  /// test pueda pintar una fuga sin levantar nada.
  final Future<ProbeReport> Function() run;

  /// En true, esta máquina hospeda y no puede correrlo.
  final bool isHost;

  /// Cómo se llama el host, para no tener que enseñar un número.
  final String hostName;

  @override
  State<ProbeSection> createState() => _ProbeSectionState();
}

class _ProbeSectionState extends State<ProbeSection> {
  ProbeReport _report = const ProbeReport.blind();
  bool _running = false;
  String? _error;

  Future<void> _run() async {
    setState(() {
      _running = true;
      _error = null;
    });
    try {
      final ProbeReport r = await widget.run();
      if (!mounted) return;
      setState(() {
        _report = r;
        _running = false;
      });
    } on Object catch (e) {
      // El fallo se enseña y no se traga: acá los errores son precondiciones
      // que el usuario entiende, del tipo "el host todavía no está".
      if (!mounted) return;
      setState(() {
        _error = e.toString();
        _running = false;
        // El informe anterior se descarta. Un resultado viejo al lado de un
        // error nuevo se lee como si siguiera valiendo.
        _report = const ProbeReport.blind();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.isHost) {
      return const _AskSomeoneElse();
    }

    final String? error = _error;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _Header(
          hostName: widget.hostName,
          busy: _running,
          measuredLabel: _report.measuredLabel,
          onRun: _run,
        ),
        const SizedBox(height: AppSpacing.md),
        if (_running)
          const Center(child: AppSpinner())
        else if (error != null)
          _Failed(detail: error)
        else ...<Widget>[
          AppMessageNotice(
            message: AppMessages.probe(
              _report.verdict,
              leaks: _report.leaks.map((ProbeResult r) => r.title).toList(),
            ),
          ),
          if (_report.measured) ...<Widget>[
            const SizedBox(height: AppSpacing.md),
            _Results(results: _report.results),
          ],
        ],
      ],
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({
    required this.hostName,
    required this.busy,
    required this.measuredLabel,
    required this.onRun,
  });

  final String hostName;
  final bool busy;
  final String measuredLabel;
  final VoidCallback onRun;

  @override
  Widget build(BuildContext context) {
    final String quien = hostName.isEmpty ? 'la PC del host' : 'la PC de $hostName';
    return Row(
      children: <Widget>[
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              const AppKicker('LO QUE SE ALCANZA DESDE AQUÍ'),
              Text(
                measuredLabel.isEmpty
                    ? 'Marca a $quien y cuenta qué contesta'
                    : 'Probado a las $measuredLabel',
                style: context.type.bodySm.copyWith(color: context.colors.textMuted),
              ),
            ],
          ),
        ),
        AppButton(
          label: busy ? 'Probando…' : 'Probar $quien',
          onPressed: busy ? null : onRun,
        ),
      ],
    );
  }
}

/// La cara del host: la comprobación existe y la tiene que pulsar otro.
///
/// Se dice con todas las letras en vez de esconder la sección, porque el host es
/// justo quien más necesita saber que esto se puede comprobar.
class _AskSomeoneElse extends StatelessWidget {
  const _AskSomeoneElse();

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        const AppKicker('PROBAR ESTA PC DESDE OTRA'),
        const SizedBox(height: AppSpacing.sm),
        const AppNotice(
          tone: AppNoticeTone.neutral,
          title: 'Esto lo tiene que pulsar alguien más de la sala',
          body: Text(
            'Tu PC no se puede comprobar a sí misma: lo que se manda a la propia '
            'dirección no pasa por el firewall, así que saldría que está todo '
            'abierto aunque esté cerrado. Pídele a quien esté en la sala que '
            'abra esta misma pantalla y pulse el botón.',
          ),
        ),
      ],
    );
  }
}

class _Failed extends StatelessWidget {
  const _Failed({required this.detail});

  final String detail;

  @override
  Widget build(BuildContext context) {
    return AppNotice(
      title: 'No se pudo probar',
      body: Text(detail),
    );
  }
}

class _Results extends StatelessWidget {
  const _Results({required this.results});

  final List<ProbeResult> results;

  @override
  Widget build(BuildContext context) {
    return AppRowList(
      children: <Widget>[
        for (final ProbeResult r in results) _ResultRow(result: r),
      ],
    );
  }
}

class _ResultRow extends StatelessWidget {
  const _ResultRow({required this.result});

  final ProbeResult result;

  @override
  Widget build(BuildContext context) {
    return AppRow(
      child: Row(
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(result.title, style: context.type.body),
                Text(
                  _porQue(result.kind),
                  style: context.type.bodySm.copyWith(color: context.colors.textMuted),
                ),
              ],
            ),
          ),
          _Outcome(result: result),
        ],
      ),
    );
  }

  /// Por qué ese puerto está en la lista. Sin esto, la fila del canal de la sala
  /// contestando se lee igual que la de SMB contestando, y son lo contrario.
  static String _porQue(ProbeKind kind) => switch (kind) {
        ProbeKind.reference => 'Tiene que contestar: es la prueba de que la comprobación llegó',
        ProbeKind.forbidden => 'No tiene que contestar',
        ProbeKind.game => 'Lo abrió el juego',
      };
}

class _Outcome extends StatelessWidget {
  const _Outcome({required this.result});

  final ProbeResult result;

  @override
  Widget build(BuildContext context) {
    final bool malo = result.kind == ProbeKind.forbidden && result.reached;
    final bool bueno = result.kind != ProbeKind.forbidden && result.reached;

    Color color = context.colors.textMuted;
    if (malo) color = context.colors.warn;
    if (bueno) color = context.colors.ok;

    return Text(
      _texto(result),
      style: context.type.bodySm.copyWith(color: color),
    );
  }

  /// El texto de cada resultado, y el del silencio es el que más cuidado tiene.
  ///
  /// "Sin respuesta" y no "cerrado", porque está MEDIDO que en Windows un puerto
  /// callado no distingue "bloqueado" de "no hay nada escuchando". Escribir
  /// "cerrado" en esa fila sería afirmar lo que la medición no dice.
  static String _texto(ProbeResult r) => switch (r.outcome) {
        ProbeOutcome.answered => 'contesta · ${r.rttMs} ms',
        ProbeOutcome.refused => 'rebota',
        ProbeOutcome.silent => 'sin respuesta',
        ProbeOutcome.failed => 'no se pudo probar',
      };
}
