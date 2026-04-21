import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/api/api_models.dart';
import '../workspace/workspace_provider.dart';
import 'swipe_pref_provider.dart';
import 'terminal_provider.dart';

/// Horizontal window-tab bar shown below the special-key toolbar for tmux-backed
/// sessions.  Renders one chip per tmux window; the chip for the window that
/// owns the current pane is highlighted.  Tapping a chip opens the first pane
/// of that window (or the already-open session for that pane).
class TmuxWindowTabs extends ConsumerWidget {
  final String sessionId;

  const TmuxWindowTabs({super.key, required this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final termState = ref.watch(terminalProvider).valueOrNull;
    final sess      = termState?.sessions[sessionId];
    if (sess == null || !sess.tmuxBacked) return const SizedBox.shrink();

    final ws = ref.watch(workspaceProvider).valueOrNull;
    if (ws == null) return const SizedBox.shrink();

    // Find which device owns this session.
    String? deviceId;
    for (final entry in ws.sessionsByDevice.entries) {
      if (entry.value.any((s) => s.id == sessionId)) {
        deviceId = entry.key;
        break;
      }
    }
    if (deviceId == null) return const SizedBox.shrink();

    final tree = ws.tmuxTreeByDevice[deviceId];
    if (tree == null) return const SizedBox.shrink();

    // Find the tmux session and window that contain the current pane.
    TmuxSessionNode? currentTmuxSession;
    TmuxWindowNode? currentWindow;
    outer:
    for (final ts in tree.sessions) {
      for (final w in ts.windows) {
        for (final p in w.panes) {
          if (p.ccmuxId == sessionId) {
            currentTmuxSession = ts;
            currentWindow = w;
            break outer;
          }
        }
      }
    }

    if (currentTmuxSession == null) return const SizedBox.shrink();

    final windows     = currentTmuxSession.windows;
    final colorScheme = Theme.of(context).colorScheme;
    final swipeEnabled = ref.watch(swipePrefProvider).valueOrNull ?? false;

    return Container(
      height: 36,
      color: colorScheme.surfaceContainerHigh,
      child: Row(
        children: [
          Expanded(
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
              itemCount: windows.length,
              separatorBuilder: (_, __) => const SizedBox(width: 4),
              itemBuilder: (_, i) {
                final w       = windows[i];
                final isActive = w == currentWindow;
                return GestureDetector(
                  onTap: () => _switchToWindow(ref, w, ws, deviceId!),
                  child: AnimatedContainer(
                    duration: const Duration(milliseconds: 150),
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 2),
                    decoration: BoxDecoration(
                      color: isActive
                          ? colorScheme.primary
                          : colorScheme.surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Text(
                      '${w.index}: ${w.name}',
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 12,
                        color: isActive
                            ? colorScheme.onPrimary
                            : colorScheme.onSurface,
                      ),
                    ),
                  ),
                );
              },
            ),
          ),
          // Swipe-gesture toggle button.
          Tooltip(
            message: swipeEnabled
                ? 'Swipe to change window: ON'
                : 'Swipe to change window: OFF',
            child: IconButton(
              iconSize: 16,
              padding: const EdgeInsets.symmetric(horizontal: 8),
              constraints: const BoxConstraints(minWidth: 32, minHeight: 36),
              icon: Icon(
                swipeEnabled ? Icons.swipe : Icons.swipe_outlined,
                color: swipeEnabled
                    ? colorScheme.primary
                    : colorScheme.onSurface.withValues(alpha: 0.4),
              ),
              onPressed: () =>
                  ref.read(swipePrefProvider.notifier).toggle(),
            ),
          ),
        ],
      ),
    );
  }

  void _switchToWindow(
    WidgetRef ref,
    TmuxWindowNode window,
    WorkspaceState ws,
    String deviceId,
  ) {
    // Find a pane in this window that is already open, or open the active pane.
    final sessions = ws.sessionsByDevice[deviceId] ?? [];
    final termNotifier = ref.read(terminalProvider.notifier);
    final termState    = ref.read(terminalProvider).valueOrNull;

    // Prefer the active pane; fall back to the first pane.
    final target = window.panes.firstWhere(
      (p) => p.active,
      orElse: () => window.panes.first,
    );

    // If we already have this pane open, just switch to it.
    if (termState != null && termState.sessions.containsKey(target.ccmuxId)) {
      termNotifier.setActiveSession(target.ccmuxId);
      return;
    }

    // Otherwise open the session. Look up the session model for a name.
    final model = sessions.firstWhere(
      (s) => s.id == target.ccmuxId,
      orElse: () => SessionModel(
        id:           target.ccmuxId,
        command:      '',
        name:         target.title.isNotEmpty ? target.title : window.name,
        status:       'active',
        startedAt:    '',
        lastActivity: '',
      ),
    );

    // Send Ctrl+B followed by the window index digit to jump directly.
    final indexStr = '${window.index}';
    final bytes    = <int>[0x02, ...indexStr.codeUnits];
    ref.read(terminalProvider.notifier)
        .sendInput(sessionId, Uint8List.fromList(bytes));

    // Also open the pane session so the tab bar can track it.
    termNotifier.openSession(
      target.ccmuxId,
      name:       model.name.isNotEmpty ? model.name : window.name,
      tmuxBacked: true,
    );
  }
}
