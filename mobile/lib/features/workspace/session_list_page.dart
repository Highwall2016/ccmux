import 'dart:async';
import 'dart:math';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../core/theme.dart';
import '../../core/api/api_models.dart';
import '../auth/auth_provider.dart';
import '../terminal/terminal_provider.dart';
import 'workspace_provider.dart';

// ── helpers ────────────────────────────────────────────────────────────────

String _relTime(String iso) {
  final dt = DateTime.tryParse(iso);
  if (dt == null) return '';
  final d = DateTime.now().difference(dt);
  if (d.inSeconds < 60) return '${d.inSeconds}s';
  if (d.inMinutes < 60) return '${d.inMinutes}m';
  if (d.inHours < 24) return '${d.inHours}h';
  return '${d.inDays}d';
}

// ── Avatar ──────────────────────────────────────────────────────────────────

String _initials(String name, String command) {
  final s = (name.isNotEmpty ? name : command).toUpperCase();
  if (s.isEmpty) return '??';
  if (s.length == 1) return s;
  return s.substring(0, 2);
}

class _SessionAvatar extends StatelessWidget {
  final SessionModel session;
  const _SessionAvatar({required this.session});

  @override
  Widget build(BuildContext context) {
    final color = CcmuxColors.forName(
        session.name.isNotEmpty ? session.name : session.command);
    final initials = _initials(session.name, session.command);
    final dotColor = CcmuxColors.statusDot(session.status, session.exitCode);

    return SizedBox(
      width: 46,
      height: 46,
      child: Stack(
        children: [
          Container(
            width: 46,
            height: 46,
            decoration: BoxDecoration(
              color: color.withOpacity(0.13),
              borderRadius: BorderRadius.circular(13),
              border: Border.all(color: color.withOpacity(0.27), width: 1.5),
            ),
            alignment: Alignment.center,
            child: Text(
              initials,
              style: TextStyle(
                fontFamily: 'monospace',
                fontSize: 13,
                fontWeight: FontWeight.w700,
                color: color,
                letterSpacing: -0.5,
              ),
            ),
          ),
          Positioned(
            bottom: 0,
            right: 0,
            child: Container(
              width: 10,
              height: 10,
              decoration: BoxDecoration(
                color: dotColor,
                shape: BoxShape.circle,
                border: Border.all(color: CcmuxColors.bg, width: 2),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

// ── Resource Widget ──────────────────────────────────────────────────────────

class _ResourceWidget extends StatefulWidget {
  final bool active;
  const _ResourceWidget({required this.active});

  @override
  State<_ResourceWidget> createState() => _ResourceWidgetState();
}

class _ResourceWidgetState extends State<_ResourceWidget> {
  final _rng = Random();
  double _cpu = 20;
  double _mem = 120;
  late final int _memTotalMb; // simulated device total RAM
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    // Pick a realistic total RAM from common device sizes (8/16/32 GB).
    const totals = [8192, 16384, 32768];
    _memTotalMb = totals[_rng.nextInt(totals.length)];
    _mem = (_memTotalMb * 0.15).clamp(256, 4096).toDouble();
    if (widget.active) _startTimer();
  }

  @override
  void didUpdateWidget(_ResourceWidget old) {
    super.didUpdateWidget(old);
    if (widget.active && _timer == null) _startTimer();
    if (!widget.active) {
      _timer?.cancel();
      _timer = null;
    }
  }

  void _startTimer() {
    _timer = Timer.periodic(const Duration(milliseconds: 1200), (_) {
      if (!mounted) return;
      setState(() {
        _cpu = (_cpu + (_rng.nextDouble() - 0.5) * 14).clamp(1, 99);
        _mem = (_mem + (_rng.nextDouble() - 0.5) * 80)
            .clamp(256, _memTotalMb * 0.85);
      });
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  String _fmtMem(double mb) {
    if (mb >= 1024) return '${(mb / 1024).toStringAsFixed(1)}G';
    return '${mb.round()}M';
  }

  @override
  Widget build(BuildContext context) {
    final cpuColor = _cpu > 80
        ? CcmuxColors.red
        : _cpu > 50
            ? CcmuxColors.yellow
            : CcmuxColors.accent;
    final memColor = (_mem / _memTotalMb) > 0.8
        ? CcmuxColors.red
        : (_mem / _memTotalMb) > 0.6
            ? CcmuxColors.yellow
            : CcmuxColors.blue;

    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        _chip('CPU', '${_cpu.round()}%', cpuColor),
        const SizedBox(width: 4),
        _chip('MEM', '${_fmtMem(_mem)}/${_fmtMem(_memTotalMb.toDouble())}',
            memColor),
      ],
    );
  }

  Widget _chip(String label, String value, Color color) => Container(
        padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 2),
        decoration: BoxDecoration(
          color: Colors.white.withOpacity(0.05),
          borderRadius: BorderRadius.circular(5),
        ),
        child: Text(
          '$label $value',
          style: TextStyle(
            fontFamily: 'monospace',
            fontSize: 9,
            fontWeight: FontWeight.w600,
            color: color,
            letterSpacing: 0.2,
          ),
        ),
      );
}

// ── Session Row ─────────────────────────────────────────────────────────────

class _SessionRow extends ConsumerWidget {
  final SessionModel session;
  final DeviceModel device;
  final VoidCallback onTap;
  final VoidCallback onKill;
  final VoidCallback onRename;

  const _SessionRow({
    required this.session,
    required this.device,
    required this.onTap,
    required this.onKill,
    required this.onRename,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final termState = ref.watch(terminalProvider).valueOrNull;
    final hasNew = termState?.sessions[session.id]?.hasNewOutput ?? false;
    final isError = session.exitCode != null && session.exitCode != 0;
    final isEnded = !session.isActive;

    // Ended sessions: swipe removes from list. Active: swipe kills the process.
    final swipeColor = isEnded ? const Color(0xFF3A3A3F) : CcmuxColors.red;
    final swipeIcon =
        isEnded ? Icons.remove_circle_outline : Icons.delete_outline;
    final swipeLabel = isEnded ? 'Remove' : 'Kill';

    return Dismissible(
      key: Key(session.id),
      direction: DismissDirection.endToStart,
      background: Container(
        color: swipeColor,
        alignment: Alignment.centerRight,
        padding: const EdgeInsets.only(right: 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(swipeIcon, color: Colors.white, size: 20),
            const SizedBox(height: 2),
            Text(swipeLabel,
                style: const TextStyle(
                    color: Colors.white,
                    fontSize: 10,
                    fontWeight: FontWeight.w700)),
          ],
        ),
      ),
      confirmDismiss: (_) async {
        if (isEnded)
          return true; // no confirmation needed for already-dead sessions
        return await showDialog<bool>(
              context: context,
              builder: (ctx) => _ConfirmKillDialog(name: session.name),
            ) ??
            false;
      },
      onDismissed: (_) => onKill(),
      child: GestureDetector(
        onTap: onTap,
        onLongPress: () => _showContextSheet(context, ref),
        child: Container(
          decoration: const BoxDecoration(
            color: CcmuxColors.bg,
            border: Border(bottom: BorderSide(color: CcmuxColors.divider)),
          ),
          padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 13),
          child: Row(
            children: [
              _SessionAvatar(session: session),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            session.name.isNotEmpty
                                ? session.name
                                : session.command,
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 14,
                              fontWeight:
                                  hasNew ? FontWeight.w700 : FontWeight.w500,
                              color: hasNew
                                  ? CcmuxColors.text
                                  : CcmuxColors.textMuted,
                              letterSpacing: -0.3,
                            ),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        const SizedBox(width: 6),
                        Text(
                          _relTime(session.lastActivity),
                          style: const TextStyle(
                              fontSize: 11, color: Color(0xFF3D3D42)),
                        ),
                        if (hasNew) ...[
                          const SizedBox(width: 6),
                          Container(
                            width: 8,
                            height: 8,
                            decoration: BoxDecoration(
                              color: isError
                                  ? CcmuxColors.red
                                  : CcmuxColors.accent,
                              shape: BoxShape.circle,
                            ),
                          ),
                        ],
                      ],
                    ),
                    const SizedBox(height: 3),
                    Text(
                      session.command,
                      style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 12,
                        color: hasNew
                            ? CcmuxColors.textSub
                            : CcmuxColors.textFaint,
                        fontWeight: hasNew ? FontWeight.w500 : FontWeight.w400,
                        letterSpacing: -0.2,
                      ),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _showContextSheet(BuildContext context, WidgetRef ref) {
    showModalBottomSheet(
      context: context,
      backgroundColor: CcmuxColors.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (_) => _ContextSheet(
        session: session,
        onRename: () {
          Navigator.pop(context);
          onRename();
        },
        onKill: () {
          Navigator.pop(context);
          onKill();
        },
      ),
    );
  }
}

// ── Context Sheet ────────────────────────────────────────────────────────────

class _ContextSheet extends StatelessWidget {
  final SessionModel session;
  final VoidCallback onRename;
  final VoidCallback onKill;

  const _ContextSheet(
      {required this.session, required this.onRename, required this.onKill});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 36),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Center(
            child: Container(
              width: 36,
              height: 4,
              decoration: BoxDecoration(
                color: const Color(0xFF333333),
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
          const SizedBox(height: 20),
          Row(
            children: [
              Text(
                session.name.isNotEmpty ? session.name : session.command,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 15,
                  fontWeight: FontWeight.w700,
                  color: CcmuxColors.text,
                ),
              ),
              const SizedBox(width: 10),
              Text(
                session.command,
                style: const TextStyle(
                  fontFamily: 'monospace',
                  fontSize: 11,
                  color: CcmuxColors.textDim,
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          if (session.isActive)
            _sheetAction(context,
                icon: Icons.edit_outlined, label: 'Rename', onTap: onRename),
          if (session.isActive) const SizedBox(height: 6),
          _sheetAction(
            context,
            icon: session.isActive
                ? Icons.stop_circle_outlined
                : Icons.remove_circle_outline,
            label: session.isActive ? 'Kill session' : 'Remove',
            color: session.isActive ? CcmuxColors.red : CcmuxColors.textDim,
            onTap: onKill,
          ),
          const SizedBox(height: 8),
          GestureDetector(
            onTap: () => Navigator.pop(context),
            child: Center(
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text('Cancel',
                    style: TextStyle(fontSize: 13, color: CcmuxColors.textDim)),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _sheetAction(
    BuildContext context, {
    required IconData icon,
    required String label,
    Color? color,
    required VoidCallback onTap,
  }) {
    final c = color ?? CcmuxColors.textSub;
    return GestureDetector(
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
        decoration: BoxDecoration(
          color: color != null
              ? color.withOpacity(0.08)
              : Colors.white.withOpacity(0.04),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(
            color: color != null
                ? color.withOpacity(0.2)
                : Colors.white.withOpacity(0.07),
          ),
        ),
        child: Row(
          children: [
            Icon(icon, color: c, size: 18),
            const SizedBox(width: 14),
            Text(label,
                style: TextStyle(
                    fontSize: 14, fontWeight: FontWeight.w500, color: c)),
          ],
        ),
      ),
    );
  }
}

// ── Confirm Kill Dialog ──────────────────────────────────────────────────────

class _ConfirmKillDialog extends StatelessWidget {
  final String name;
  const _ConfirmKillDialog({required this.name});

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      backgroundColor: CcmuxColors.surface,
      title: Text('Kill "$name"?',
          style: const TextStyle(
              color: CcmuxColors.text, fontWeight: FontWeight.w700)),
      content: const Text('This will terminate the process immediately.',
          style: TextStyle(color: CcmuxColors.textSub)),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: const Text('Cancel',
              style: TextStyle(color: CcmuxColors.textMuted)),
        ),
        TextButton(
          style: TextButton.styleFrom(foregroundColor: CcmuxColors.red),
          onPressed: () => Navigator.pop(context, true),
          child:
              const Text('Kill', style: TextStyle(fontWeight: FontWeight.w700)),
        ),
      ],
    );
  }
}

// ── Device Switcher Sheet ────────────────────────────────────────────────────

class _DeviceSwitcherSheet extends ConsumerWidget {
  final List<DeviceModel> devices;
  final WorkspaceState ws;

  const _DeviceSwitcherSheet({required this.devices, required this.ws});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selectedId = ref.watch(selectedDeviceIdProvider);

    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 36),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Center(
            child: Container(
              width: 36,
              height: 4,
              decoration: BoxDecoration(
                color: const Color(0xFF333333),
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
          const SizedBox(height: 20),
          const Text('Switch Device',
              style: TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w700,
                color: CcmuxColors.text,
                letterSpacing: -0.3,
              )),
          const SizedBox(height: 16),
          ...devices.map((dev) {
            final devSessions = ws.sessionsByDevice[dev.id] ?? [];
            final activeCount = devSessions.where((s) => s.isActive).length;
            final firstId = devices.isNotEmpty ? devices.first.id : null;
            final isCurrent = (selectedId ?? firstId) == dev.id;

            return Padding(
              padding: const EdgeInsets.only(bottom: 6),
              child: GestureDetector(
                onTap: () {
                  ref.read(selectedDeviceIdProvider.notifier).state = dev.id;
                  Navigator.pop(context);
                },
                child: Container(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                  decoration: BoxDecoration(
                    color: isCurrent
                        ? CcmuxColors.accent.withOpacity(0.09)
                        : Colors.white.withOpacity(0.04),
                    borderRadius: BorderRadius.circular(14),
                    border: Border.all(
                      color: isCurrent
                          ? CcmuxColors.accent.withOpacity(0.27)
                          : Colors.white.withOpacity(0.07),
                      width: 1.5,
                    ),
                  ),
                  child: Row(
                    children: [
                      Container(
                        width: 40,
                        height: 40,
                        decoration: BoxDecoration(
                          color: Colors.white.withOpacity(0.06),
                          borderRadius: BorderRadius.circular(12),
                        ),
                        child: Icon(
                          dev.platform == 'macos'
                              ? Icons.laptop_mac
                              : Icons.dns_outlined,
                          color: isCurrent
                              ? CcmuxColors.accent
                              : const Color(0xFF666666),
                          size: 18,
                        ),
                      ),
                      const SizedBox(width: 14),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(dev.name,
                                style: TextStyle(
                                  fontSize: 14,
                                  fontWeight: FontWeight.w600,
                                  color: isCurrent
                                      ? CcmuxColors.text
                                      : CcmuxColors.textSub,
                                  letterSpacing: -0.2,
                                )),
                            const SizedBox(height: 2),
                            Text(
                              dev.online
                                  ? '$activeCount active session${activeCount != 1 ? 's' : ''}'
                                  : 'offline',
                              style: const TextStyle(
                                fontFamily: 'monospace',
                                fontSize: 11,
                                color: CcmuxColors.textFaint,
                              ),
                            ),
                          ],
                        ),
                      ),
                      Container(
                        width: 8,
                        height: 8,
                        decoration: BoxDecoration(
                          color: dev.online
                              ? CcmuxColors.accent
                              : const Color(0xFF444444),
                          shape: BoxShape.circle,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            );
          }),
          const SizedBox(height: 8),
          GestureDetector(
            onTap: () => Navigator.pop(context),
            child: Center(
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text('Cancel',
                    style: const TextStyle(
                        fontSize: 13, color: CcmuxColors.textDim)),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

// ── Settings Sheet ───────────────────────────────────────────────────────────

class _SettingsSheet extends ConsumerWidget {
  const _SettingsSheet();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 20, 20, 36),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Center(
            child: Container(
              width: 36,
              height: 4,
              decoration: BoxDecoration(
                color: const Color(0xFF333333),
                borderRadius: BorderRadius.circular(2),
              ),
            ),
          ),
          const SizedBox(height: 20),
          const Align(
            alignment: Alignment.centerLeft,
            child: Text('Settings',
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                  color: CcmuxColors.text,
                  letterSpacing: -0.3,
                )),
          ),
          const SizedBox(height: 16),
          _settingRow(
            icon: Icons.person_outline,
            label: 'Account',
            sub: 'Manage your profile',
          ),
          const SizedBox(height: 6),
          _settingRow(
            icon: Icons.star_outline,
            label: 'Upgrade to Pro',
            sub: 'Unlimited devices & sessions',
            highlight: CcmuxColors.yellow,
          ),
          const SizedBox(height: 6),
          _settingRow(
            icon: Icons.info_outline,
            label: 'About ccmux',
            sub: 'Version & release notes',
          ),
          const SizedBox(height: 6),
          _settingRow(
            icon: Icons.help_outline,
            label: 'Help & Docs',
            sub: 'github.com/ccmux',
          ),
          const SizedBox(height: 8),
          GestureDetector(
            onTap: () {
              Navigator.pop(context);
              ref.read(authProvider.notifier).logout();
            },
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
              decoration: BoxDecoration(
                color: CcmuxColors.red.withOpacity(0.08),
                borderRadius: BorderRadius.circular(14),
                border: Border.all(color: CcmuxColors.red.withOpacity(0.2)),
              ),
              child: Row(
                children: [
                  Container(
                    width: 36,
                    height: 36,
                    decoration: BoxDecoration(
                      color: CcmuxColors.red.withOpacity(0.1),
                      borderRadius: BorderRadius.circular(10),
                    ),
                    child: const Icon(Icons.logout,
                        color: CcmuxColors.red, size: 18),
                  ),
                  const SizedBox(width: 14),
                  const Text('Sign Out',
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w600,
                        color: CcmuxColors.red,
                      )),
                ],
              ),
            ),
          ),
          const SizedBox(height: 8),
          GestureDetector(
            onTap: () => Navigator.pop(context),
            child: Center(
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Text('Close',
                    style: const TextStyle(
                        fontSize: 13, color: CcmuxColors.textDim)),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _settingRow({
    required IconData icon,
    required String label,
    required String sub,
    Color? highlight,
  }) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: highlight != null
            ? highlight.withOpacity(0.07)
            : Colors.white.withOpacity(0.04),
        borderRadius: BorderRadius.circular(14),
        border: Border.all(
          color: highlight != null
              ? highlight.withOpacity(0.27)
              : Colors.white.withOpacity(0.07),
          width: 1.5,
        ),
      ),
      child: Row(
        children: [
          Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(
              color: Colors.white.withOpacity(0.06),
              borderRadius: BorderRadius.circular(10),
            ),
            child:
                Icon(icon, color: highlight ?? CcmuxColors.textSub, size: 18),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(label,
                    style: TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                      color: highlight ?? CcmuxColors.textSub,
                      letterSpacing: -0.2,
                    )),
                const SizedBox(height: 1),
                Text(sub,
                    style: const TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 11,
                      color: CcmuxColors.textDim,
                    )),
              ],
            ),
          ),
          const Icon(Icons.chevron_right, color: Color(0xFF444444), size: 18),
        ],
      ),
    );
  }
}

// ── Section Label ────────────────────────────────────────────────────────────

class _SectionLabel extends StatelessWidget {
  final String label;
  const _SectionLabel(this.label);

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 5),
      child: Text(
        label.toUpperCase(),
        style: const TextStyle(
          fontSize: 10.5,
          fontWeight: FontWeight.w600,
          color: Color(0xFF3A3A3F),
          letterSpacing: 1,
        ),
      ),
    );
  }
}

// ── Sheet helpers ─────────────────────────────────────────────────────────────

Widget _sheetHandle() => Center(
      child: Container(
        width: 36,
        height: 4,
        decoration: BoxDecoration(
          color: const Color(0xFF333333),
          borderRadius: BorderRadius.circular(2),
        ),
      ),
    );

Widget _sheetInput({
  required TextEditingController controller,
  required String hint,
  bool autofocus = false,
}) {
  return Container(
    decoration: BoxDecoration(
      color: CcmuxColors.bg,
      borderRadius: BorderRadius.circular(12),
      border: Border.all(color: CcmuxColors.accent.withOpacity(0.33)),
    ),
    padding: const EdgeInsets.symmetric(horizontal: 12),
    child: Row(
      children: [
        const Text('\$',
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 13,
              color: CcmuxColors.accent,
            )),
        const SizedBox(width: 8),
        Expanded(
          child: TextField(
            controller: controller,
            autofocus: autofocus,
            style: const TextStyle(
              fontFamily: 'monospace',
              fontSize: 14,
              color: CcmuxColors.text,
              letterSpacing: -0.3,
            ),
            decoration: InputDecoration(
              border: InputBorder.none,
              hintText: hint,
              hintStyle: const TextStyle(
                  color: CcmuxColors.textDim, fontFamily: 'monospace'),
              contentPadding: const EdgeInsets.symmetric(vertical: 12),
            ),
          ),
        ),
      ],
    ),
  );
}

Row _sheetButtons({
  required String cancel,
  required String confirm,
  required VoidCallback onCancel,
  required VoidCallback onConfirm,
}) {
  return Row(children: [
    Expanded(
      child: GestureDetector(
        onTap: onCancel,
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 13),
          decoration: BoxDecoration(
            color: const Color(0x0FFFFFFF),
            borderRadius: BorderRadius.circular(12),
          ),
          alignment: Alignment.center,
          child: Text(cancel,
              style: const TextStyle(
                  color: CcmuxColors.textMuted,
                  fontSize: 14,
                  fontWeight: FontWeight.w500)),
        ),
      ),
    ),
    const SizedBox(width: 10),
    Expanded(
      child: GestureDetector(
        onTap: onConfirm,
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 13),
          decoration: BoxDecoration(
            color: CcmuxColors.accent,
            borderRadius: BorderRadius.circular(12),
          ),
          alignment: Alignment.center,
          child: Text(confirm,
              style: const TextStyle(
                  color: Colors.black,
                  fontSize: 14,
                  fontWeight: FontWeight.w700)),
        ),
      ),
    ),
  ]);
}

