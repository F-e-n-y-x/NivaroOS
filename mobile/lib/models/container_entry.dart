class ContainerEntry {
  final String id;
  final String name;
  final String image;
  final String state;
  final String status;
  final bool hasUpdate;

  ContainerEntry({
    required this.id,
    required this.name,
    required this.image,
    required this.state,
    required this.status,
    required this.hasUpdate,
  });

  factory ContainerEntry.fromJson(Map<String, dynamic> j) => ContainerEntry(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        image: j['image'] as String? ?? '',
        state: j['state'] as String? ?? 'unknown',
        status: j['status'] as String? ?? '',
        hasUpdate: j['has_update'] as bool? ?? false,
      );

  bool get isRunning => state == 'running';
}
