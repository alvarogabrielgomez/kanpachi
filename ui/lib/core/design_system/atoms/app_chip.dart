import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// La píldora de sólo lectura donde vive un dato literal: el código de la
/// sala en la cabecera.
///
/// Va en monoespaciada porque es un dato que se dicta y se compara carácter a
/// carácter, y no se puede seleccionar por accidente al arrastrar: para
/// llevárselo está el botón de copiar, que además dice que lo hizo.
class AppChip extends StatelessWidget {
  const AppChip(this.text, {this.mono = true, super.key});

  final String text;
  final bool mono;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final type = context.type;
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.xxl,
        vertical: AppSpacing.lg,
      ),
      decoration: BoxDecoration(
        color: colors.chip,
        borderRadius: AppRadius.pill,
      ),
      child: Text(
        text,
        style: (mono ? type.monoSm : type.labelSm)
            .copyWith(color: colors.textOnChip),
      ),
    );
  }
}

/// El bloque de texto explicativo sobre fondo de chip.
///
/// Es el sitio donde la app explica QUÉ va a pasar antes de que pase — que el
/// nombre no se manda a ningún servidor, que entrar no expone la máquina. Se
/// distingue de un aviso ([AppNotice]) en que no pide nada: informa y ya.
class AppExplainer extends StatelessWidget {
  const AppExplainer(
    this.text, {
    this.rich,
    this.textAlign,
    this.compact = false,
    super.key,
  });

  final String text;

  /// Cuando parte del texto va en negrita. Si viene, manda sobre [text].
  final InlineSpan? rich;

  /// El diseño lo centra sólo en la pantalla del nombre, donde todo lo demás
  /// va centrado. En la invitación y en el diálogo va a la izquierda.
  final TextAlign? textAlign;

  /// Dentro de un diálogo: caja más chica y menos aire. Un diálogo ya es una
  /// caja, y la geometría de pantalla completa dentro de otra caja se lee como
  /// dos marcos.
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final TextStyle base = context.type.body.copyWith(color: colors.textOnChip);
    return Container(
      width: double.infinity,
      padding: compact
          ? const EdgeInsets.symmetric(horizontal: 17, vertical: 15)
          : const EdgeInsets.symmetric(
              horizontal: AppSpacing.x4l,
              vertical: AppSpacing.x3l,
            ),
      decoration: BoxDecoration(
        color: colors.chip,
        borderRadius: compact ? AppRadius.allLg : AppRadius.allXl,
      ),
      child: rich == null
          ? Text(text, style: base, textAlign: textAlign)
          : Text.rich(rich!, style: base, textAlign: textAlign),
    );
  }
}
