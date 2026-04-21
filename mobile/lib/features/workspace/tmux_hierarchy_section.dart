import 'package:flutter/material.dart';
import '../../core/api/api_models.dart';

/// Renders the tmux session → window → pane tree for one device in the
/// workspace drawer.  Bare (non-tmux) sessions are shown in a separate
/// "Direct PTY" section below the tmux tree.
class TmuxHierarchySection extends StatelessWidget {
  final DeviceModel device;
  final TmuxTree tmuxTree;
  final List<SessionModel> bareSessions;
  final void Function(String sessionId, String sessionName) onSessionTap;
  final void Function(String sessionId, String currentName) onRenameSession;
  final void Function(String sessionId) onKillSession;
  final void Function() onRemoveDevice;
  final void Function() onSpawnSession;

  const TmuxHierarchySection({
    super.key,
    required this.device,
    required this.tmuxTree,
    required this.bareSessions,
    required this.onSessionTap,
    required this.onRenameSession,
    required this.onKillSession,
    required this.onRemoveDevice,
    required this.onSpawnSession,
  });

  @override
  Widget build(BuildContext context) {
    final online = device.isOnline;
    return ExpansionTile(
      leading: Icon(Icons.computer, color: online ? Colors.green : Colors.grey),
      title: Text(device.name, style: const TextStyle(fontWeight: FontWeight.w600)),
      subtitle: Text(
        online ? 'online · tmux' : 'offline',
        style: TextStyle(
          color: online ? Colors.green : Colors.grey,
          fontSize: 12,
        ),
      ),
      trailing: PopupMenuButton<String>(
        icon: const Icon(Icons.more_vert),
        tooltip: 'Device options',
        onSelected: (value) {
          if (value == 'new') onSpawnSession();
          if (value == 'remove') onRemoveDevice();
        },
        itemBuilder: (_) => [
          if (online)
            const PopupMenuItem(
              value: 'new',
              child: Row(children: [
                Icon(Icons.add, size: 20),
                SizedBox(width: 8),
                Text('New session'),
              ]),
            ),
          const PopupMenuItem(
            value: 'remove',
            child: Row(children: [
              Icon(Icons.delete_outline, color: Colors.red, size: 20),
              SizedBox(width: 8),
              Text('Remove device', style: TextStyle(color: Colors.red)),
            ]),
          ),
        ],
      ),
      initiallyExpanded: online,
      children: [
        // tmux session → window → pane tree.
        for (final sess in tmuxTree.sessions) ...[
          _TmuxSessionGroup(
            sessionNode: sess,
            onPaneTap: onSessionTap,
            onPaneLongPress: (id, name) => _showPaneMenu(context, id, name),
          ),
        ],
        // Bare (non-tmux) PTY sessions, if any.
        if (bareSessions.isNotEmpty) ...[
          const Padding(
            padding: EdgeInsets.only(left: 16, top: 8, bottom: 4),
            child: Row(children: [
              Icon(Icons.terminal, size: 14, color: Colors.grey),
              SizedBox(width: 6),
              Text('Direct PTY', style: TextStyle(color: Colors.grey, fontSize: 12)),
            ]),
          ),
          for (final s in bareSessions)
            ListTile(
              contentPadding: const EdgeInsets.symmetric(horizontal: 32),
              leading: const Icon(Icons.terminal, size: 18),
              title: Text(
                s.name.isNotEmpty ? s.name : s.command,
                style: const TextStyle(fontFamily: 'monospace'),
                overflow: TextOverflow.ellipsis,
              ),
              onTap: () => onSessionTap(s.id, s.name.isNotEmpty ? s.name : s.command),
              onLongPress: () => _showPaneMenu(context, s.id, s.name.isNotEmpty ? s.name : s.command),
              trailing: IconButton(
                icon: const Icon(Icons.more_vert, size: 18),
                onPressed: () => _showPaneMenu(context, s.id, s.name.isNotEmpty ? s.name : s.command),
              ),
            ),
        ],
      ],
    );
  }

  void _showPaneMenu(BuildContext context, String sessionId, String label) {
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
                onRenameSession(sessionId, label);
              },
            ),
            ListTile(
              leading: const Icon(Icons.stop_circle_outlined, color: Colors.red),
              title: Text('Kill "$label"', style: const TextStyle(color: Colors.red)),
              onTap: () {
                Navigator.pop(ctx);
                onKillSession(sessionId);
              },
            ),
          ],
        ),
      ),
    );
  }
}

class _TmuxSessionGroup extends StatelessWidget {
  final TmuxSessionNode sessionNode;
  final void Function(String sessionId, String name) onPaneTap;
  final void Function(String sessionId, String name) onPaneLongPress;

  const _TmuxSessionGroup({
    required this.sessionNode,
    required this.onPaneTap,
    required this.onPaneLongPress,
  });

  @override
  Widget build(BuildContext context) {
    return ExpansionTile(
      tilePadding: const EdgeInsets.symmetric(horizontal: 16),
      leading: const Icon(Icons.workspaces_outline, size: 18),
      title: Text(
        sessionNode.name,
        style: const TextStyle(fontWeight: FontWeight.w500, fontSize: 13),
      ),
      initiallyExpanded: true,
      children: [
        for (final win in sessionNode.windows)
          _TmuxWindowGroup(
            window: win,
            onPaneTap: onPaneTap,
            onPaneLongPress: onPaneLongPress,
          ),
      ],
    );
  }
}

class _TmuxWindowGroup extends StatelessWidget {
  final TmuxWindowNode window;
  final void Function(String sessionId, String name) onPaneTap;
  final void Function(String sessionId, String name) onPaneLongPress;

  const _TmuxWindowGroup({
    required this.window,
    required this.onPaneTap,
    required this.onPaneLongPress,
  });

  @override
  Widget build(BuildContext context) {
    return ExpansionTile(
      tilePadding: const EdgeInsets.only(left: 28, right: 16),
      leading: Text(
        '${window.index}',
        style: const TextStyle(
          fontFamily: 'monospace',
          fontSize: 12,
          color: Colors.grey,
        ),
      ),
      title: Text(
        window.name,
        style: const TextStyle(fontSize: 13),
      ),
      initiallyExpanded: true,
      children: [
        for (final pane in window.panes)
          ListTile(
            contentPadding: const EdgeInsets.only(left: 44, right: 16),
            leading: Icon(
              pane.active ? Icons.chevron_right : Icons.remove,
              size: 16,
              color: pane.active ? Theme.of(context).colorScheme.primary : Colors.grey,
            ),
            title: Text(
              pane.title.isNotEmpty ? pane.title : 'pane ${pane.ccmuxId.substring(0, 6)}',
              style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
              overflow: TextOverflow.ellipsis,
            ),
            onTap: () => onPaneTap(pane.ccmuxId, pane.title),
            onLongPress: () => onPaneLongPress(pane.ccmuxId, pane.title),
          ),
      ],
    );
  }
}
