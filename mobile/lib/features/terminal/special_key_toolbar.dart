import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/theme.dart';
import 'modifier_provider.dart';
import 'terminal_provider.dart';
import 'tmux_prefix_button.dart';

class SpecialKeyToolbar extends ConsumerWidget {
  final String? sessionId;

  const SpecialKeyToolbar({super.key, this.sessionId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (sessionId == null) return const SizedBox.shrink();

    final modifier   = ref.watch(modifierProvider);
    final termState  = ref.watch(terminalProvider).valueOrNull;
    final tmuxBacked = termState?.sessions[sessionId]?.tmuxBacked ?? false;

    void send(List<int> bytes) {
      ref.read(modifierProvider.notifier).state = ArmedModifier.none;
      ref.read(terminalProvider.notifier)
          .sendInput(sessionId!, Uint8List.fromList(bytes));
    }

    void toggleModifier(ArmedModifier m) {
      ref.read(modifierProvider.notifier).state =
          modifier == m ? ArmedModifier.none : m;
    }

    Widget modifierBtn(String label, ArmedModifier m) {
      final armed = modifier == m;
      return _ToolbarKey(
        label: label,
        armed: armed,
        armedColor: CcmuxColors.accent,
        onTap: () => toggleModifier(m),
      );
    }

    Widget plainBtn(String label, List<int> bytes, {bool isArrow = false}) {
      return _ToolbarKey(
        label: label,
        isArrow: isArrow,
        onTap: () => send(bytes),
      );
    }

    Widget ctrlCBtn() => _ToolbarKey(
      label: 'Ctrl+C',
      keyColor: CcmuxColors.red,
      onTap: () => send([0x03]),
    );

    final items = <Widget>[
      plainBtn('Tab',  [0x09]),
      plainBtn('Esc',  [0x1B]),
      modifierBtn('Ctrl', ArmedModifier.ctrl),
      modifierBtn('Alt',  ArmedModifier.alt),
      ctrlCBtn(),
      plainBtn('←', [0x1B, 0x5B, 0x44], isArrow: true),
      plainBtn('→', [0x1B, 0x5B, 0x43], isArrow: true),
      plainBtn('↑', [0x1B, 0x5B, 0x41], isArrow: true),
      plainBtn('↓', [0x1B, 0x5B, 0x42], isArrow: true),
      plainBtn('PgUp', [0x1B, 0x5B, 0x35, 0x7E]),
      plainBtn('PgDn', [0x1B, 0x5B, 0x36, 0x7E]),
      if (tmuxBacked) TmuxPrefixButton(sessionId: sessionId!),
    ];

    return Container(
      height: 44,
      color: CcmuxColors.bgToolbar,
      child: ListView.separated(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
        itemCount: items.length,
        separatorBuilder: (_, __) => const SizedBox(width: 5),
        itemBuilder: (_, i) => items[i],
      ),
    );
  }
}

class _ToolbarKey extends StatefulWidget {
  final String label;
  final bool isArrow;
  final bool armed;
  final Color? armedColor;
  final Color? keyColor;
  final VoidCallback onTap;

  const _ToolbarKey({
    required this.label,
    required this.onTap,
    this.isArrow = false,
    this.armed = false,
    this.armedColor,
    this.keyColor,
  });

  @override
  State<_ToolbarKey> createState() => _ToolbarKeyState();
}

class _ToolbarKeyState extends State<_ToolbarKey> {
  bool _pressed = false;

  @override
  Widget build(BuildContext context) {
    final color = widget.keyColor ?? (widget.armed ? widget.armedColor : null);
    final bgDefault = color != null
        ? color.withOpacity(0.09)
        : const Color(0xFF1A1A1E);
    final bgPressed = color != null
        ? color.withOpacity(0.2)
        : const Color(0xFF252529);
    final textColor = color ?? (widget.armed ? (widget.armedColor ?? Colors.white) : const Color(0xFF888888));
    final borderColor = color != null
        ? color.withOpacity(0.2)
        : const Color(0xFF0FFFFFFF);

    final isWide = widget.label.length > 3;

    return GestureDetector(
      onTapDown: (_) => setState(() => _pressed = true),
      onTapUp: (_) { setState(() => _pressed = false); widget.onTap(); },
      onTapCancel: () => setState(() => _pressed = false),
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 80),
        constraints: BoxConstraints(minWidth: isWide ? 52 : 38),
        height: 34,
        padding: const EdgeInsets.symmetric(horizontal: 6),
        decoration: BoxDecoration(
          color: _pressed ? bgPressed : bgDefault,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: borderColor, width: 1),
        ),
        alignment: Alignment.center,
        child: Text(
          widget.label,
          style: TextStyle(
            fontFamily: 'monospace',
            fontSize: widget.isArrow ? 16 : 11,
            fontWeight: widget.keyColor != null ? FontWeight.w600 : FontWeight.w500,
            color: _pressed ? (color ?? Colors.white) : textColor,
          ),
        ),
      ),
    );
  }
}
