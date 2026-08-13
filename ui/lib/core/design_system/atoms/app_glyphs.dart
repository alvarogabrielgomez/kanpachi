import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';

/// Los dos glifos que el diseño dibuja a mano y Material no tiene.
///
/// # Por qué no son `Icons.*` como el resto
///
/// Porque no se parecen. `Icons.content_copy` a 12 px pierde la hoja de atrás y
/// queda un cuadrado suelto, y `Icons.link` es una cadena HORIZONTAL: girada 45°
/// para que caiga como la del diseño se lee como un rombo. Medido en una foto,
/// no supuesto.
///
/// Son los trazos del archivo de diseño, punto por punto, sobre el mismo lienzo
/// de 16 que usa el SVG. Se estiran al tamaño que se les pida y toman el color
/// del [IconTheme] que tengan encima — que es como [AppButton] les pasa el color
/// del texto y su atenuación, así que se tiñen solos al pasar el ratón.
class CopyGlyph extends StatelessWidget {
  const CopyGlyph({this.size = 12, super.key});

  final double size;

  @override
  Widget build(BuildContext context) {
    return _Glyph(size: size, painter: _CopyPainter.new);
  }
}

/// El eslabón inclinado de «Copiar enlace».
class LinkGlyph extends StatelessWidget {
  const LinkGlyph({this.size = 13, super.key});

  final double size;

  @override
  Widget build(BuildContext context) {
    return _Glyph(size: size, painter: _LinkPainter.new);
  }
}

/// El triángulo con la admiración del aviso de confianza.
///
/// A mano por lo mismo que los otros dos: `Icons.warning_amber` viene RELLENO y
/// con la punta redondeada, y a 16 px al lado de un párrafo gris se lee como una
/// alarma. El del diseño es de trazo, del mismo grosor que el texto que
/// acompaña, y dice «mira esto» sin gritar.
class WarnGlyph extends StatelessWidget {
  const WarnGlyph({this.size = 16, super.key});

  final double size;

  @override
  Widget build(BuildContext context) {
    return _Glyph(size: size, painter: _WarnPainter.new);
  }
}

/// La caja común: toma el color del [IconTheme] y pinta.
class _Glyph extends StatelessWidget {
  const _Glyph({required this.size, required this.painter});

  final double size;
  final CustomPainter Function(Color) painter;

  @override
  Widget build(BuildContext context) {
    final IconThemeData theme = IconTheme.of(context);
    // El color del texto como suelo, y no un negro a mano: estos glifos van
    // siempre pegados a una etiqueta, así que el color de texto es el que
    // menos desentona cuando nadie puso un IconTheme encima.
    final Color base = theme.color ?? context.colors.text;
    final Color color = base.withValues(alpha: base.a * (theme.opacity ?? 1));
    return SizedBox(
      width: size,
      height: size,
      child: CustomPaint(painter: painter(color)),
    );
  }
}

/// El lienzo del SVG original. Todo lo de abajo está en estas unidades y se
/// escala al final, así que los números se pueden comparar con el diseño tal
/// cual están escritos ahí.
const double _canvas = 16;

Paint _stroke(Color color, double width) => Paint()
  ..style = PaintingStyle.stroke
  ..strokeWidth = width
  ..strokeCap = StrokeCap.round
  ..strokeJoin = StrokeJoin.round
  ..color = color;

/// Dos hojas: la de delante entera, la de atrás asomando por arriba y por la
/// izquierda.
class _CopyPainter extends CustomPainter {
  const _CopyPainter(this.color);

  final Color color;

  static const Radius _corner = Radius.circular(1.5);

  @override
  void paint(Canvas canvas, Size size) {
    canvas.scale(size.width / _canvas, size.height / _canvas);
    final Paint pincel = _stroke(color, 1.4);

    // La hoja de atrás, abierta: sube desde la esquina de la de delante,
    // rodea por arriba y vuelve a bajar. Los arcos son los del SVG, con el
    // mismo radio y el mismo sentido antihorario.
    final Path atras = Path()
      ..moveTo(10.5, 3.5)
      ..arcToPoint(const Offset(9, 2.5), radius: _corner, clockwise: false)
      ..lineTo(4, 2.5)
      ..arcToPoint(const Offset(2.5, 4), radius: _corner, clockwise: false)
      ..lineTo(2.5, 9)
      ..arcToPoint(const Offset(3.5, 10.5), radius: _corner, clockwise: false);
    canvas.drawPath(atras, pincel);

    canvas.drawRRect(
      RRect.fromRectAndRadius(
        const Rect.fromLTWH(5.5, 5.5, 8, 8),
        const Radius.circular(2),
      ),
      pincel,
    );
  }

  @override
  bool shouldRepaint(_CopyPainter old) => old.color != color;
}

/// Dos medios eslabones que se cruzan en diagonal.
class _LinkPainter extends CustomPainter {
  const _LinkPainter(this.color);

  final Color color;

  static const Radius _bend = Radius.circular(2.6);

  @override
  void paint(Canvas canvas, Size size) {
    canvas.scale(size.width / _canvas, size.height / _canvas);
    final Paint pincel = _stroke(color, 1.5);

    final Path eslabones = Path()
      // El de arriba a la derecha.
      ..moveTo(6.6, 9.4)
      ..arcToPoint(const Offset(10.3, 9.4), radius: _bend, clockwise: false)
      ..lineTo(12.5, 7.2)
      ..arcToPoint(const Offset(8.8, 3.5), radius: _bend, clockwise: false)
      ..lineTo(7.9, 4.4)
      // El de abajo a la izquierda.
      ..moveTo(9.4, 6.6)
      ..arcToPoint(const Offset(5.7, 6.6), radius: _bend, clockwise: false)
      ..lineTo(3.5, 8.8)
      ..arcToPoint(const Offset(7.2, 12.5), radius: _bend, clockwise: false)
      ..lineTo(8.1, 11.6);
    canvas.drawPath(eslabones, pincel);
  }

  @override
  bool shouldRepaint(_LinkPainter old) => old.color != color;
}

/// El triángulo de [WarnGlyph], sobre el mismo lienzo de 16 del diseño.
class _WarnPainter extends CustomPainter {
  _WarnPainter(this.color);

  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final double k = size.width / 16;
    final Paint trazo = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeJoin = StrokeJoin.round
      ..strokeCap = StrokeCap.round
      ..strokeWidth = 1.3 * k;

    // M8 2.6 14 13H2Z
    final Path triangulo = Path()
      ..moveTo(8 * k, 2.6 * k)
      ..lineTo(14 * k, 13 * k)
      ..lineTo(2 * k, 13 * k)
      ..close();
    canvas.drawPath(triangulo, trazo);

    // El palo, un pelo más grueso que el triángulo, como en el SVG.
    canvas.drawLine(
      Offset(8 * k, 6.6 * k),
      Offset(8 * k, 9.6 * k),
      Paint()
        ..color = color
        ..style = PaintingStyle.stroke
        ..strokeCap = StrokeCap.round
        ..strokeWidth = 1.4 * k,
    );

    // El punto va RELLENO. De trazo, a este tamaño, sale un anillo hueco que
    // se lee como otro símbolo.
    canvas.drawCircle(Offset(8 * k, 11.2 * k), 0.7 * k, Paint()..color = color);
  }

  @override
  bool shouldRepaint(_WarnPainter old) => old.color != color;
}
