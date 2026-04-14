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
                        );
                      },
                    ),
            ),
          ),
        ],
      ),
    );
  }
}
