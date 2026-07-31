# Design system — tokens, atomic design, organización

El DS es una **capa propia** del proyecto (`lib/core/design_system/`), no una
carpeta de widgets sueltos. Una sola familia de tokens, un componente por
arquetipo, y candados que impiden salirse.

Código en [templates/design_system/](../templates/design_system/) y
[templates/tests/](../templates/tests/).

## 1. Estructura objetivo

```
lib/core/design_system/
├── tokens/
│   ├── color_tokens.dart        # ThemeExtension (theme_tailor) — viaja por context
│   ├── typography_tokens.dart   # ThemeExtension + compositor AppType
│   ├── spacing_tokens.dart      # static const — escala, radios, bordes
│   ├── motion_tokens.dart       # static const — duraciones y curvas
│   ├── breakpoint_tokens.dart   # static const
│   └── context_ext.dart         # context.colors / context.type
├── theme/
│   └── app_theme.dart           # AppTheme.build(tokens) -> ThemeData
├── atoms/                       # app_button · app_card · app_field · app_chip · app_icons …
├── molecules/                   # app_modal · app_rows · app_top_bar · app_dock · app_toast …
├── organisms/                   # secciones compuestas (emergen con el tiempo)
├── responsive.dart              # prefersBottomSheet() y afines
└── design_system.dart           # barrel export + las reglas en el doc comment
```

Arrancar con `lib/core/design_system/` (no un package interno): menos fricción y
respeta la arquitectura features + core. Se puede extraer a package después, si
un segundo proyecto lo pide.

## 2. Tokens en tres tiers

| Tier | Qué es | Ejemplo |
|---|---|---|
| **Reference / primitive** | paleta cruda, agnóstica de rol | `rust = 0xFFD94A11`, `space4 = 4` |
| **Semantic / alias** | mapea primitivos a **roles** | `color.text.muted`, `color.border`, `gap.md` |
| **Component** | tokens por componente | `button.height.md` |

El tier **semántico** es el que hace posible light/dark y cualquier cambio de
paleta sin duplicar componentes: los componentes referencian roles, y el theme
decide qué primitivo hay detrás de cada rol. Nombrar por rol, nunca por color
(`textPrimary`, no `chalkWhite`): el rol sobrevive a un cambio de paleta.

### Qué mecanismo para cada cosa

| Mecanismo | Usar para | Tradeoff |
|---|---|---|
| `ThemeExtension<T>` | tokens tipados que viajan con `ThemeData`; anima con `lerp` | boilerplate → lo resuelve `theme_tailor`. **Es el camino principal.** |
| `ColorScheme.fromSeed` | colores de los widgets Material | no modela roles custom, spacing ni tipografía. Complementario |
| `static const` | primitivos y todo lo que no cambia en runtime | no es theme-aware |
| `InheritedWidget` a mano | casi nunca | `ThemeExtension` ya es el InheritedWidget bendecido para tokens |

### La línea divisoria: qué viaja por `context` y qué no

**Color y tipografía viajan por `context`** (cambian en runtime — light/dark,
theme por superficie — y el color anima con `lerp`). **Spacing, motion y
breakpoints son `static const`.**

El motivo es de performance, y es la misma mecánica que prohíbe los
widget-returning methods: un `ThemeExtension` se lee por `context`, así que
**nunca es const**. Cualquier widget que lea spacing por context pierde su
constructor `const`, y con él la rama de skip total de `Element.updateChild`
(`child.widget == newWidget` → sin rebuild). El color paga ese costo porque
cambia y anima; el spacing no hace ni una ni la otra.

### `theme_tailor`

`theme_tailor` + `theme_tailor_annotation` generan `copyWith`/`lerp`/`==`/
`hashCode`/`debugFillProperties` de cada `ThemeExtension` vía `build_runner`.
Con 25-30 campos × 4 métodos, escribirlos a mano es exactamente donde se cuelan
los bugs (un `copyWith()` que no toma parámetros y devuelve `this`; un
`ThemeExtension` sin `==` que no anima).

