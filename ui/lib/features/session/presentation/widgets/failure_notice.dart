import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/features/session/domain/entities/action_failure.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/detail_drawer.dart';

/// The notice for an action the user asked for, that did not happen.
///
/// Same shape as every other notice in the app, in the error tone: accent
/// pulled towards red. It is the only tone that breaks the "never red" rule,
/// and it breaks it with reason — the other notices talk about things Kanpachi
/// noticed on its own, this one about an order that failed, and only here is
/// somebody waiting with something to retry.
///
/// # The details button appears only when narrating
///
/// Not for secrecy. The details name subnets, adapters, seeds and Go error
/// strings: useful to whoever builds Kanpachi, noise to whoever plays it. With
/// the narration off the button is not drawn, so there is nothing to press and
/// no half-open drawer to explain — and it would open on nothing anyway,
/// because with narration off nobody collected the steps. It is a setting and
/// not a build flag: see [AppPreferences.verbose] for why that changed.
class FailureNotice extends StatefulWidget {
  const FailureNotice({
    required this.failure,
    required this.verbose,
    this.onDismiss,
    super.key,
  });

  final ActionFailure failure;

  /// Whether this window narrates. Passed in rather than read from the cubit
  /// so the notice stays paintable on its own.
  final bool verbose;

  /// Dismisses the notice. Null hides the button, for screens where the notice
  /// goes away on its own with the next action.
  final VoidCallback? onDismiss;

  @override
  State<FailureNotice> createState() => _FailureNoticeState();
}

class _FailureNoticeState extends State<FailureNotice> {
  bool _abierto = false;

  @override
  void didUpdateWidget(FailureNotice old) {
    super.didUpdateWidget(old);
    // A new failure closes the drawer. Leaving it open would show the previous
    // failure's steps under the new failure's title, which is worse than
    // showing nothing.
    if (!identical(old.failure, widget.failure)) _abierto = false;
  }

  @override
  Widget build(BuildContext context) {
    final bool puedeDetallar = widget.verbose && widget.failure.hasDetails;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        AppMessageNotice(
          message: AppMessages.actionFailed(
            widget.failure.action,
            code: widget.failure.code,
          ),
          actions: <Widget>[
            if (widget.onDismiss != null)
              AppButton(
                label: 'Entendido',
                onPressed: widget.onDismiss,
                variant: AppButtonVariant.ghost,
              ),
            if (puedeDetallar)
              AppButton(
                label: _abierto ? 'Ocultar detalles' : 'Ver detalles',
                onPressed: () => setState(() => _abierto = !_abierto),
                variant: AppButtonVariant.ghost,
              ),
          ],
        ),
        if (puedeDetallar && _abierto) ...<Widget>[
          const SizedBox(height: AppSpacing.xl),
          DetailDrawer(
            progress: widget.failure.progress,
            reason: widget.failure.reason,
          ),
        ],
      ],
    );
  }
}
