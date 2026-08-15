import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/brand.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_doc_link.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_glyphs.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/platform/system_browser.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';

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
  ///
  /// Apuntaba a `Brand.docs` con un ancla `#seed` que no existía: el enlace
  /// abría el README y quien lo pulsaba se quedaba con la misma duda. Ahora hay
  /// un documento entero, ver [Brand.seedDoc].
  static String get _masInfo => Brand.seedDoc;

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
              // El cuerpo CRECE, no aparece de golpe.
              //
              // Con `if (_abierto)` el acordeón saltaba: el diálogo entero
              // cambiaba de alto en un fotograma y la flecha giraba suave al
              // lado, que es la peor mezcla de las dos. `AnimatedSize` anima el
              // alto de esta caja con la misma curva y la misma duración que la
              // flecha, así que el gesto es uno solo.
              //
              // El hijo se mantiene montado y se recorta con altura cero: sacar
              // el árbol y volver a meterlo no tiene qué animar.
              AnimatedSize(
                duration: AppMotion.accordion,
                curve: Curves.easeOutCubic,
                alignment: Alignment.topCenter,
                child: ClipRect(
                  child: Align(
                    alignment: Alignment.topCenter,
                    heightFactor: _abierto ? 1 : 0,
                    child: Padding(
                      padding: const EdgeInsets.fromLTRB(
                        AppSpacing.x5l,
                        0,
                        AppSpacing.x5l,
                        AppSpacing.x3l,
                      ),
                      // El texto además ENTRA: aparece subiendo desde un poco
                      // más abajo y se va poniendo opaco.
                      //
                      // No es adorno encima del alto que crece. La caja
                      // creciendo sola arrastra el texto ya opaco, así que lo
                      // que se ve es un párrafo empujando el diálogo hacia
                      // abajo; con el texto entrando, lo que se lee es que el
                      // párrafo llega, y el diálogo le hace sitio.
                      //
                      // El desplazamiento es fracción de la altura del propio
                      // texto, así que un párrafo más largo entra desde más
                      // lejos y tarda lo mismo, que es lo que mantiene la
                      // velocidad pareja.
                      child: AnimatedSlide(
                        offset: _abierto ? Offset.zero : const Offset(0, 0.25),
                        duration: AppMotion.accordion,
                        curve: Curves.easeOutCubic,
                        child: AnimatedOpacity(
                          opacity: _abierto ? 1 : 0,
                          duration: AppMotion.accordion,
                          curve: Curves.easeOutCubic,
                          child: _Prosa(
                            texto:
                                'El seed presenta a los invitados entre ellos '
                                'y reparte los códigos. Levantado el túnel se '
                                'retira, y el juego va directo entre ustedes. '
                                'Cuando una conexión no consigue camino '
                                'directo, sus paquetes pasan por él cifrados: '
                                'no tiene con qué abrirlos.',
                            onMasInfo: _abrirDoc,
                            color: colors.textMuted,
                          ),
                        ),
                      ),
                    ),
                  ),
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
          // El aviso respira más que el acordeón, y es el mismo relleno que la
          // caja del seed: son las dos cajas grandes del diálogo y con
          // rellenos distintos se leían como piezas de dos pantallas.
          child: Padding(
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpacing.x5l,
              vertical: AppSpacing.x3l,
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
                    // **Dos frases, y las dos deciden algo.** La versión larga
                    // decía además que el seed de este repositorio no guarda
                    // direcciones, con qué llevarse cada IP y con quién se
                    // junta: cuatro renglones encima del botón, que es donde
                    // menos se lee. Eso vive en el documento, entero y con lo
                    // medido. Acá queda el riesgo y su límite.
                    texto:
                        'Un seed modificado puede anotar la IP pública de todo '
                        'el que ${widget.joining ? 'entre' : 'entre a tu sala'}. '
                        'Lo de dentro de la sala no lo lee ninguno.',
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
        // El mismo margen lateral que el cuerpo que despliega y que el aviso de
        // abajo: si la cabecera entra más que su propio texto desplegado, al
        // abrir el acordeón el título se mueve respecto de lo que aparece.
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.x5l,
          vertical: AppSpacing.xxl,
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
              duration: AppMotion.accordion,
              curve: Curves.easeOutCubic,
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
            child: AppDocLink(
              label: 'Más información',
              style: base,
              onTap: onMasInfo,
            ),
          ),
        ],
      ),
    );
  }
}
