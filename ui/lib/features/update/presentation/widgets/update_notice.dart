import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/brand.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/platform/system_browser.dart';
import 'package:kanpachi_ui/features/update/presentation/cubit/update_cubit.dart';

/// Where this notice goes, and nowhere else.
///
/// The RELEASES page, not a seed's download page. It used to be the latter, on
/// a compiled host, and that host stopped meaning anything: a registry is
/// anybody's now and it is not where this binary came from. What is certain
/// about a build is the channel it was published on. See [Brand.releases].

/// One line in the status bar saying a newer version exists.
///
/// # Why it lives down here and not in a dialog
///
/// Because there is nothing to decide. Kanpachi cannot update itself — see
/// `docs/07-futuro.md` — so the most this can ever do is send somebody to a
/// web page, and interrupting a person to hand them a link is a bad trade. The
/// status bar is where the app already says things about itself that are true
/// but not urgent, and it is the one strip visible on every screen.
///
/// # Why the warning is in the tooltip and not on the line
///
/// The line has to fit next to the adapter and the address, and "there is a
/// new version" is the part that has to be readable in a glance. That
/// installing it CLOSES THE ROOM is what you need before pressing, not before
/// reading, so it waits for the hover — the same instant the pointer says the
/// person is considering it.
class UpdateNotice extends StatefulWidget {
  const UpdateNotice({super.key});

  @override
  State<UpdateNotice> createState() => _UpdateNoticeState();
}

class _UpdateNoticeState extends State<UpdateNotice> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    // Subscribed to the ONE value it draws. The status bar is rebuilt by
    // whatever else changes down there, and the version it announces changes
    // at most once in the life of a run.
    final String? nueva = context.select<UpdateCubit, String?>(
      (UpdateCubit c) => c.state.available,
    );
    if (nueva == null) return const SizedBox.shrink();

    final colors = context.colors;
    return Padding(
      padding: const EdgeInsets.only(right: AppSpacing.xl),
      child: Tooltip(
        message:
            'Se descarga desde kanpachi.accentio.dev.\n'
            'Instalarla cierra la sala que tengas abierta.',
        child: MouseRegion(
          cursor: SystemMouseCursors.click,
          onEnter: (_) => setState(() => _hovered = true),
          onExit: (_) => setState(() => _hovered = false),
          child: GestureDetector(
            // `opaque`, so the gaps between the dot and the letters are the
            // target too. The same lesson as the account button: a control
            // that only answers on the pixels it paints is one you have to aim
            // at, and this one is 12 pixels tall.
            behavior: HitTestBehavior.opaque,
            onTap: () => SystemBrowser.open(Brand.releases),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Container(
                  width: 6,
                  height: 6,
                  decoration: BoxDecoration(
                    color: colors.accent,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: AppSpacing.sm),
                AnimatedDefaultTextStyle(
                  duration: AppMotion.hover,
                  style: context.type.statusLabel.copyWith(
                    color: _hovered ? colors.text : colors.textMuted,
                    decoration: _hovered ? TextDecoration.underline : null,
                    decorationColor: colors.text,
                  ),
                  child: Text('Versión $nueva disponible'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
