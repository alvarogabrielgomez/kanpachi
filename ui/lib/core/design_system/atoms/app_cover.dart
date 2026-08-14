import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// La portada de un juego, con su hueco debajo.
///
/// # Esto no dibujaba ninguna imagen, y decía que sí
///
/// La cabecera de antes contaba que las portadas «se descargan de SteamDB
/// cuando hay red», y no había una sola petición en ningún sitio: el widget era
/// el hueco y nada más, con las palabras PORTADA STEAMDB dentro, así que en la
/// app no se veía jamás una portada. Ahora se pide de verdad, a Steam y no a
/// SteamDB, que es quien sirve esas imágenes.
///
/// # Por qué recibe una URL y no el juego
///
/// Porque esto vive en el design system, y el candado de `import_purity_test`
/// prohíbe que algo de `core/` importe una feature. Quién sabe de qué juego se
/// trata es quien lo dibuja, y qué forma pedir lo dice el hueco que va a
/// llenar: ver `SteamArt`.
///
/// # El hueco sigue siendo el hueco
///
/// Con borde discontinuo mientras la imagen viaja, y para siempre cuando el
/// juego no está en Steam — que es un caso normal, no un fallo: dos de los once
/// perfiles de fábrica no tienen identificador. Un rectángulo gris liso se
/// leería como un juego sin portada; el borde discontinuo dice «falta», no «no
/// hay».
class AppCover extends StatelessWidget {
  const AppCover({
    required this.width,
    required this.height,
    required this.radius,
    this.imageUrl,
    this.badge,
    super.key,
  });

  // El radio va por constructor y no calculado del alto: el diseño lo escala
  // con el tamaño del hueco (6 / 7 / 8 / 10), y una cadena de ternarios sobre
  // `height` acierta hoy y miente en cuanto aparezca un quinto tamaño.
  const AppCover.thumb({super.key, this.imageUrl, this.badge})
    : width = 34,
      height = 46,
      radius = AppRadius.allXs;

  const AppCover.grid({super.key, this.imageUrl, this.badge})
    : width = double.infinity,
      height = 104,
      radius = AppRadius.allSm;

  const AppCover.room({super.key, this.imageUrl, this.badge})
    : width = 44,
      height = 60,
      radius = AppRadius.all7;

  const AppCover.dialog({super.key, this.imageUrl, this.badge})
    : width = 52,
      height = 70,
      radius = AppRadius.allSm;

  /// La vista previa del alta manual: ocupa el ancho y es la más grande.
  const AppCover.preview({super.key, this.imageUrl, this.badge})
    : width = double.infinity,
      height = 150,
      radius = AppRadius.all10;

  final double width;
  final double height;
  final BorderRadius radius;

  /// La imagen que llena el hueco. Null es un juego sin portada que pedir.
  final String? imageUrl;

  /// La etiqueta que se superpone arriba a la izquierda: INSTALADO.
  final Widget? badge;

  @override
  Widget build(BuildContext context) {
    final String? url = imageUrl;
    final Widget box = SizedBox(
      width: width,
      height: height,
      child: url == null
          ? _Hueco(radius: radius, height: height)
          : ClipRRect(
              borderRadius: radius,
              child: Image.network(
                url,
                fit: BoxFit.cover,
                width: width,
                height: height,
                // **Aparece cuando llega, sin parpadeo.** `frameBuilder` corre
                // también con la imagen ya cacheada, y ahí `wasSynchronouslyLoaded`
                // dice que no hay nada que animar: volver a atenuar una portada
                // que ya estaba en memoria es lo que hace que una lista parpadee
                // al desplazarla.
                frameBuilder:
                    (
                      BuildContext context,
                      Widget child,
                      int? frame,
                      bool yaEstaba,
                    ) {
                      if (yaEstaba) return child;
                      return AnimatedOpacity(
                        opacity: frame == null ? 0 : 1,
                        duration: AppMotion.hover,
                        curve: AppMotion.enter,
                        child: child,
                      );
                    },
                // Mientras viaja, y cuando no llega, el MISMO hueco. Sin ruedita:
                // son once imágenes de sesenta kilobytes en una rejilla, y once
                // ruedecitas girando a la vez dicen «la app está trabajando»
                // sobre algo que es decoración.
                loadingBuilder:
                    (
                      BuildContext context,
                      Widget child,
                      ImageChunkEvent? progreso,
                    ) => progreso == null
                    ? child
                    : _Hueco(radius: radius, height: height),
                // Sin red, con la imagen retirada del CDN o con un identificador
                // que no existe, se cae al hueco. Un juego no se deja de poder
                // elegir porque su portada no cargue.
                errorBuilder:
                    (BuildContext context, Object error, StackTrace? _) =>
                        _Hueco(radius: radius, height: height),
              ),
            ),
    );
    if (badge == null) return box;
    return Stack(
      children: <Widget>[
        box,
        Positioned(top: AppSpacing.sm, left: AppSpacing.sm, child: badge!),
      ],
    );
  }
}

/// El hueco: lo que se ve mientras no hay portada.
class _Hueco extends StatelessWidget {
  const _Hueco({required this.radius, required this.height});

  final BorderRadius radius;
  final double height;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    // La miniatura de 46 px es la única por debajo de 50, y es la que el
    // diseño trata distinto: radio más cerrado, dos palabras cortas y menos
    // tracking. En un hueco de ese tamaño «SIN PORTADA» no cabe sin apretarse
    // contra los bordes.
    final bool mini = height < 50;
    return AppCard(
      dashed: true,
      // Con relleno hundido: las seis portadas del diseño lo llevan. Sin él
      // el hueco se confunde con el fondo de la tarjeta que lo contiene.
      filled: true,
      radius: radius,
      child: Center(
        child: Text(
          mini ? 'SIN\nARTE' : 'SIN\nPORTADA',
          textAlign: TextAlign.center,
          style: context.type.monoXxs.copyWith(
            color: colors.textMuted,
            fontSize: mini ? 7 : 8.5,
            height: mini ? 1.2 : 1.4,
            letterSpacing: mini ? 0.28 : 0.5,
          ),
        ),
      ),
    );
  }
}

/// La etiqueta verde de INSTALADO sobre una portada.
class AppInstalledBadge extends StatelessWidget {
  const AppInstalledBadge({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: BoxDecoration(color: colors.ok, borderRadius: AppRadius.pill),
      child: Text(
        'INSTALADO',
        style: context.type.monoXxs.copyWith(
          color: colors.okInk,
          fontSize: 8,
          fontWeight: FontWeight.w600,
          letterSpacing: 0.48,
        ),
      ),
    );
  }
}
