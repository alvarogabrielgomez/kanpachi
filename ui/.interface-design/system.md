# Bohemian Gym — Interface Design System

## Direction

**Industrial-bohemian ledger.** Basement gym meets kraft-paper notebook.
Honest materials: oxidized iron, gym chalk, warm coal, brass. Not chrome,
not glass, not neon. Every choice should feel like it came from a place
where heavy things get lifted and sets get logged with a pencil.

### Intent
- **Who:** A lifter. Mid-session with chalky hands AND Sunday-night with coffee.
- **Lead verb:** *See what's next today.* Today screen is a glance, not a dashboard.
- **Feel:** Warm-coal, hand-stamped, tabular. Quiet structure. No softness for softness' sake.

### The signature (still to land in components)
**Tally-mark set counter.** Completed sets render as literal `||||` strokes,
fifth crosses through. Reads like a notebook ledger, not a progress bar.
First component-level work where this lands gets documented under "Patterns".

---

## Tokens

All defined in `lib/core/theme/app_colors.dart` and surfaced via
`AppColorsExtension`. Access in widgets via `context.colors.<name>`.

### Coal stack — surface elevation (warm-shifted, never zinc)
| Token | Value | Use |
|-------|-------|-----|
| `backgroundDark` | `#050507` | Void: behind modals, scrim base |
| `background` | `#0B0A09` | Page canvas |
| `surface` | `#161310` | First elevation: app bar, input fill, sheet |
| `card` | `#1F1B16` | Second elevation: cards, popovers |

Each step shifts ~3% lightness. Whisper-quiet. Warm bias on every level.

### Rust — the only accent
| Token | Value | Use |
|-------|-------|-----|
| `primary` | `#D94A11` | Action, focus rings, signature tally, key emphasis |
| `onPrimary` | `#F4EEE3` | Chalk on rust (NOT pure white — too clinical) |

One accent. Used with intention. Don't reach for a second.

### Ink — text hierarchy (4 levels, all warm)
| Token | Value | Role |
|-------|-------|------|
| `textHighlight` | `#F4EEE3` (chalk-bone) | Primary text, headlines, current values |
| `textMuted` | `#A89A87` (kraft) | Secondary text, body copy, labels |
| `textSubtle` | `#7B6F5E` (kraft-deep) | Tertiary, metadata, placeholders |
| `iconInactive` | `#7B6F5E` | Inactive icons (matches kraft-deep) |

Use all four. If only two are present, hierarchy is too flat.

### Border
| Token | Value | Use |
|-------|-------|-----|
| `border` | `#2A241D` | Card edges, dividers, input outlines |

Single border token for now. Warm coal-kraft hybrid — disappears when not
looking, findable when needed. **Never** use a brighter solid hex border.

### Semantic — desaturated to fit the coal world
| Token | Value | Use |
|-------|-------|-----|
| `success` | `#5FA365` | Oxidized green. Set complete, save success. |
| `warning` | `#7A5419` | Brass-deep. Warning surfaces. |
| `warningText` | `#E8C46A` | Brass-light. Warning copy on warning surfaces. |

**No neon greens, no candy reds.** If you need destructive, derive from rust.

---

## Depth

**Borders-only.** No drop shadows anywhere. Basement-gym light is hard
overhead light — it doesn't make soft halos. Shadows would lie about the world.

- Cards: `card` background + 1px `border` + 2px radius
- Inputs: `surface` fill (darker than card to feel inset) + 1px `border`
- Focus: `primary` border, 2px
- Dropdowns / popovers: stack to next surface level (use `card` over `surface`)

**Never** add `BoxShadow`. Never set `Card.elevation > 0`.

---

## Typography

**Oswald** for display + stencil labels (condensed, stamped feel).
**Space Grotesk** for body + data readouts (tabular figures for log-book columns).

Full hierarchy lives in `app_theme.dart` `textTheme`. Use `Theme.of(context).textTheme`
or shortcuts on `context`. Don't redefine sizes inline.

| Style | Font | Size / Weight | Use |
|-------|------|---------------|-----|
| `displayLarge` | Oswald 700 | 36 / -0.5 tracking | Page hero, big number |
| `displayMedium` | Oswald 700 | 28 | Secondary hero |
| `titleLarge` | Oswald 600 | 22 | Section header, exercise name |
| `titleMedium` | Oswald 600 | 18 | Subsection |
| `titleSmall` | Oswald 600 | 14 / +0.8 tracking | Eyebrow / category label (kraft) |
| `bodyLarge` | Space Grotesk 400 | 16 | Primary body (chalk) |
| `bodyMedium` | Space Grotesk 400 | 14 | Body (kraft) |
| `bodySmall` | Space Grotesk 400 | 12 | Tertiary (kraft-deep) |
| `labelLarge` | Oswald 700 UPPER | 14 / +1.2 tracking | Buttons, primary CTAs (rust) |
| `labelMedium` | Oswald 600 UPPER | 12 / +1.0 | Tab labels, eyebrows (kraft) |
| `labelSmall` | Space Grotesk 500 tabular | 11 | Numeric metadata, set counts |

Numbers use `FontFeature.tabularFigures()` so set/rep/weight columns align.

---

## Spacing

`AppSpacing` constants (`lib/core/theme/app_spacing.dart`). 4px base.

| Token | px | Use |
|-------|----|-----|
| `micro` | 4 | Icon-text gap, tally-stroke gap |
| `xs` | 8 | Tight inline spacing |
| `sm` | 12 | Component-internal |
| `md` | 16 | Card padding, button padding (default) |
| `lg` | 24 | Between components in a section |
| `xl` | 32 | Between sections |
| `xxl` | 48 | Major regions, top-level rhythm |

**Never use literal padding numbers.** If a value isn't here, ask why.

### Radius
| Token | px | Use |
|-------|----|-----|
| `radiusInput` | 0 | Buttons, primary CTAs (stamped, no give) |
| `radiusCard` | 2 | Cards, inputs (hair of softness) |
| `radiusSheet` | 8 | Bottom sheets, modals (slightly inviting) |

---

## What we reject (and what replaces it)

| Default | Bohemian Ledger replaces with |
|---------|-------------------------------|
| Linear progress bar (`X of Y`) | **Tally-mark ledger** (literal strokes) |
| Material card grid + circle icons | Notebook-row layout w/ horizontal rule dividers |
| Neon-highlighted nav tab | Ink-stamp accent block on rust |
| Pure-white text | Chalk-bone (`#F4EEE3`) — warm, not clinical |
| Zinc-gray mutes (`#8A8A8E`) | Kraft (`#A89A87`) — warm, hand-aged |
| Drop shadows | 1px warm border |
| Multiple accent colors | Single rust accent. Hierarchy via type weight + chalk/kraft. |

---

## Patterns

*To be filled as components are built. Add a pattern here when it's used 2+
times, has specific measurements worth remembering, or carries a load-bearing
decision (e.g. the tally-mark set counter when it lands).*

---

## Migration note (one-shot)

`ThemeRepository.loadTheme()` detects pre-bohemian defaults stored in ObjectBox
and refreshes them on first load after this system landed. Users with custom
themes are preserved. See `theme_repository.dart`.

---

## Consistency checks

When adding any UI:
- Padding/spacing comes from `AppSpacing`. No literal numbers.
- Color comes from `context.colors.*`. No `Color(0xFF...)` in widgets.
- Text style comes from `Theme.of(context).textTheme`. No inline `TextStyle(fontSize: ...)`.
- Depth = border. If you wrote `BoxShadow`, delete it.
- One accent (rust). If you reached for a second hue, find another way.

If a screen breaks any of these, it's not yet on system.
