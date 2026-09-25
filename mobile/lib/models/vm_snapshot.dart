/// A VM snapshot, as the sidecar lists it (libvirt domain snapshots).
class VmSnapshot {
  const VmSnapshot({
    required this.name,
    this.description = '',
    this.state = '',
    this.creationTime,
    this.parent = '',
    this.current = false,
  });

  final String name;
  final String description;

  /// The VM's state when it was taken: "running" (memory included) or
  /// "shutoff" (disks only).
  final String state;

  /// Null when the server didn't say - shown as unknown, never as "now".
  final DateTime? creationTime;
  final String parent;

  /// The snapshot the VM is currently based on.
  final bool current;

  bool get includesMemory => state == 'running' || state == 'paused';

  factory VmSnapshot.fromJson(Map<String, dynamic> j) {
    final seconds = (j['creation_time'] as num?)?.toInt() ?? 0;
    return VmSnapshot(
      name: j['name'] as String? ?? '',
      description: j['description'] as String? ?? '',
      state: j['state'] as String? ?? '',
      creationTime: seconds > 0 ? DateTime.fromMillisecondsSinceEpoch(seconds * 1000) : null,
      parent: j['parent'] as String? ?? '',
      current: j['current'] as bool? ?? false,
    );
  }

  Map<String, dynamic> toJson() => {
        'name': name,
        'description': description,
        'state': state,
        if (creationTime != null) 'creation_time': creationTime!.millisecondsSinceEpoch ~/ 1000,
        'parent': parent,
        'current': current,
      };
}