// ── Spawn Session Sheet ──────────────────────────────────────────────────────

class _SpawnSheet extends StatefulWidget {
  final DeviceModel device;
  final bool hasTmux;

  const _SpawnSheet({required this.device, required this.hasTmux});

  @override
  State<_SpawnSheet> createState() => _SpawnSheetState();
}

class _SpawnSheetState extends State<_SpawnSheet> {
  final _commandCtrl = TextEditingController(text: 'bash');
  final _nameCtrl = TextEditingController();
  late bool _useTmux;
  bool _tmuxSplit = false;

  @override
  void initState() {
    super.initState();
    _useTmux = widget.hasTmux;
  }

  @override
  void dispose() {
    _commandCtrl.dispose();
    _nameCtrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(
        20,
        20,
        20,
        MediaQuery.of(context).viewInsets.bottom + 36,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _sheetHandle(),
          const SizedBox(height: 20),
          Text('New session on ${widget.device.name}',
              style: const TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w700,
                color: CcmuxColors.text,
                letterSpacing: -0.3,
              )),
          const SizedBox(height: 20),
          _sheetInput(
              controller: _commandCtrl, hint: 'Command', autofocus: true),
          const SizedBox(height: 12),
          _sheetInput(
            controller: _nameCtrl,
            hint: _useTmux ? 'Session name (auto)' : 'Name (optional)',
          ),
          if (widget.hasTmux) ...[
            const SizedBox(height: 8),
            _toggleRow(
                'Use tmux',
                _useTmux,
                (v) => setState(() {
                      _useTmux = v;
                      if (!v) _tmuxSplit = false;
                    })),
            if (_useTmux)
              _toggleRow('Split pane', _tmuxSplit,
                  (v) => setState(() => _tmuxSplit = v)),
          ],
          const SizedBox(height: 20),
          _sheetButtons(
            cancel: 'Cancel',
            confirm: 'Create',
            onCancel: () => Navigator.pop(context),
            onConfirm: () => Navigator.pop(context, (
              command: _commandCtrl.text.trim().isEmpty
                  ? 'bash'
                  : _commandCtrl.text.trim(),
              name: _nameCtrl.text.trim(),
              useTmux: _useTmux,
              tmuxSplit: _tmuxSplit,
            )),
          ),
        ],
      ),
    );
  }
}

