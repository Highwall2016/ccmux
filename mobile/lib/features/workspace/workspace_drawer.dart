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
                          onSessionTap: (sessionId, sessionName) {
                            ref.read(terminalProvider.notifier)
                                .openSession(sessionId, name: sessionName);
                            Navigator.of(context).pop(); // close drawer
                          },
                          onRenameSession: (sessionId, currentName) =>
                              _showRenameDialog(context, ref, device.id, sessionId, currentName),
                          onKillSession: (sessionId) =>
                              _confirmKill(context, ref, device.id, sessionId),
                          onRemoveDevice: () =>
                              _confirmRemoveDevice(context, ref, device.id, device.name),
                          onSpawnSession: () =>
                              _showSpawnDialog(context, ref, device.id, device.name),
                        );
                      },
                    ),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _showSpawnDialog(
    BuildContext context,
    WidgetRef ref,
    String deviceId,
    String deviceName,
  ) async {
    final commandCtrl = TextEditingController(text: 'bash');
    final nameCtrl = TextEditingController();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('New session on $deviceName'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: commandCtrl,
                autofocus: true,
                decoration: const InputDecoration(labelText: 'Command'),
                onSubmitted: (_) => Navigator.pop(ctx, true),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: nameCtrl,
                decoration: const InputDecoration(
                  labelText: 'Name (optional)',
                  hintText: 'auto',
                ),
                onSubmitted: (_) => Navigator.pop(ctx, true),
              ),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Create'),
          ),
        ],
      ),
    );
    if (confirmed != true) {
      commandCtrl.dispose();
      nameCtrl.dispose();
      return;
    }
    final command = commandCtrl.text.trim().isEmpty ? 'bash' : commandCtrl.text.trim();
    final name = nameCtrl.text.trim();
    commandCtrl.dispose();
    nameCtrl.dispose();

    try {
      final sessionId = await ref.read(workspaceProvider.notifier)
          .spawnSession(deviceId, name: name, command: command);
      // Open terminal immediately and close drawer.
      if (context.mounted) {
        ref.read(terminalProvider.notifier).openSession(sessionId, name: name.isNotEmpty ? name : command);
        Navigator.of(context).pop();
      }
    } catch (e) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to create session: $e')),
        );
      }
    }
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

  Future<void> _confirmRemoveDevice(
    BuildContext context,
    WidgetRef ref,
    String deviceId,
    String deviceName,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Remove device?'),
        content: Text(
          'Remove "$deviceName" from your account? '
          'The agent on that device will need to re-login.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          TextButton(
            style: TextButton.styleFrom(foregroundColor: Colors.red),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed == true) {
      await ref.read(workspaceProvider.notifier).removeDevice(deviceId);
    }
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
