// coverage:ignore-file
// GENERATED CODE - DO NOT MODIFY BY HAND
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'density_tokens.dart';

// **************************************************************************
// TailorAnnotationsGenerator
// **************************************************************************

mixin _$DensityTokensTailorMixin on ThemeExtension<DensityTokens> {
  double get pagePad;
  double get rowPadVertical;
  double get rowPadHorizontal;
  double get gap;

  @override
  DensityTokens copyWith({
    double? pagePad,
    double? rowPadVertical,
    double? rowPadHorizontal,
    double? gap,
  }) {
    return DensityTokens(
      pagePad: pagePad ?? this.pagePad,
      rowPadVertical: rowPadVertical ?? this.rowPadVertical,
      rowPadHorizontal: rowPadHorizontal ?? this.rowPadHorizontal,
      gap: gap ?? this.gap,
    );
  }

  @override
  DensityTokens lerp(covariant ThemeExtension<DensityTokens>? other, double t) {
    if (other is! DensityTokens) return this as DensityTokens;
    return DensityTokens(
      pagePad: t < 0.5 ? pagePad : other.pagePad,
      rowPadVertical: t < 0.5 ? rowPadVertical : other.rowPadVertical,
      rowPadHorizontal: t < 0.5 ? rowPadHorizontal : other.rowPadHorizontal,
      gap: t < 0.5 ? gap : other.gap,
    );
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is DensityTokens &&
            const DeepCollectionEquality().equals(pagePad, other.pagePad) &&
            const DeepCollectionEquality().equals(
              rowPadVertical,
              other.rowPadVertical,
            ) &&
            const DeepCollectionEquality().equals(
              rowPadHorizontal,
              other.rowPadHorizontal,
            ) &&
            const DeepCollectionEquality().equals(gap, other.gap));
  }

  @override
  int get hashCode {
    return Object.hash(
      runtimeType.hashCode,
      const DeepCollectionEquality().hash(pagePad),
      const DeepCollectionEquality().hash(rowPadVertical),
      const DeepCollectionEquality().hash(rowPadHorizontal),
      const DeepCollectionEquality().hash(gap),
    );
  }
}

extension DensityTokensBuildContextProps on BuildContext {
  DensityTokens get densityTokens => Theme.of(this).extension<DensityTokens>()!;

  /// El aire vertical entre el borde de la tarjeta y el contenido.
  double get pagePad => densityTokens.pagePad;
  double get rowPadVertical => densityTokens.rowPadVertical;
  double get rowPadHorizontal => densityTokens.rowPadHorizontal;

  /// La separación entre bloques apilados dentro de una pantalla.
  double get gap => densityTokens.gap;
}
