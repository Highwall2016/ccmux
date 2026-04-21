import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:xterm/xterm.dart' as xterm;
import 'swipe_pref_provider.dart';
import 'terminal_provider.dart';

class TerminalView extends ConsumerStatefulWidget {
  final String sessionId;

  const TerminalView({super.key, required this.sessionId});

  @override
  ConsumerState<TerminalView> createState() => _TerminalViewState();
}

class _TerminalViewState extends ConsumerState<TerminalView> {
  late final xterm.TerminalController _controller;
  double _fontSize = 14.0;

  @override
  void initState() {
    super.initState();
    _controller = xterm.TerminalController();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _sendBytes(List<int> bytes) {
    ref.read(terminalProvider.notifier)
        .sendInput(widget.sessionId, Uint8List.fromList(bytes));
  }

  @override
  Widget build(BuildContext context) {
    final termState   = ref.watch(terminalProvider).valueOrNull;
    final sess        = termState?.sessions[widget.sessionId];
    if (sess == null) return const SizedBox.shrink();

    final swipeEnabled = ref.watch(swipePrefProvider).valueOrNull ?? false;
    final tmuxBacked   = sess.tmuxBacked;

    return GestureDetector(
      onScaleUpdate: (details) {
        if (details.scale == 1.0) return;
        setState(() {
          _fontSize = (_fontSize * details.scale).clamp(8.0, 32.0);
        });
      },
      onHorizontalDragEnd: (swipeEnabled && tmuxBacked)
          ? (details) {
              final vx = details.primaryVelocity ?? 0;
              if (vx.abs() < 300) return; // ignore slow drags
              if (vx < 0) {
                // Swipe left → next window (Ctrl+B n)
                _sendBytes([0x02, 0x6E]);
              } else {
                // Swipe right → previous window (Ctrl+B p)
                _sendBytes([0x02, 0x70]);
              }
            }
          : null,
      child: LayoutBuilder(
        builder: (context, constraints) {
          return xterm.TerminalView(
            sess.terminal,
            controller: _controller,
            theme: xterm.TerminalThemes.defaultTheme,
            textStyle: xterm.TerminalStyle(fontSize: _fontSize),
          );
        },
      ),
    );
  }
}
