import 'package:flutter/material.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';

class CustomInstallScreen extends StatefulWidget {
  const CustomInstallScreen({super.key});

  @override
  State<CustomInstallScreen> createState() => _CustomInstallScreenState();
}

class _CustomInstallScreenState extends State<CustomInstallScreen> {
  final _yamlCtrl = TextEditingController();
  bool _installing = false;

  final _templates = [
    (
      name: 'Nginx Web Server',
      yaml: "version: '3.8'\nservices:\n  web:\n    image: nginx:alpine\n    container_name: custom-nginx\n    restart: unless-stopped\n    ports:\n      - \"8080:80\""
    ),
    (
      name: 'Redis In-Memory DB',
      yaml: "version: '3.8'\nservices:\n  redis:\n    image: redis:alpine\n    container_name: custom-redis\n    restart: unless-stopped\n    ports:\n      - \"6379:6379\""
    ),
    (
      name: 'PostgreSQL 16',
      yaml: "version: '3.8'\nservices:\n  db:\n    image: postgres:16-alpine\n    container_name: custom-postgres\n    restart: unless-stopped\n    environment:\n      POSTGRES_USER: postgres\n      POSTGRES_PASSWORD: secretpassword\n      POSTGRES_DB: appdb\n    ports:\n      - \"5432:5432\"\n    volumes:\n      - /DATA/AppData/postgres:/var/lib/postgresql/data"
    ),
    (
      name: 'Ollama AI (Local LLM)',
      yaml: "version: '3.8'\nservices:\n  ollama:\n    image: ollama/ollama:latest\n    container_name: custom-ollama\n    restart: unless-stopped\n    ports:\n      - \"11434:11434\"\n    volumes:\n      - /DATA/AppData/ollama:/root/.ollama"
    ),
  ];

  @override
  void initState() {
    super.initState();
    _yamlCtrl.text = _templates.first.yaml;
  }

  @override
  void dispose() {
    _yamlCtrl.dispose();
    super.dispose();
  }

  Future<void> _deploy() async {
    final yaml = _yamlCtrl.text.trim();
    if (yaml.isEmpty) return;
    setState(() => _installing = true);
    try {
      await ApiClient.instance.postBody('/v2/app_management/compose', yaml, 'application/yaml');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Container application deployed successfully!')));
        Navigator.pop(context, true);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(e.toString().replaceFirst('Exception: ', '')), backgroundColor: NivaroColors.dangerLight),
        );
      }
    } finally {
      if (mounted) setState(() => _installing = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('Custom Compose / Docker', style: TextStyle(fontWeight: FontWeight.w800)),
      ),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          const SectionHeader(title: 'Quick Templates'),
          SizedBox(
            height: 40,
            child: ListView.builder(
              scrollDirection: Axis.horizontal,
              itemCount: _templates.length,
              itemBuilder: (context, i) {
                final t = _templates[i];
                return Padding(
                  padding: const EdgeInsets.only(right: 8),
                  child: ActionChip(
                    label: Text(t.name, style: const TextStyle(fontSize: 12)),
                    onPressed: () {
                      setState(() => _yamlCtrl.text = t.yaml);
                    },
                  ),
                );
              },
            ),
          ),
          const SizedBox(height: 20),

          const SectionHeader(title: 'Docker Compose YAML'),
          DarkCard(
            padding: const EdgeInsets.all(12),
            child: TextField(
              controller: _yamlCtrl,
              maxLines: 14,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 13, height: 1.4),
              decoration: const InputDecoration(
                border: InputBorder.none,
                enabledBorder: InputBorder.none,
                focusedBorder: InputBorder.none,
                filled: false,
                contentPadding: EdgeInsets.zero,
                hintText: 'Paste docker-compose.yml text here...',
              ),
            ),
          ),
          const SizedBox(height: 24),

          FilledButton.icon(
            onPressed: _installing ? null : _deploy,
            icon: _installing
                ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                : const Icon(Icons.rocket_launch_rounded),
            label: const Text('Deploy Application', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 16)),
            style: FilledButton.styleFrom(
              backgroundColor: NivaroColors.primary,
              padding: const EdgeInsets.symmetric(vertical: 16),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
            ),
          ),
        ],
      ),
    );
  }
}