```dart
@TailorMixin()
class ColorTokens extends ThemeExtension<ColorTokens> with _$ColorTokensTailorMixin {
  const ColorTokens({required this.primary, /* … */});
  @override final Color primary;
}
```

La anotación va en `dependencies` (viaja en el source); el generador en
`dev_dependencies`.

### Accessors

```dart
extension DesignTokensContext on BuildContext {
  ColorTokens get colors => Theme.of(this).extension<ColorTokens>() ?? AppPalette.tokens;
  AppType    get type    => AppType(Theme.of(this).extension<TypographyTokens>() ?? AppTypography.tokens, colors);
}
```

**Nunca `!`.** El lookup devuelve null cuando la superficie no registró la
extensión (un subtree fuera del theme, una app secundaria del mismo codebase), y
un `!` la mata en runtime. Caer a los defaults: el fallback no es excusa para no
registrar el theme, pero un color default es mejor que un crash.

### El `ThemeData` se arma en UN lugar

`AppTheme.build(colors, fonts)` hace dos cosas y sólo dos: **registra los
tokens** en `extensions:` (para que `context.colors` los encuentre) y **tematiza
los widgets de Material una vez**. Un `ElevatedButton(style: ...)` suelto en una
pantalla es una decisión de diseño escondida donde nadie la busca.

## 3. Atomic design — como guía, no como dogma

```
Tokens → Atoms → Molecules → Organisms → Templates → Pages
```

La frontera "molécula vs organismo" es ambigua y sólo se distingue por
complejidad difusa; discutirla es deuda pura. Brad Frost mismo aclara que las
etiquetas son un modelo mental, no una taxonomía. **Usar atomic design como guía
de composición y de ubicación de archivos.**

Lo que resuelve el 80% del dolor: **(1) una capa de tokens unificada, (2) una
capa de átomos compartida (el botón primero), (3) que todo lea de tokens.**
Las moléculas y organismos emergen solos al reutilizar átomos.

### El átomo botón — la pieza que más duele

Un solo componente absorbe todos los arquetipos. Diferencias **estructurales** =
constructores nombrados; diferencias de **estilo** = enums.

```dart
enum ButtonVariant { primary, ghost, quiet }   // peso visual, no tono semántico
enum ButtonSize    { sm, md, lg }              // 36 / 48 / 56 — tres, no trece

AppButton('Guardar', onPressed: _save, loading: _saving)
AppButton('Cancelar', variant: ButtonVariant.ghost, onPressed: _cancel)
AppButton.icon(Icons.refresh, tooltip: 'Actualizar', onPressed: _refresh)
AppButton.hold('Borrar todo', onPressed: _wipe)     // mantener apretado
AppButton.link('Ver más', onPressed: _open)
```

Reglas de oro:

- **Composición, no herencia.** Extender `StatelessWidget` está bien; subclasear
  `ElevatedButton` no. Los visuales por defecto se setean **una vez** en
  `ThemeData`, no inline en cada call site.
- **Preservar la semántica de Material.** `GestureDetector` + `Container` crudos
  pierden cuatro cosas de una: foco por teclado (el botón queda inalcanzable con
  Tab), cursor de mano en desktop, ripple, y el anuncio como botón al screen
  reader. Por debajo va `FilledButton`/`OutlinedButton`/`TextButton`, o al menos
  `InkWell` + `Semantics` + `ConstrainedBox(minHeight: 48)`.
- **Todo de tokens.** Un botón que hardcodea un color deja de responder al theme.
- **`disabled` = `onPressed: null`** es el canónico. `loading` = disabled +
  spinner (nunca ocultar el label: mueve el layout y rompe el screen reader).
  Un tercer estado —apagado **pero tocable**, con el caller explicando por qué—
  es legítimo y vale la pena tenerlo: un botón gris y mudo no dice qué falta,
  justo cuando más se necesita, y `Tooltip` es hover (inútil en touch).