Widget _toggleRow(String label, bool value, ValueChanged<bool> onChanged) {
  return Padding(
    padding: const EdgeInsets.only(top: 4),
    child: Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text(label,
            style: const TextStyle(fontSize: 13, color: CcmuxColors.textSub)),
        Switch(
            value: value,
            onChanged: onChanged,
            activeThumbColor: CcmuxColors.accent),
      ],
    ),
  );
}

Future<void> _showSpawnSheet(
  BuildContext context,
  WidgetRef ref,
  DeviceModel device,
  WorkspaceState ws,
) async {
  final result = await showModalBottomSheet<
      ({String command, String name, bool useTmux, bool tmuxSplit})>(
    context: context,
    isScrollControlled: true,
    backgroundColor: CcmuxColors.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => _SpawnSheet(
      device: device,
      hasTmux: ws.tmuxTreeByDevice.containsKey(device.id),
    ),
  );

  if (result == null) return;

  try {
    final sessionId = await ref.read(workspaceProvider.notifier).spawnSession(
          device.id,
          name: result.name,
          command: result.command,
          useTmux: result.useTmux,
          tmuxSplit: result.tmuxSplit,
        );
    if (context.mounted) {
      ref.read(terminalProvider.notifier).openSession(
            sessionId,
            name: result.name.isNotEmpty ? result.name : result.command,
            tmuxBacked: result.useTmux,
          );
      context.go('/terminal');
    }
  } catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Failed to create session: $e')),
      );
    }
  }
}

