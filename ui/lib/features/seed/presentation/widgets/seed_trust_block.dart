import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/brand.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_glyphs.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/platform/system_browser.dart';

/// Qué es un seed, y qué puede hacerte uno malo.
///
/// # Por qué es UN widget y no el texto repetido en dos pantallas
///
/// Porque aparece en los tres momentos en que alguien decide hablarle a un
/// registro: el enlace que llega de fuera, abrir una sala y entrar a una. Es la
/// misma advertencia sobre la misma máquina, y escribirla tres veces es cómo
/// una acaba diciendo algo distinto de las otras dos sin que nadie lo note.
///
/// # Por qué la explicación va plegada y el aviso no
///
/// Son dos cosas con dos urgencias. Qué ES un seed lo pregunta quien nunca lo
/// oyó, una vez, y a quien ya lo sabe le estorba en cada sala. Lo que puede
/// hacerte uno malo hay que verlo SIN abrir nada, porque es de lo que depende
/// la decisión que se está tomando justo debajo.
class SeedTrustBlock extends StatefulWidget {
  const SeedTrustBlock({this.joining = true, super.key});

  /// Entrar, contra abrir. Cambia una palabra del aviso, «antes de entrar» o
  /// «antes de seguir», que es lo que lo ata a lo que el botón va a hacer.
  final bool joining;

  @override
  State<SeedTrustBlock> createState() => _SeedTrustBlockState();
}

class _SeedTrustBlockState extends State<SeedTrustBlock> {
  bool _abierto = false;

  /// Dónde se explica esto con calma. Sale de [Brand] y no escrito acá: es el
  /// repositorio de quien publicó esta copia, y un fork manda a la suya.
  static String get _masInfo => '${Brand.docs}#seed';

  /// Sin avisar si Windows no lo abrió. Es un enlace de ayuda, y una pantalla
  /// de error encima taparía la decisión que la persona está tomando justo
  /// debajo. Ver [SystemBrowser.open].
  void _abrirDoc() => SystemBrowser.open(_masInfo);

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        DecoratedBox(
          decoration: BoxDecoration(
            color: colors.surfaceSunken,
            borderRadius: BorderRadius.circular(14),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              _Cabecera(
                abierto: _abierto,
                onTap: () => setState(() => _abierto = !_abierto),
              ),
              if (_abierto)
                Padding(
                  padding: const EdgeInsets.fromLTRB(
                    AppSpacing.xl,
                    0,
                    AppSpacing.xl,
                    AppSpacing.lg,
                  ),
                  child: _Prosa(
                    texto:
                        'El seed presenta a los invitados entre ellos. Cuando '
                        'el túnel queda levantado se retira: los datos de la '
                        'sala van directo entre ustedes y no deberían pasar '
                        'por él.',
                    onMasInfo: _abrirDoc,
                    color: colors.textMuted,
                  ),
                ),
            ],
          ),
        ),
        const SizedBox(height: AppSpacing.md),
        DecoratedBox(
          decoration: BoxDecoration(
            border: Border.all(color: colors.border),
            borderRadius: BorderRadius.circular(14),
          ),
          child: Padding(
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpacing.xl,
              vertical: AppSpacing.lg,
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Padding(
                  padding: const EdgeInsets.only(top: 2),
                  child: IconTheme(
                    data: IconThemeData(color: colors.textMuted),
                    child: const WarnGlyph(),
                  ),
                ),
                const SizedBox(width: AppSpacing.md),
                Expanded(
                  child: _Prosa(
                    texto:
                        'Revisa a dónde apunta la dirección antes de '
                        '${widget.joining ? 'entrar' : 'seguir'}. Un seed '
                        'malicioso puede registrar las IP públicas de quienes '
                        'se presentan ante él, e incluso intentar capturar '
                        'información entre los participantes.',
                    onMasInfo: _abrirDoc,
                    color: colors.textMuted,
                  ),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// La fila que se pulsa para desplegar. Clase y no método por la regla de la
/// casa, y `const` donde se puede.
class _Cabecera extends StatelessWidget {
  const _Cabecera({required this.abierto, required this.onTap});

  final bool abierto;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(14),
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.xl,
          vertical: 13,
        ),
        child: Row(
          children: <Widget>[
            Expanded(
              child: Text(
                '¿Qué es un seed?',
                style: context.type.label.copyWith(
                  fontSize: 13,
                  color: colors.text,
                ),
              ),
            ),
            AnimatedRotation(
              turns: abierto ? 0.5 : 0,
              duration: const Duration(milliseconds: 180),
              child: Icon(
                Icons.keyboard_arrow_down_rounded,
                size: 18,
                color: colors.textMuted,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// El texto con «Más información» pegado al final, como una sola frase.
class _Prosa extends StatelessWidget {
  const _Prosa({
    required this.texto,
    required this.onMasInfo,
    required this.color,
  });

  final String texto;
  final VoidCallback onMasInfo;
  final Color color;

  @override
  Widget build(BuildContext context) {
    final TextStyle base = context.type.bodySm.copyWith(
      color: color,
      height: 1.55,
    );
    return Text.rich(
      TextSpan(
        style: base,
        children: <InlineSpan>[
          TextSpan(text: '$texto '),
          WidgetSpan(
            alignment: PlaceholderAlignment.baseline,
            baseline: TextBaseline.alphabetic,
            child: InkWell(
              onTap: onMasInfo,
              child: Text(
                'Más información',
                style: base.copyWith(color: context.colors.accent),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