- **Ícono, ghost y pill son variantes, no átomos nuevos.** "Pill" es un token de
  radio. Excepción: el ícono-solo compacto sigue siendo un constructor aparte por
  tap-target y tooltip.
- **Una sola convención de tamaño.** Vigilar la palabra `size`: puede significar
  el enum, el diámetro de la caja en px, o el tamaño de fuente. Que signifique
  una sola cosa.

### El panel es `AppCard`

Superficie de la rampa (`level`: surface · card · raised) + borde
(`edge`: none · low · standard · strong · accent). La apariencia se dicta con
esos dos parámetros, **nunca pasando colores sueltos**: así el panel no puede
apartarse de la rampa.

El `Card` de Material no se usa si el sistema hace la profundidad con bordes:
trae elevation, sombra y el radius de M3, y se ve de otro sistema.

## 4. Reglas de presentación transversales

- **Modales = bottom sheet en mobile, diálogo centrado en desktop, y el criterio
  es la PLATAFORMA, no el ancho.** Un teléfono grande o un foldable reportan
  ancho por encima del breakpoint de "phone" pero siguen siendo touch: la hoja
  tiene que quedar al alcance del pulgar. Al revés, una ventana de escritorio
  angosta sigue queriendo el diálogo centrado. Fuente única:
  `responsive.dart → prefersBottomSheet()` basado en `defaultTargetPlatform`,
  sin dependencias de material ni de la DB → importable desde cualquier flavor.
  El **ancho** sí decide el **layout interno** del contenido: es responsividad de
  contenido, ortogonal a esta regla. No confundir los dos ejes.
- **Insets del sistema con `SafeArea`/`MediaQuery`, nunca padding hardcodeado
  "para el notch".** Dan 0 en desktop, así que el edge-to-edge de escritorio sale
  gratis. Los sheets suben con `MediaQuery.viewInsetsOf(context).bottom` y
  calculan su `maxHeight` sobre el espacio visible (pantalla − teclado).
- **Overlays montados fuera del Navigator necesitan su propio `Material`**
  (transparente si no quieren fondo). Sin él, `Text` no hereda
  `DefaultTextStyle` y sale con el subrayado amarillo de fallback del engine, y
  los `InkWell` tiran "No Material widget found". `Tooltip` además necesita un
  `Overlay` ancestro: fuera del Navigator, usar un affordance visible.
- **Paneles flotantes con conciencia de pantalla.** Un dropdown anclado a un
  botón se clampea dentro de los márgenes del viewport, adapta su ancho
  (`min(tope, screen.width − 2·margin)`) y hace flip hacia arriba si abajo no
  entra. Un `CompositedTransformFollower` con ancla fija se sale de pantalla
  cuando el botón está cerca de un borde.
- **Una sola librería de iconos.** Todo icono sale de un `AppIcons` del DS
  (IconData + paths SVG en un archivo). Nunca `Icons.*` suelto en una feature.

## 5. Pitfalls que matan un design system

- **Hardcodear un color dentro de un componente.** Mata el sistema antes de
  empezar: ese componente deja de responder al theme y la divergencia no la ve
  nadie hasta que está en pantalla.
- **Lista plana de tokens sin capa semántica.** Sin roles, cambiar la paleta
  obliga a tocar cada componente.
- **Tres kits de "atoms" que no se comparten.** Pasa solo: cada pantalla grande
  se hace los suyos, y a los seis meses hay tres escalas de spacing y el mismo
  primitivo definido dos veces con nombres distintos. La regla que lo evita:
  **un widget sin lógica de negocio va al DS aunque hoy lo use una sola
  superficie.**
- **Que la misma palabra signifique tres cosas.** `size` como enum, como
  diámetro en px y como tamaño de fuente en tres componentes del mismo repo es
  garantía de call sites que dicen lo contrario de lo que hacen.
