import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'terminal_provider.dart';

/// A toolbar button that sends the tmux prefix key (Ctrl+B = 0x02).
/// Long-pressing opens a quick-action sheet for common tmux operations.
/// Only shown when the active session is tmux-backed.
class TmuxPrefixButton extends ConsumerWidget {
  final String sessionId;

  const TmuxPrefixButton({super.key, required this.sessionId});

  void _send(WidgetRef ref, List<int> bytes) {
    ref.read(terminalProvider.notifier).sendInput(sessionId, Uint8List.fromList(bytes));
  }

  // Ctrl+B prefix followed by a command byte.
  void _sendCmd(WidgetRef ref, int cmd) => _send(ref, [0x02, cmd]);

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return GestureDetector(
      onLongPress: () => _showTmuxActions(context, ref),
      child: Center(
        child: TextButton(
          style: TextButton.styleFrom(
            minimumSize: const Size(56, 36),
            padding: const EdgeInsets.symmetric(horizontal: 10),
            tapTargetSize: MaterialTapTargetSize.shrinkWrap,
            backgroundColor: Theme.of(context).colorScheme.secondaryContainer,
            foregroundColor: Theme.of(context).colorScheme.onSecondaryContainer,
          ),
          onPressed: () => _send(ref, [0x02]), // Ctrl+B
          child: const Text('Ctrl+B',
              style: TextStyle(fontFamily: 'monospace', fontSize: 13)),
        ),
      ),
    );
  }

  void _showTmuxActions(BuildContext context, WidgetRef ref) {
    showModalBottomSheet<void>(
      context: context,
      builder: (ctx) {
        void action(String label, List<int> bytes) {
          Navigator.pop(ctx);
          _send(ref, bytes);
        }

        return SafeArea(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.all(12),
                child: Text('tmux',
                    style: Theme.of(context).textTheme.titleSmall?.copyWith(
                          fontFamily: 'monospace',
                          color: Theme.of(context).colorScheme.primary,
                        )),
              ),
              const Divider(height: 1),
              ListTile(
                leading: const Icon(Icons.add),
                title: const Text('New window'),
                subtitle: const Text('Ctrl+B c'),
                onTap: () => action('new window', [0x02, 0x63]),
              ),
              ListTile(
                leading: const Icon(Icons.arrow_forward),
                title: const Text('Next window'),
                subtitle: const Text('Ctrl+B n'),
                onTap: () => action('next window', [0x02, 0x6E]),
              ),
              ListTile(
                leading: const Icon(Icons.arrow_back),
                title: const Text('Previous window'),
                subtitle: const Text('Ctrl+B p'),
                onTap: () => action('prev window', [0x02, 0x70]),
              ),
              ListTile(
                leading: const Icon(Icons.drive_file_rename_outline),
                title: const Text('Rename window'),
                subtitle: const Text('Ctrl+B ,'),
                onTap: () => action('rename window', [0x02, 0x2C]),
              ),
              ListTile(
                leading: const Icon(Icons.vertical_split_outlined),
                title: const Text('Split vertical'),
                subtitle: const Text('Ctrl+B %'),
                onTap: () => action('split vertical', [0x02, 0x25]),
              ),
              ListTile(
                leading: const Icon(Icons.horizontal_split_outlined),
                title: const Text('Split horizontal'),
                subtitle: const Text('Ctrl+B "'),
                onTap: () => action('split horizontal', [0x02, 0x22]),
              ),
              ListTile(
                leading: const Icon(Icons.exit_to_app_outlined),
                title: const Text('Detach'),
                subtitle: const Text('Ctrl+B d'),
                onTap: () => action('detach', [0x02, 0x64]),
              ),
            ],
          ),
        );
      },
    );
  }
}