// ── Rename Sheet ─────────────────────────────────────────────────────────────

class _RenameSheet extends StatefulWidget {
  final SessionModel session;
  const _RenameSheet({required this.session});

  @override
  State<_RenameSheet> createState() => _RenameSheetState();
}

class _RenameSheetState extends State<_RenameSheet> {
  late final TextEditingController _ctrl;

  @override
  void initState() {
    super.initState();
    _ctrl = TextEditingController(text: widget.session.name);
  }

  @override
  void dispose() {
    _ctrl.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(
        20,
        20,
        20,
        MediaQuery.of(context).viewInsets.bottom + 36,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _sheetHandle(),
          const SizedBox(height: 20),
          const Text('Rename Session',
              style: TextStyle(
                fontSize: 16,
                fontWeight: FontWeight.w700,
                color: CcmuxColors.text,
                letterSpacing: -0.3,
              )),
          const SizedBox(height: 6),
          Text(widget.session.command,
              style: const TextStyle(
                fontFamily: 'monospace',
                fontSize: 12,
                color: CcmuxColors.textDim,
              )),
          const SizedBox(height: 18),
          _sheetInput(controller: _ctrl, hint: 'Session name', autofocus: true),
          const SizedBox(height: 16),
          _sheetButtons(
            cancel: 'Cancel',
            confirm: 'Rename',
            onCancel: () => Navigator.pop(context),
            onConfirm: () {
              final name = _ctrl.text.trim();
              if (name.isNotEmpty) Navigator.pop(context, name);
            },
          ),
        ],
      ),
    );
  }
}

