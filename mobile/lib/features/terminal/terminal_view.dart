import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:xterm/xterm.dart';
import 'terminal_provider.dart';

class TerminalView extends ConsumerStatefulWidget {
  final String sessionId;

  const TerminalView({super.key, required this.sessionId});

  @override
  ConsumerState<TerminalView> createState() => _TerminalViewState();
}

class _TerminalViewState extends ConsumerState<TerminalView> {
  late final TerminalController _controller;
  double _fontSize = 14.0;
  // Last known size to avoid sending duplicate resize events.
  int _lastCols = 0;
  int _lastRows = 0;

  @override
  void initState() {
    super.initState();
    _controller = TerminalController();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _onResize(int cols, int rows) {
    if (cols == _lastCols && rows == _lastRows) return;
    _lastCols = cols;
    _lastRows = rows;
    ref.read(terminalProvider.notifier).sendResize(widget.sessionId, cols, rows);
  }

  @override
  Widget build(BuildContext context) {
    final termState = ref.watch(terminalProvider).valueOrNull;
    final sess = termState?.sessions[widget.sessionId];
    if (sess == null) return const SizedBox.shrink();

    return GestureDetector(
      onScaleUpdate: (details) {
        if (details.scale == 1.0) return;
        setState(() {
          _fontSize = (_fontSize * details.scale).clamp(8.0, 32.0);
        });
      },
      child: LayoutBuilder(
        builder: (context, constraints) {
          return xterm.TerminalView(
            sess.terminal,
            controller: _controller,
            theme: TerminalThemes.defaultTheme,
            textStyle: TerminalTextStyle(fontSize: _fontSize),
            onOutput: (data) {
              // User typed — send input to the backend.
              ref.read(terminalProvider.notifier).sendInput(
                widget.sessionId,
                Uint8List.fromList(data.codeUnits),
              );
            },
            onResize: _onResize,
          );
        },
      ),
    );
  }
}
