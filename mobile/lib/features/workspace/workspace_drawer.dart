import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../auth/auth_provider.dart';
import '../terminal/terminal_provider.dart';
import 'workspace_provider.dart';
import 'device_section.dart';

class WorkspaceDrawer extends ConsumerWidget {
  const WorkspaceDrawer({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final workspaceAsync = ref.watch(workspaceProvider);

    return Drawer(
      child: Column(
        children: [
          DrawerHeader(
            decoration: BoxDecoration(color: Theme.of(context).colorScheme.primaryContainer),
            child: Row(
              children: [
                const Expanded(
                  child: Text('ccmux',
                      style: TextStyle(fontSize: 22, fontWeight: FontWeight.bold)),
                ),
                IconButton(
                  icon: const Icon(Icons.refresh),
                  tooltip: 'Refresh',
                  onPressed: () => ref.read(workspaceProvider.notifier).refresh(),
                ),
                IconButton(
                  icon: const Icon(Icons.logout),
                  tooltip: 'Sign out',
                  onPressed: () => ref.read(authProvider.notifier).logout(),
                ),
              ],
            ),
          ),
          Expanded(
            child: workspaceAsync.when(
              loading: () => const Center(child: CircularProgressIndicator()),
              error:   (e, _) => Center(child: Text('Error: $e')),
              data:    (ws) => ws.devices.isEmpty
                  ? const Center(child: Text('No devices registered'))
                  : ListView.builder(
                      itemCount: ws.devices.length,
                      itemBuilder: (context, i) {
                        final device = ws.devices[i];
                        final sessions = ws.sessionsByDevice[device.id] ?? [];
                        return DeviceSection(
                          device: device,
                          sessions: sessions,
                          onSessionTap: (sessionId) {
                            ref.read(terminalProvider.notifier)
                                .openSession(sessionId);
                            Navigator.of(context).pop(); // close drawer
                          },
                          onRenameSession: (sessionId, currentName) =>
                              _showRenameDialog(context, ref, device.id, sessionId, currentName),
                          onKillSession: (sessionId) =>
                              _confirmKill(context, ref, device.id, sessionId),
                        );
                      },
                    ),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _showRenameDialog(
    BuildContext context,
    WidgetRef ref,
    String deviceId,
    String sessionId,
    String currentName,
  ) async {
    final controller = TextEditingController(text: currentName);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Rename session'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(labelText: 'Name'),
          onSubmitted: (_) => Navigator.pop(ctx, true),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Rename'),
          ),
        ],
      ),
    );
    if (confirmed == true && controller.text.trim().isNotEmpty) {
      await ref.read(workspaceProvider.notifier)
          .renameSession(deviceId, sessionId, controller.text.trim());
    }
    controller.dispose();
  }

  Future<void> _confirmKill(
    BuildContext context,
    WidgetRef ref,
    String deviceId,
    String sessionId,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Kill session?'),
        content: const Text('This will terminate the process immediately.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            style: TextButton.styleFrom(foregroundColor: Colors.red),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Kill'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await ref.read(workspaceProvider.notifier)
          .killSession(deviceId, sessionId);
    }
  }
}
