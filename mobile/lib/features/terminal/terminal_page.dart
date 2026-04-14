import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../workspace/workspace_drawer.dart';
import 'terminal_provider.dart';
import 'tab_panel.dart';
import 'terminal_view.dart' as tv;
import 'special_key_toolbar.dart';

class TerminalPage extends ConsumerWidget {
  const TerminalPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final termState = ref.watch(terminalProvider).valueOrNull;
    final activeId  = termState?.activeSessionId;

    return Scaffold(
      backgroundColor: Colors.black,
      drawer: const WorkspaceDrawer(),
      appBar: AppBar(
        backgroundColor: Colors.black,
        foregroundColor: Colors.white,
        titleTextStyle: const TextStyle(
          fontFamily: 'monospace',
          fontSize: 14,
          color: Colors.white70,
        ),
        title: Text(activeId != null
            ? 'session: ${activeId.substring(0, 8)}'
            : 'ccmux'),
        actions: [
          if (termState != null && termState.sessions.isEmpty)
            const Padding(
              padding: EdgeInsets.symmetric(horizontal: 16),
              child: Center(
                child: Text('Open the drawer to pick a session',
                    style: TextStyle(color: Colors.white38, fontSize: 12)),
              ),
            ),
        ],
      ),
      body: Column(
        children: [
          // Session tabs.
          const TabPanel(),
          // Terminal output area.
          Expanded(
            child: activeId != null
                ? tv.TerminalView(sessionId: activeId)
                : const Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(Icons.terminal, size: 64, color: Colors.white24),
                        SizedBox(height: 16),
                        Text('Swipe right or tap ☰ to open the workspace drawer',
                            style: TextStyle(color: Colors.white38),
                            textAlign: TextAlign.center),
                      ],
                    ),
                  ),
          ),
          // Special keys toolbar.
          SpecialKeyToolbar(sessionId: activeId),
        ],
      ),
    );
  }
}
