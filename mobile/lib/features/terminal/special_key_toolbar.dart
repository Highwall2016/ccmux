import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'terminal_provider.dart';

class SpecialKeyToolbar extends ConsumerWidget {
  final String? sessionId;

  const SpecialKeyToolbar({super.key, this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (sessionId == null) return const SizedBox.shrink();

    void send(List<int> bytes) {
      ref.read(terminalProvider.notifier)
          .sendInput(sessionId!, Uint8List.fromList(bytes));
    }

    final keys = [
      _Key('Tab',     () => send([0x09])),
      _Key('Esc',     () => send([0x1B])),
      _Key('Ctrl+C',  () => send([0x03])),
      _Key('Ctrl+D',  () => send([0x04])),
      _Key('Ctrl+Z',  () => send([0x1A])),
      _Key('Ctrl+L',  () => send([0x0C])),
      _Key('↑',     () => send([0x1B, 0x5B, 0x41])),
      _Key('↓',     () => send([0x1B, 0x5B, 0x42])),
      _Key('←',     () => send([0x1B, 0x5B, 0x44])),
      _Key('→',     () => send([0x1B, 0x5B, 0x43])),
      _Key('PgUp',  () => send([0x1B, 0x5B, 0x35, 0x7E])),
      _Key('PgDn',  () => send([0x1B, 0x5B, 0x36, 0x7E])),
    ];

    return Container(
      height: 44,
      color: Theme.of(context).colorScheme.surfaceContainerHighest,
      child: ListView.separated(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 8),
        itemCount: keys.length,
        separatorBuilder: (_, __) => const SizedBox(width: 4),
        itemBuilder: (_, i) => Center(
          child: TextButton(
            style: TextButton.styleFrom(
              minimumSize: const Size(48, 36),
              padding: const EdgeInsets.symmetric(horizontal: 10),
              tapTargetSize: MaterialTapTargetSize.shrinkWrap,
            ),
            onPressed: keys[i].onTap,
            child: Text(keys[i].label,
                style: const TextStyle(fontFamily: 'monospace', fontSize: 13)),
          ),
        ),
      ),
    );
  }
}

class _Key {
  final String label;
  final VoidCallback onTap;
  const _Key(this.label, this.onTap);
}
