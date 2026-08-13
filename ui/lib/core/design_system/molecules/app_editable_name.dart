import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// De qué tamaño se dibuja un [AppEditableName].
///
/// Es una variante del MISMO componente, igual que un botón tiene su xl y su
/// sm. Lo que cambia entre las dos es la tipografía, el lápiz y si el campo
/// lleva su propio botón de confirmar; lo que NO cambia es el comportamiento,
/// que es de lo que se trata: pulsar el bloque abre el campo, y quien lo
/// reutilice hereda el gesto en vez de volver a escribirlo.
enum AppEditableNameSize {
  /// El de la cabecera de la sala: título grande y lápiz en botón propio.
  lg,

  /// El del diálogo de confianza: a la altura de una línea de etiqueta.
  ///
  /// Ahí el nombre no es el protagonista de la pantalla, lo es la dirección del
  /// servidor. Con el tamaño de la cabecera competiría con ella justo en el
  /// momento en que hay que leerla.
  sm;

  bool get _grande => this == AppEditableNameSize.lg;
}

/// Un nombre que se lee, y que se edita pulsándolo.
///
/// # Por qué es UN componente con tamaños y no dos widgets
///
/// Porque el gesto es el mismo en los dos sitios donde se renombra una sala: la
/// cabecera de la sala y el diálogo que confirma antes de crearla. Escrito dos
/// veces, la segunda copia nació sin la marca al pasar por encima ni el campo
/// que aparece al pulsar, o sea con otro comportamiento en la misma app. Es la
/// regla de la casa, y es la misma por la que un botón chico no es un widget
/// nuevo: se reusa el componente y se le da un estilo.
///
/// # Por qué el objetivo de clic es el bloque entero
///
/// El lápiz es chico y aparece pegado al nombre, así que quien quiere renombrar
/// pincha el nombre. La marca al pasar por encima es lo que avisa de que se
/// puede, sin dejar una caja permanente compitiendo con el texto.
///
/// # Quién manda sobre el modo edición
///
/// Este widget. Es estado de la interacción y muere con ella, así que dejarlo
/// afuera obligaba a cada llamador a llevar un `bool` y a acordarse de bajarlo
/// al confirmar. [onCommit] avisa del valor ya recortado, y solo cuando quedó
/// algo escrito.
class AppEditableName extends StatefulWidget {
  const AppEditableName({
    required this.controller,
    required this.onCommit,
    this.size = AppEditableNameSize.lg,
    this.canEdit = true,
    this.onChanged,
    this.maxLength = 24,
    this.editTooltip = 'Renombrar',
    super.key,
  });

  /// El texto, que es del llamador: en el diálogo es el MISMO borrador que
  /// edita el campo de la portada, y acá se comparte en vez de copiarse.
  final TextEditingController controller;

  /// Se llama al confirmar, con el valor recortado y jamás vacío. Un nombre en
  /// blanco no es un nombre, y borrar el que había pulsando Enter sería un
  /// borrado sin pedirlo.
  final ValueChanged<String> onCommit;

  final AppEditableNameSize size;

  /// Sin permiso se dibuja el texto y nada más: ni lápiz, ni marca, ni cursor
  /// de escritura. Un lápiz apagado diría que la opción existe para quien no la
  /// tiene, que es peor que no dibujarlo.
  final bool canEdit;

  /// Cada tecla, para quien necesite el valor mientras se escribe.
  final ValueChanged<String>? onChanged;

  final int maxLength;

  /// Lo que dice el globo del lápiz, solo en [AppEditableNameSize.lg]: en el
  /// chico el lápiz es un icono dentro del bloque y no un botón propio.
  final String editTooltip;

  @override
  State<AppEditableName> createState() => _AppEditableNameState();
}

class _AppEditableNameState extends State<AppEditableName> {
  bool _editando = false;

  void _confirmar() {
    final String v = widget.controller.text.trim();
    if (v.isNotEmpty) widget.onCommit(v);
    setState(() => _editando = false);
  }

  @override
  Widget build(BuildContext context) {
    if (_editando) {
      return _NombreEnCampo(
        controller: widget.controller,
        size: widget.size,
        maxLength: widget.maxLength,
        onChanged: widget.onChanged,
        onConfirmar: _confirmar,
      );
    }
    return _NombreEnLectura(
      texto: widget.controller.text,
      size: widget.size,
      canEdit: widget.canEdit,
      editTooltip: widget.editTooltip,
      onEditar: () => setState(() => _editando = true),
    );
  }
}

/// El campo, mientras se escribe.
class _NombreEnCampo extends StatelessWidget {
  const _NombreEnCampo({
    required this.controller,
    required this.size,
    required this.maxLength,
    required this.onChanged,
    required this.onConfirmar,
  });