Future<void> _showRenameSheet(
  BuildContext context,
  WidgetRef ref,
  DeviceModel device,
  SessionModel session,
) async {
  final newName = await showModalBottomSheet<String>(
    context: context,
    isScrollControlled: true,
    backgroundColor: CcmuxColors.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => _RenameSheet(session: session),
  );

  if (newName != null && context.mounted) {
    await ref
        .read(workspaceProvider.notifier)
        .renameSession(device.id, session.id, newName);
  }
}

// ── Main Page ────────────────────────────────────────────────────────────────

class SessionListPage extends ConsumerWidget {
  const SessionListPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final workspaceAsync = ref.watch(workspaceProvider);

    return workspaceAsync.when(
      loading: () => const Scaffold(
        backgroundColor: CcmuxColors.bg,
        body:
            Center(child: CircularProgressIndicator(color: CcmuxColors.accent)),
      ),
      error: (e, _) => Scaffold(
        backgroundColor: CcmuxColors.bg,
        body: Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.error_outline, color: CcmuxColors.red, size: 48),
              const SizedBox(height: 12),
              Text('$e',
                  style: const TextStyle(
                      color: CcmuxColors.textSub, fontSize: 13)),
              const SizedBox(height: 16),
              TextButton(
                onPressed: () => ref.invalidate(workspaceProvider),
                child: const Text('Retry',
                    style: TextStyle(color: CcmuxColors.accent)),
              ),
            ],
          ),
        ),
      ),
      data: (ws) => _SessionListContent(ws: ws),
    );
  }
}

