import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';

/// Install any app from a hand-written or pasted docker-compose file with template quick-inserts.
class CustomInstallScreen extends StatefulWidget {
  const CustomInstallScreen({super.key});

  @override
  State<CustomInstallScreen> createState() => _CustomInstallScreenState();
}

class _CustomInstallScreenState extends State<CustomInstallScreen> {
  final _controller = TextEditingController();
  bool _installing = false;
  String? _error;

  final Map<String, String> _templates = {
    'Nginx': '''services:
  nginx:
    image: nginx:alpine
    container_name: nginx-web
    restart: unless-stopped
    ports:
      - "8080:80"
''',
    'Redis': '''services:
  redis:
    image: redis:alpine
    container_name: redis-cache
    restart: unless-stopped
    ports:
      - "6379:6379"
''',
    'PostgreSQL': '''services:
  postgres:
    image: postgres:16-alpine
    container_name: postgres-db
    restart: unless-stopped
    environment:
      POSTGRES_USER: nivaro
      POSTGRES_PASSWORD: secretpassword
      POSTGRES_DB: nivarodb
    ports:
      - "5432:5432"
    volumes:
      - /DATA/AppData/postgres:/var/lib/postgresql/data
''',
    'Portainer': '''services:
  portainer:
    image: portainer/portainer-ce:latest
    container_name: portainer
    restart: unless-stopped
    ports:
      - "9000:9000"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /DATA/AppData/portainer:/data
''',
  };

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _insertTemplate(String name) {
    HapticFeedback.selectionClick();
    setState(() {
      _controller.text = _templates[name] ?? '';
    });
  }

  Future<void> _install() async {
    final yaml = _controller.text.trim();
    if (yaml.isEmpty) return;
    HapticFeedback.mediumImpact();
    setState(() {
      _installing = true;
      _error = null;
    });
    try {
      await ApiClient.instance.postBody('/v2/app_management/compose', yaml, 'application/yaml');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Custom compose app deployed successfully.'),
            backgroundColor: NivaroColors.success,
            behavior: SnackBarBehavior.floating,
          ),
        );
        Navigator.of(context).pop(true);
      }
    } catch (e) {
      setState(() => _error = e.toString().replaceFirst('Exception: ', ''));
    } finally {
      if (mounted) setState(() => _installing = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  IconButton(
                    icon: const Icon(Icons.arrow_back_rounded),
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'Custom Compose',
                      style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                            fontWeight: FontWeight.w800,
                            letterSpacing: -0.5,
                          ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              const Text(
                'Paste a docker-compose.yml file below to install custom multi-container stacks.',
                style: TextStyle(color: NivaroColors.textMuted, fontSize: 13),
              ),
              const SizedBox(height: 12),

              // Templates quick insert
              SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: Row(
                  children: [
                    const Text('Templates:', style: TextStyle(color: NivaroColors.textFaint, fontSize: 12, fontWeight: FontWeight.w600)),
                    const SizedBox(width: 8),
                    ..._templates.keys.map((name) => Padding(
                          padding: const EdgeInsets.only(right: 6),
                          child: ActionChip(
                            label: Text(name),
                            labelStyle: const TextStyle(fontSize: 11, fontWeight: FontWeight.w600),
                            backgroundColor: NivaroColors.surfaceRaised,
                            shape: RoundedRectangleBorder(
                              borderRadius: BorderRadius.circular(NivaroShape.full),
                              side: const BorderSide(color: NivaroColors.borderSubtle),
                            ),
                            onPressed: () => _insertTemplate(name),
                          ),
                        )),
                  ],
                ),
              ),
              const SizedBox(height: 12),

              // YAML editor container
              Expanded(
                child: Container(
                  decoration: BoxDecoration(
                    color: NivaroColors.surfaceDim,
                    borderRadius: BorderRadius.circular(NivaroShape.large),
                    border: Border.all(color: NivaroColors.borderSubtle),
                  ),
                  padding: const EdgeInsets.all(12),
                  child: TextField(
                    controller: _controller,
                    maxLines: null,
                    expands: true,
                    textAlignVertical: TextAlignVertical.top,
                    style: const TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 13,
                      height: 1.4,
                      color: Color(0xFFE2E8F0),
                    ),
                    decoration: const InputDecoration(
                      hintText: 'services:\n  myapp:\n    image: nginx:alpine\n    ports:\n      - "8080:80"',
                      hintStyle: TextStyle(color: NivaroColors.textFaint, fontFamily: 'monospace'),
                      border: InputBorder.none,
                      filled: false,
                    ),
                  ),
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 12),
                DarkCard(
                  color: NivaroColors.danger.withValues(alpha: 0.15),
                  child: Row(
                    children: [
                      const Icon(Icons.error_outline_rounded, color: NivaroColors.danger),
                      const SizedBox(width: 12),
                      Expanded(child: Text(_error!, style: const TextStyle(color: NivaroColors.danger, fontSize: 13))),
                    ],
                  ),
                ),
              ],
              const SizedBox(height: 16),
              SizedBox(
                width: double.infinity,
                height: 50,
                child: FilledButton(
                  onPressed: _installing ? null : _install,
                  style: FilledButton.styleFrom(
                    backgroundColor: NivaroColors.primary,
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                  ),
                  child: _installing
                      ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                      : const Text('Deploy Stack', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 15)),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

