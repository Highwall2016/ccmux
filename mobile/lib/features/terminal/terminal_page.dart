import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../core/theme.dart';
import 'terminal_provider.dart';
import 'terminal_view.dart' as tv;
import 'special_key_toolbar.dart';
import 'tmux_window_tabs.dart';

// ─────────────────────────────────────────────────────────────────────────────

class TerminalPage extends ConsumerWidget {
  const TerminalPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final termState = ref.watch(terminalProvider).valueOrNull;
    final activeId  = termState?.activeSessionId;
    final sess      = activeId != null ? termState!.sessions[activeId] : null;

    if (activeId == null || sess == null) {
      return Scaffold(
        backgroundColor: CcmuxColors.bg,
        body: SafeArea(
          child: Column(
            children: [
              _buildNavBar(context, ref, null, null),
              const Expanded(
                child: Center(
                  child: Text('No session selected',
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 13,
                        color: CcmuxColors.textDim,
                      )),
                ),
              ),
            ],
          ),
        ),
      );
    }

    return Scaffold(
      backgroundColor: CcmuxColors.bgDark,
      body: SafeArea(
        child: Column(
          children: [
            _buildNavBar(context, ref, activeId, sess),
            Expanded(child: tv.TerminalView(sessionId: activeId)),
            SpecialKeyToolbar(sessionId: activeId),
            TmuxWindowTabs(sessionId: activeId),
          ],
        ),
      ),
    );
  }

  Widget _buildNavBar(
    BuildContext context,
    WidgetRef ref,
    String? sessionId,
    TerminalSessionState? sess,
  ) {
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 6, 16, 10),
      decoration: const BoxDecoration(
        color: CcmuxColors.bg,
        border: Border(bottom: BorderSide(color: CcmuxColors.divider)),
      ),
      child: Stack(
        alignment: Alignment.center,
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 48),
            child: Text(
              sess?.name ?? '',
              style: const TextStyle(
                fontFamily: 'monospace',
                fontSize: 14,
                fontWeight: FontWeight.w600,
                color: CcmuxColors.text,
                letterSpacing: -0.3,
              ),
              textAlign: TextAlign.center,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          Align(
            alignment: Alignment.centerLeft,
            child: GestureDetector(
              onTap: () => context.go('/sessions'),
              child: const Icon(Icons.chevron_left, color: CcmuxColors.accent, size: 22),
            ),
          ),
        ],
      ),
    );
  }
}