class _SessionListContent extends ConsumerWidget {
  final WorkspaceState ws;
  const _SessionListContent({required this.ws});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selectedId = ref.watch(selectedDeviceIdProvider);

    final device = ws.devices.isEmpty
        ? null
        : (selectedId != null
            ? ws.devices.firstWhere((d) => d.id == selectedId,
                orElse: () => ws.devices.first)
            : ws.devices.first);

    if (device == null) {
      return const Scaffold(
        backgroundColor: CcmuxColors.bg,
        body: Center(
          child: Text('No devices registered',
              style: TextStyle(
                  color: CcmuxColors.textDim, fontFamily: 'monospace')),
        ),
      );
    }

    final sessions = ws.sessionsByDevice[device.id] ?? [];
    final active = sessions.where((s) => s.isActive).toList()
      ..sort((a, b) => b.lastActivity.compareTo(a.lastActivity));
    final ended = sessions.where((s) => !s.isActive).toList()
      ..sort((a, b) => b.lastActivity.compareTo(a.lastActivity));

    return Scaffold(
      backgroundColor: CcmuxColors.bg,
      body: SafeArea(
        child: Column(
          children: [
            _Header(device: device, sessions: sessions, ws: ws),
            Expanded(
              child: ListView(
                children: [
                  if (active.isNotEmpty) ...[
                    const _SectionLabel('Active'),
                    ...active.map((s) => _buildRow(context, ref, s, device)),
                  ],
                  if (ended.isNotEmpty) ...[
                    const _SectionLabel('Ended'),
                    ...ended.map((s) => _buildRow(context, ref, s, device)),
                  ],
                  if (sessions.isEmpty)
                    const Padding(
                      padding: EdgeInsets.symmetric(vertical: 60),
                      child: Center(
                        child: Text('no sessions',
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 12,
                              color: Color(0xFF333333),
                            )),
                      ),
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildRow(
    BuildContext context,
    WidgetRef ref,
    SessionModel session,
    DeviceModel device,
  ) {
    return _SessionRow(
      session: session,
      device: device,
      onTap: () {
        ref.read(terminalProvider.notifier).openSession(
              session.id,
              name: session.name.isNotEmpty ? session.name : session.command,
              tmuxBacked: session.tmuxBacked,
            );
        context.go('/terminal');
      },
      onKill: () {
        if (session.isActive) {
          ref
              .read(workspaceProvider.notifier)
              .killSession(device.id, session.id);
        } else {
          // Session already ended — remove it from local state without an API call.
          ref
              .read(workspaceProvider.notifier)
              .removeEndedSession(device.id, session.id);
        }
      },
      onRename: () => _showRenameSheet(context, ref, device, session),
    );
  }
}

// ── Header ───────────────────────────────────────────────────────────────────

class _Header extends ConsumerWidget {
  final DeviceModel device;
  final List<SessionModel> sessions;
  final WorkspaceState ws;

  const _Header(
      {required this.device, required this.sessions, required this.ws});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final activeCount = sessions.where((s) => s.isActive).length;
    final termState = ref.watch(terminalProvider).valueOrNull;
    final hasNew =
        sessions.any((s) => termState?.sessions[s.id]?.hasNewOutput ?? false);

    return Container(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 14),
      decoration: const BoxDecoration(
        color: CcmuxColors.bg,
        border: Border(bottom: BorderSide(color: CcmuxColors.divider)),
      ),
      child: Row(
        children: [
          // Settings button
          GestureDetector(
            onTap: () => showModalBottomSheet(
              context: context,
              backgroundColor: CcmuxColors.surface,
              shape: const RoundedRectangleBorder(
                borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
              ),
              builder: (_) => const _SettingsSheet(),
            ),
            child: Container(
              width: 36,
              height: 36,
              decoration: BoxDecoration(
                color: Colors.white.withOpacity(0.06),
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: Colors.white.withOpacity(0.08)),
              ),
              child: const Icon(Icons.settings_outlined,
                  color: Color(0xFF777777), size: 17),
            ),
          ),

          // Device name (center)
          Expanded(
            child: GestureDetector(
              onTap: ws.devices.length > 1
                  ? () => showModalBottomSheet(
                        context: context,
                        backgroundColor: CcmuxColors.surface,
                        shape: const RoundedRectangleBorder(
                          borderRadius:
                              BorderRadius.vertical(top: Radius.circular(24)),
                        ),
                        builder: (_) =>
                            _DeviceSwitcherSheet(devices: ws.devices, ws: ws),
                      )
                  : null,
              child: Column(
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        device.platform == 'macos'
                            ? Icons.laptop_mac
                            : Icons.dns_outlined,
                        color: CcmuxColors.accent,
                        size: 14,
                      ),
                      const SizedBox(width: 6),
                      Flexible(
                        child: Text(
                          device.name,
                          style: const TextStyle(
                            fontSize: 17,
                            fontWeight: FontWeight.w700,
                            color: CcmuxColors.text,
                            letterSpacing: -0.4,
                          ),
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      if (ws.devices.length > 1) ...[
                        const SizedBox(width: 4),
                        const Icon(Icons.keyboard_arrow_down,
                            color: Color(0xFF555555), size: 16),
                      ],
                    ],
                  ),
                  const SizedBox(height: 2),
                  Text(
                    '$activeCount active${hasNew ? ' · new output' : ''}',
                    style: const TextStyle(
                      fontSize: 11,
                      color: CcmuxColors.textDim,
                      fontFamily: 'monospace',
                    ),
                  ),
                  if (device.online) ...[
                    const SizedBox(height: 5),
                    _ResourceWidget(active: true),
                  ],
                ],
              ),
            ),
          ),

          // New session button
          GestureDetector(
            onTap: () => _showSpawnSheet(context, ref, device, ws),
            child: Container(
              width: 36,
              height: 36,
              decoration: BoxDecoration(
                color: CcmuxColors.accent.withOpacity(0.13),
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: CcmuxColors.accent.withOpacity(0.27)),
              ),
              child: const Icon(Icons.add, color: CcmuxColors.accent, size: 16),
            ),
          ),
        ],
      ),
    );
  }
}
