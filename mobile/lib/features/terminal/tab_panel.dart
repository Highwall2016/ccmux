import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'terminal_provider.dart';

class TabPanel extends ConsumerWidget {
  const TabPanel({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(terminalProvider).valueOrNull;
    if (state == null || state.sessions.isEmpty) return const SizedBox.shrink();

    final sessions = state.sessions.values.toList();

    return Container(
      height: 44,
      color: Theme.of(context).colorScheme.surfaceContainer,
      child: ListView.builder(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
        itemCount: sessions.length,
        itemBuilder: (context, i) {
          final sess = sessions[i];
          final isActive = sess.id == state.activeSessionId;
          final label = sess.name;

          return Padding(
            padding: const EdgeInsets.only(right: 4),
            child: Stack(
              clipBehavior: Clip.none,
              children: [
                // Tab chip.
                GestureDetector(
                  onTap: () => ref.read(terminalProvider.notifier).setActiveSession(sess.id),
                  child: Container(
                    constraints: const BoxConstraints(minWidth: 72, maxWidth: 140),
                    padding: const EdgeInsets.symmetric(horizontal: 10),
                    decoration: BoxDecoration(
                      color: isActive
                          ? Theme.of(context).colorScheme.primary
                          : Theme.of(context).colorScheme.surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(6),
                    ),
                    alignment: Alignment.center,
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        // Status indicator.
                        if (sess.status != 'active')
                          Padding(
                            padding: const EdgeInsets.only(right: 4),
                            child: Icon(Icons.circle,
                                size: 8,
                                color: isActive ? Colors.white : Colors.red),
                          ),
                        Flexible(
                          child: Text(
                            label,
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 12,
                              color: isActive ? Colors.white : null,
                            ),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
                // Close button.
                Positioned(
                  top: -4,
                  right: -6,
                  child: GestureDetector(
                    onTap: () => ref.read(terminalProvider.notifier).closeSession(sess.id),
                    child: Container(
                      width: 16,
                      height: 16,
                      decoration: BoxDecoration(
                        color: Theme.of(context).colorScheme.error,
                        shape: BoxShape.circle,
                      ),
                      child: const Icon(Icons.close, size: 10, color: Colors.white),
                    ),
                  ),
                ),
                // New-output dot.
                if (sess.hasNewOutput && !isActive)
                  const Positioned(
                    bottom: -2,
                    right: 8,
                    child: CircleAvatar(
                      radius: 4,
                      backgroundColor: Colors.blue,
                    ),
                  ),
              ],
            ),
          );
        },
      ),
    );
  }
}
