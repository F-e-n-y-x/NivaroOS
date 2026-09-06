/// Model representing a VM Snapshot from libvirt Domain Snapshot API.
class VmSnapshot {
  final String name;
  final String description;
  final String state;
  final DateTime creationTime;
  final String parent;
  final bool current;

  VmSnapshot({
    required this.name,
    required this.description,
    required this.state,
    required this.creationTime,
    required this.parent,
    required this.current,
  });

  factory VmSnapshot.fromJson(Map<String, dynamic> j) {
    final timestamp = (j['creation_time'] as num?)?.toInt() ?? 0;
    return VmSnapshot(
      name: j['name'] as String? ?? '',
      description: j['description'] as String? ?? '',
      state: j['state'] as String? ?? 'shutoff',
      creationTime: timestamp > 0
          ? DateTime.fromMillisecondsSinceEpoch(timestamp * 1000)
          : DateTime.now(),
      parent: j['parent'] as String? ?? '',
      current: j['current'] as bool? ?? false,
    );
  }

  Map<String, dynamic> toJson() => {
        'name': name,
        'description': description,
        'state': state,
        'creation_time': creationTime.millisecondsSinceEpoch ~/ 1000,
        'parent': parent,
        'current': current,
      };
}