  final TextEditingController controller;
  final AppEditableNameSize size;
  final int maxLength;
  final ValueChanged<String>? onChanged;
  final VoidCallback onConfirmar;

  @override
  Widget build(BuildContext context) {
    final AppField campo = AppField(
      controller: controller,
      shape: AppFieldShape.inline,
      height: size._grande ? 44 : null,
      radius: AppRadius.all10,
      maxLength: maxLength,
      autofocus: true,
      textStyle: size._grande ? context.type.titleLg : null,
      onSubmitted: (_) => onConfirmar(),
      onChanged: onChanged,
      // El botón de confirmar solo en el grande. En el chico el campo ocupa una
      // línea junto a su etiqueta, y un botón ahí dentro la parte en dos; se
      // confirma con Enter, que es lo que ya hace quien acaba de escribir.
      trailing: size._grande ? _Confirmar(onTap: onConfirmar) : null,
    );
    if (!size._grande) return campo;
    // Tope y no ancho fijo. En la ventana mínima quedan menos de 300 px a la
    // izquierda de los botones de la cabecera, y un ancho fijo se desborda
    // justo mientras alguien escribe dentro.
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 360),
      child: campo,
    );
  }
}

/// El nombre en firme, con su lápiz y su marca al pasar por encima.
class _NombreEnLectura extends StatefulWidget {
  const _NombreEnLectura({
    required this.texto,
    required this.size,
    required this.canEdit,
    required this.editTooltip,
    required this.onEditar,
  });

  final String texto;
  final AppEditableNameSize size;
  final bool canEdit;
  final String editTooltip;
  final VoidCallback onEditar;

  @override
  State<_NombreEnLectura> createState() => _NombreEnLecturaState();
}

class _NombreEnLecturaState extends State<_NombreEnLectura> {
  bool _encima = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool grande = widget.size._grande;

    final Widget fila = Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        // Flexible y no suelto: el nombre lo escribe una persona, hasta
        // `maxLength`, y no siempre cabe entero al lado de lo que tenga a la
        // derecha. Cede él, que se puede recortar.
        Flexible(
          child: Text(
            widget.texto,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: (grande ? context.type.titleLg : context.type.label)
                .copyWith(color: colors.text),
          ),
        ),
        if (widget.canEdit) ...<Widget>[
          SizedBox(width: grande ? AppSpacing.sm : AppSpacing.md),
          if (grande)
            AppIconButton(
              icon: Icons.edit_outlined,
              tooltip: widget.editTooltip,
              width: 30,
              height: 30,
              iconSize: 15,
              danger: true,
              onPressed: widget.onEditar,
            )
          else
            Icon(Icons.edit_outlined, size: 15, color: colors.textMuted),
        ],
      ],
    );

    if (!widget.canEdit) return fila;

    return MouseRegion(
      cursor: SystemMouseCursors.text,
      onEnter: (_) => setState(() => _encima = true),
      onExit: (_) => setState(() => _encima = false),
      child: GestureDetector(
        onTap: widget.onEditar,
        // El desplazamiento va por `Transform` y no por padding negativo, que
        // no existe: así la caja de la marca alinea con el texto sin mover nada
        // de lo que tenga alrededor, porque una transformación es sólo pintura.
        child: Transform.translate(
          offset: const Offset(-AppSpacing.lg, 0),
          child: AnimatedContainer(
            duration: AppMotion.hover,
            // El aire vertical solo en el chico. El grande vive dentro de una
            // caja de 44 px que ya se lo da, y sumarle relleno lo desalinea de
            // la etiqueta que tiene encima.
            padding: EdgeInsets.symmetric(
              horizontal: AppSpacing.lg,
              vertical: grande ? 0 : AppSpacing.md,
            ),
            decoration: BoxDecoration(
              color: _encima ? colors.surfaceSunken : null,
              borderRadius: AppRadius.all10,
              border: Border.all(
                color: _encima ? colors.border : Colors.transparent,
                width: AppStroke.hairline,
              ),
            ),
            child: fila,
          ),
        ),
      ),
    );
  }
}

/// El botón de confirmar del tamaño grande.
///
/// Relleno de acento y no transparente: es la acción que confirma el cambio, no
/// compite con nada, y en el diseño es la única mancha de color de la cabecera
/// mientras se escribe.
class _Confirmar extends StatelessWidget {
  const _Confirmar({required this.onTap});

  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      child: GestureDetector(
        onTap: onTap,
        child: Tooltip(
          message: 'Guardar',
          child: Container(
            width: 36,
            height: 34,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: colors.accent,
              borderRadius: AppRadius.allSm,
            ),
            child: Icon(Icons.check, size: 15, color: colors.accentInk),
          ),
        ),
      ),
    );
  }
}