- **Las restricciones duras NO son tokens.** Contraste de accesibilidad,
  tap-targets mínimos y disclosures legales van fuera del sistema de tokens: no
  son configurables.

## 6. Candados (enforcement)

No existe lint built-in que banee `Colors.*` o hex hardcodeado. Dos capas:

1. **`custom_lint`** (Invertase) para la DX en el IDE — hot reload,
   `// expect_lint`. Ojo: `dart analyze` **no** levanta las reglas custom; hay
   que correr `dart run custom_lint`.
2. **Un test de pureza** como enforcement duro en CI, porque `flutter test` ya
   corre en el pipeline y una violación rompe el build sin depender de un step
   que alguien olvide. Ver [templates/tests/ds_purity_test.dart](../templates/tests/ds_purity_test.dart).

Lo que prohíbe, fuera de `core/design_system/`:

| Prohibido | Por qué |
|---|---|
| `Colors.*` (salvo `Colors.transparent`) | paleta de Material — ignora los tokens |
| `Color(0x…)` | hex hardcodeado |
| `GoogleFonts.*` / `fontFamily:` literal | la familia se resuelve por token |
| `FilledButton`/`TextButton`/`IconButton`/… directos | el DS los envuelve para no perder foco/ripple/tap-target/a11y |
| `Card(` de Material | trae elevation + sombra + radius de M3 |

Y **el segundo candado, el de import purity**: nada dentro de
`core/design_system/` puede importar la DB local ni nada específico de una
feature. Es lo que permite que el barrel se importe desde superficies
secundarias (una consola de escritorio, un flavor sin store abierto) sin
arrastrar medio proyecto. Si el DS importa la DB, una app secundaria muere en
runtime y **nada te avisa en compile time**.

Sobre el allowlist: **mínimo a propósito**. Sólo entra lo que genuinamente no es
UI de producto (chrome nativo de una titlebar, hex que son el *dato* de una
pantalla de prueba de temas). Un allowlist que crece es deuda escondida.

## 7. Migración a un repo existente: strangler fig, sin big-bang

La capa nueva convive con la vieja; se migra pantalla por pantalla; nunca se
rompe lo que anda. Cada fase entrega valor sola.

- **Fase 0 — fundacional, no cambia UI.** Crear la carpeta + barrel. Mover la
  escala de spacing (re-export para no romper imports). **Completar el tier
  semántico**: absorber cada constante de color hardcodeada como token. Subir
  motion y breakpoints si viven en una sola superficie. Meter `theme_tailor`.
- **Fase 1 — el botón unificado** y reemplazo de los duplicados literales
  (los públicos y los que existen dos veces con el mismo nombre privado).
- **Fase 2 — resto de átomos**; subir los primitivos duplicados (eyebrow,
  section header, hair rule, paginación) y borrar las copias.
- **Fase 3 — un solo vocabulario.** Si conviven dos familias de tokens (una por
  superficie, con los mismos 15 conceptos y otros nombres), reexpresarlas con la
  MISMA forma: mismos nombres de campo, misma clase base. Que una superficie
  quiera valores distintos es legítimo; que un dev tenga que aprender dos
  vocabularios para el mismo concepto, no.
- **Fase 4 — candados.** Recién acá, cuando el allowlist puede quedar chico.

Barridos parciales: **los usos nuevos van por el DS; los viejos se migran al
tocar cada pantalla.** Y **no poner el candado antes de terminar el barrido**:
prohibir hoy lo que todavía tiene 300 usos obliga a un allowlist gigante, que es
exactamente lo que este enfoque evita.

Dos aclaraciones para cualquier barrido de `BoxDecoration`: no todo
`BoxDecoration` es un card (las variantes de un solo lado ya tienen dueño:
docks, filas, chips), y migrar de golpe cambia superficies que nadie verificó en
device.
