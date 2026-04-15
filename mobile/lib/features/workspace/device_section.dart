import 'package:flutter/material.dart';
import '../../core/api/api_models.dart';

class DeviceSection extends StatelessWidget {
  final DeviceModel device;
  final List<SessionModel> sessions;
  final void Function(String sessionId, String sessionName) onSessionTap;
  final void Function(String sessionId, String currentName) onRenameSession;
  final void Function(String sessionId) onKillSession;

  const DeviceSection({
    super.key,
    required this.device,
    required this.sessions,
    required this.onSessionTap,
    required this.onRenameSession,
    required this.onKillSession,
  });

  @override
  Widget build(BuildContext context) {
    final online = device.isOnline;
    // Only show active sessions; exited/killed ones are hidden.
    final activeSessions = sessions.where((s) => s.isActive).toList();

    return ExpansionTile(
      leading: Icon(
        Icons.computer,
        color: online ? Colors.green : Colors.grey,
      ),
      title: Text(device.name, style: const TextStyle(fontWeight: FontWeight.w600)),
      subtitle: Text(online ? 'online' : 'offline',
          style: TextStyle(color: online ? Colors.green : Colors.grey, fontSize: 12)),
      initiallyExpanded: online,
      children: activeSessions.isEmpty
          ? [
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                child: Text('No active sessions', style: TextStyle(color: Colors.grey)),
              )
            ]
          : activeSessions.map((s) {
              final label = s.name.isNotEmpty ? s.name : s.command;
              return ListTile(
                contentPadding: const EdgeInsets.symmetric(horizontal: 32),
                leading: const Icon(Icons.terminal, size: 18),
                title: Text(
                  label,
                  style: const TextStyle(fontFamily: 'monospace'),
                  overflow: TextOverflow.ellipsis,
                ),
                onTap: () => onSessionTap(s.id, s.name.isNotEmpty ? s.name : s.command),
                onLongPress: () => _showSessionMenu(context, s),
                trailing: IconButton(
                  icon: const Icon(Icons.more_vert, size: 18),
                  onPressed: () => _showSessionMenu(context, s),
                ),
              );
            }).toList(),
    );
  }

  void _showSessionMenu(BuildContext context, SessionModel s) {
    final label = s.name.isNotEmpty ? s.name : s.command;
    showModalBottomSheet<void>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.drive_file_rename_outline),
              title: Text('Rename "$label"'),
              onTap: () {
                Navigator.pop(ctx);
                onRenameSession(s.id, s.name.isNotEmpty ? s.name : s.command);
              },
            ),
            ListTile(
              leading: const Icon(Icons.stop_circle_outlined, color: Colors.red),
              title: Text('Kill "$label"', style: const TextStyle(color: Colors.red)),
              onTap: () {
                Navigator.pop(ctx);
                onKillSession(s.id);
              },
            ),
          ],
        ),
      ),
    );
  }
}
