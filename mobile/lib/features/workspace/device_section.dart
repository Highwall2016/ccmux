import 'package:flutter/material.dart';
import '../../core/api/api_models.dart';

class DeviceSection extends StatelessWidget {
  final DeviceModel device;
  final List<SessionModel> sessions;
  final void Function(String sessionId) onSessionTap;

  const DeviceSection({
    super.key,
    required this.device,
    required this.sessions,
    required this.onSessionTap,
  });

  @override
  Widget build(BuildContext context) {
    final online = device.isOnline;
    return ExpansionTile(
      leading: Icon(
        Icons.computer,
        color: online ? Colors.green : Colors.grey,
      ),
      title: Text(device.name, style: const TextStyle(fontWeight: FontWeight.w600)),
      subtitle: Text(online ? 'online' : 'offline',
          style: TextStyle(color: online ? Colors.green : Colors.grey, fontSize: 12)),
      initiallyExpanded: online,
      children: sessions.isEmpty
          ? [
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                child: Text('No sessions', style: TextStyle(color: Colors.grey)),
              )
            ]
          : sessions.map((s) {
              final isActive = s.isActive;
              return ListTile(
                contentPadding: const EdgeInsets.symmetric(horizontal: 32),
                leading: Icon(
                  Icons.terminal,
                  size: 18,
                  color: isActive ? null : Colors.grey,
                ),
                title: Text(
                  s.name.isNotEmpty ? s.name : s.command,
                  style: TextStyle(
                    fontFamily: 'monospace',
                    color: isActive ? null : Colors.grey,
                  ),
                  overflow: TextOverflow.ellipsis,
                ),
                subtitle: isActive
                    ? null
                    : Text('exited (${s.exitCode ?? '?'})',
                        style: const TextStyle(fontSize: 11, color: Colors.red)),
                onTap: isActive ? () => onSessionTap(s.id) : null,
              );
            }).toList(),
    );
  }
}
