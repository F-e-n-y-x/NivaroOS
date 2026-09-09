import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'dart:convert';
import '../theme.dart';
import '../models/server_profile.dart';
import '../services/storage_service.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';
import 'home_shell.dart';
import 'login_screen.dart';

class ServerProfilesScreen extends StatefulWidget {
  const ServerProfilesScreen({super.key});

  @override
  State<ServerProfilesScreen> createState() => _ServerProfilesScreenState();
}

class _ServerProfilesScreenState extends State<ServerProfilesScreen> {
  List<ServerProfile> _profiles = [];
  String? _activeUrl;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final profiles = await StorageService.instance.getProfiles();
      final activeUrl = await StorageService.instance.getServerUrl();
      if (mounted) {
        setState(() {
          _profiles = profiles;
          _activeUrl = activeUrl;
          _loading = false;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _loading = false;
        });
      }
    }
  }

  Future<void> _switchToProfile(ServerProfile profile) async {
    HapticFeedback.mediumImpact();

    await StorageService.instance.switchProfile(profile);
    ApiClient.instance.setBaseUrl(profile.url);
    if (profile.accessToken != null && profile.refreshToken != null && profile.accessToken!.isNotEmpty) {
      ApiClient.instance.setSession(profile.accessToken!, profile.refreshToken!);
    } else {
      ApiClient.instance.clearSession();
    }

    if (!ApiClient.instance.hasSession) {
      if (mounted) {
        Navigator.of(context).push(
          MaterialPageRoute(builder: (_) => LoginScreen(initialUsername: profile.username)),
        );
      }
      return;
    }

    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('Switched active server to "${profile.name}"'),
          backgroundColor: NivaroColors.success,
        ),
      );
      Navigator.of(context).pushAndRemoveUntil(
        MaterialPageRoute(builder: (_) => const HomeShell()),
        (route) => false,
      );
    }
  }

  Future<void> _reauthProfile(ServerProfile profile) async {
    HapticFeedback.lightImpact();
    await StorageService.instance.setServerUrl(profile.url);
    ApiClient.instance.setBaseUrl(profile.url);
    if (!mounted) return;
    final success = await Navigator.of(context).push<bool>(
      MaterialPageRoute(
        builder: (_) => LoginScreen(
          isReauth: true,
          initialUsername: profile.username,
        ),
      ),
    );
    if (success == true && mounted) {
      _load();
    }
  }

  Future<void> _addOrEditProfile([ServerProfile? existing]) async {
    HapticFeedback.lightImpact();
    final nameCtrl = TextEditingController(text: existing?.name ?? '');
    final urlCtrl = TextEditingController(text: existing?.url ?? '');
    final userCtrl = TextEditingController(text: existing?.username ?? 'admin');
    final passCtrl = TextEditingController();
    bool isSaving = false;
    String? errorText;

    await showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (ctx) => StatefulBuilder(
        builder: (context, setSheetState) => Padding(
          padding: EdgeInsets.only(bottom: MediaQuery.of(context).viewInsets.bottom),
          child: Container(
            padding: const EdgeInsets.all(22),
            decoration: const BoxDecoration(
              color: NivaroColors.surfaceContainerLowest,
              borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Container(
                      width: 42,
                      height: 42,
                      decoration: BoxDecoration(
                        color: NivaroColors.primary.withOpacity(0.12),
                        borderRadius: BorderRadius.circular(12),
                      ),
                      alignment: Alignment.center,
                      child: const Icon(Icons.add_to_photos_rounded, color: NivaroColors.primaryLight, size: 22),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            existing == null ? 'Add Server Profile' : 'Edit Server Profile',
                            style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16.5, color: NivaroColors.textPrimary),
                          ),
                          const Text('Configure host address and login credentials', style: TextStyle(color: NivaroColors.textMuted, fontSize: 11.5)),
                        ],
                      ),
                    ),
                    RoundIconButton(icon: Icons.close_rounded, size: 34, onPressed: () => Navigator.pop(ctx)),
                  ],
                ),
                const SizedBox(height: 18),

                if (errorText != null) ...[
                  Container(
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: NivaroColors.danger.withOpacity(0.12),
                      borderRadius: BorderRadius.circular(8),
                      border: Border.all(color: NivaroColors.danger.withOpacity(0.3)),
                    ),
                    child: Row(
                      children: [
                        const Icon(Icons.error_outline_rounded, color: NivaroColors.dangerLight, size: 16),
                        const SizedBox(width: 8),
                        Expanded(child: Text(errorText!, style: const TextStyle(color: NivaroColors.dangerLight, fontSize: 12))),
                      ],
                    ),
                  ),
                  const SizedBox(height: 12),
                ],

                TextField(
                  controller: nameCtrl,
                  decoration: const InputDecoration(
                    labelText: 'Server Nickname',
                    hintText: 'e.g. Home Server, Office Lab',
                    filled: true,
                    fillColor: NivaroColors.surfaceRaised,
                  ),
                ),
                const SizedBox(height: 10),

                TextField(
                  controller: urlCtrl,
                  decoration: const InputDecoration(
                    labelText: 'Server URL or IP',
                    hintText: 'http://192.168.1.100:8080 or https://...',
                    filled: true,
                    fillColor: NivaroColors.surfaceRaised,
                  ),
                ),
                const SizedBox(height: 10),

                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: userCtrl,
                        decoration: const InputDecoration(
                          labelText: 'Username',
                          hintText: 'admin',
                          filled: true,
                          fillColor: NivaroColors.surfaceRaised,
                        ),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: TextField(
                        controller: passCtrl,
                        obscureText: true,
                        decoration: const InputDecoration(
                          labelText: 'Password',
                          hintText: '••••••••',
                          filled: true,
                          fillColor: NivaroColors.surfaceRaised,
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 20),

                SizedBox(
                  width: double.infinity,
                  height: 48,
                  child: ElevatedButton(
                    style: ElevatedButton.styleFrom(
                      backgroundColor: NivaroColors.primary,
                      foregroundColor: Colors.white,
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                    ),
                    onPressed: isSaving
                        ? null
                        : () async {
                            final rawUrl = urlCtrl.text.trim();
                            final name = nameCtrl.text.trim().isNotEmpty ? nameCtrl.text.trim() : 'Nivaro Server';
                            final username = userCtrl.text.trim();
                            final password = passCtrl.text;

                            if (rawUrl.isEmpty) {
                              setSheetState(() => errorText = 'Please provide a valid server URL or IP address.');
                              return;
                            }

                            String cleanUrl = rawUrl;
                            if (!cleanUrl.startsWith('http://') && !cleanUrl.startsWith('https://')) {
                              cleanUrl = 'http://$cleanUrl';
                            }
                            cleanUrl = cleanUrl.replaceAll(RegExp(r'/+$'), '');

                            setSheetState(() {
                              isSaving = true;
                              errorText = null;
                            });

                            String? accessToken = existing?.accessToken;
                            String? refreshToken = existing?.refreshToken;

                            // Attempt authentication if password provided
                            if (password.isNotEmpty) {
                              try {
                                final loginUri = Uri.parse('$cleanUrl/v1/users/login');
                                final res = await http.post(
                                  loginUri,
                                  headers: {'Content-Type': 'application/json'},
                                  body: jsonEncode({'username': username, 'password': password}),
                                ).timeout(const Duration(seconds: 4));

                                if (res.statusCode == 200) {
                                  final body = jsonDecode(res.body) as Map<String, dynamic>;
                                  final data = body['data'] as Map<String, dynamic>? ?? body;
                                  final token = data['token'] as Map<String, dynamic>? ?? data;
                                  accessToken = token['access_token']?.toString();
                                  refreshToken = token['refresh_token']?.toString();
                                } else {
                                  setSheetState(() {
                                    isSaving = false;
                                    errorText = 'Authentication failed (HTTP ${res.statusCode}). Check username/password.';
                                  });
                                  return;
                                }
                              } catch (e) {
                                setSheetState(() {
                                  isSaving = false;
                                  errorText = 'Could not reach server: $e';
                                });
                                return;
                              }
                            }

                            final profile = ServerProfile(
                              id: existing?.id ?? 'srv_${DateTime.now().millisecondsSinceEpoch}',
                              name: name,
                              url: cleanUrl,
                              username: username,
                              accessToken: accessToken,
                              refreshToken: refreshToken,
                              lastConnected: DateTime.now(),
                            );

                            await StorageService.instance.saveProfile(profile);
                            if (ctx.mounted) {
                              Navigator.pop(ctx);
                            }
                            if (mounted) {
                              _load();
                            }
                          },
                    child: isSaving
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                        : Text(existing == null ? 'Save Server Profile' : 'Update Profile', style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14.5)),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _deleteProfile(ServerProfile profile) async {
    if (_profiles.length <= 1) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Cannot delete the only configured server profile.')),
      );
      return;
    }

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: Text('Delete "${profile.name}"?'),
        content: Text('Remove saved connection to ${profile.url}?'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );

    if (confirmed == true) {
      await StorageService.instance.deleteProfile(profile.id);
      _load();
    }
  }

  Widget _buildProfileCard(ServerProfile profile) {
    final isActive = profile.url == _activeUrl;
    return InkWell(
      borderRadius: BorderRadius.circular(16),
      onTap: () => _switchToProfile(profile),
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: isActive ? NivaroColors.primary.withOpacity(0.08) : NivaroColors.surfaceRaised,
          borderRadius: BorderRadius.circular(16),
          border: Border.all(
            color: isActive ? NivaroColors.primaryLight : NivaroColors.borderSubtle,
            width: isActive ? 1.5 : 1,
          ),
        ),
        child: Row(
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: isActive ? NivaroColors.primary.withOpacity(0.2) : NivaroColors.surfaceContainerHighest,
                shape: BoxShape.circle,
              ),
              child: Icon(
                Icons.dns_rounded,
                color: isActive ? NivaroColors.primaryLight : NivaroColors.textMuted,
              ),
            ),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Row(
                    children: [
                      Flexible(
                        child: Text(
                          profile.name,
                          style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 15, color: NivaroColors.textPrimary),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                      if (isActive) ...[
                        const SizedBox(width: 8),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                          decoration: BoxDecoration(
                            color: NivaroColors.success.withOpacity(0.2),
                            borderRadius: BorderRadius.circular(8),
                          ),
                          child: const Text(
                            'ACTIVE',
                            style: TextStyle(color: NivaroColors.successLight, fontSize: 9.5, fontWeight: FontWeight.w800),
                          ),
                        ),
                      ],
                    ],
                  ),
                  const SizedBox(height: 3),
                  Text(
                    profile.url,
                    style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                  const SizedBox(height: 2),
                  Text(
                    'User: ${profile.username}',
                    style: const TextStyle(color: NivaroColors.textFaint, fontSize: 11),
                  ),
                ],
              ),
            ),
              PopupMenuButton<String>(
                icon: const Icon(Icons.more_vert_rounded, size: 20, color: NivaroColors.textSecondary),
                onSelected: (val) {
                  if (val == 'switch') {
                    _switchToProfile(profile);
                  } else if (val == 'relogin') {
                    _reauthProfile(profile);
                  } else if (val == 'edit') {
                    _addOrEditProfile(profile);
                  } else if (val == 'delete') {
                    _deleteProfile(profile);
                  }
                },
                itemBuilder: (context) => [
                  if (!isActive)
                    const PopupMenuItem(
                      value: 'switch',
                      child: Row(
                        children: [
                          Icon(Icons.swap_horiz_rounded, color: NivaroColors.successLight, size: 18),
                          SizedBox(width: 8),
                          Text('Switch to Server'),
                        ],
                      ),
                    ),
                  const PopupMenuItem(
                    value: 'relogin',
                    child: Row(
                      children: [
                        Icon(Icons.lock_open_rounded, color: NivaroColors.primaryLight, size: 18),
                        SizedBox(width: 8),
                        Text('Sign In / Re-authenticate'),
                      ],
                    ),
                  ),
                  const PopupMenuItem(
                    value: 'edit',
                    child: Row(
                      children: [
                        Icon(Icons.edit_rounded, color: NivaroColors.primaryLight, size: 18),
                        SizedBox(width: 8),
                        Text('Edit Profile'),
                      ],
                    ),
                  ),
                  if (_profiles.length > 1)
                    const PopupMenuItem(
                      value: 'delete',
                      child: Row(
                        children: [
                          Icon(Icons.delete_outline_rounded, color: NivaroColors.dangerLight, size: 18),
                          SizedBox(width: 8),
                          Text('Delete Profile'),
                        ],
                      ),
                    ),
                ],
              ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('Server Profiles', style: TextStyle(fontWeight: FontWeight.w800)),
        actions: [
          IconButton(
            icon: const Icon(Icons.add_rounded, size: 24, color: NivaroColors.primaryLight),
            tooltip: 'Add Server',
            onPressed: () => _addOrEditProfile(),
          ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator(color: NivaroColors.primaryLight))
          : Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 1050),
                child: ListView(
                  padding: const EdgeInsets.all(20),
                  children: [
                    const SectionHeader(
                      title: 'Saved NivaroOS Servers',
                      subtitle: 'Tap any server profile to switch active connection',
                    ),
                    Builder(builder: (context) {
                      final width = MediaQuery.of(context).size.width;
                      final cols = width >= 750 ? 2 : 1;

                      if (cols > 1) {
                        return GridView.builder(
                          shrinkWrap: true,
                          physics: const NeverScrollableScrollPhysics(),
                          gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                            crossAxisCount: 2,
                            crossAxisSpacing: 12,
                            mainAxisSpacing: 12,
                            childAspectRatio: 3.5,
                          ),
                          itemCount: _profiles.length,
                          itemBuilder: (context, index) => _buildProfileCard(_profiles[index]),
                        );
                      }

                      return Column(
                        children: _profiles.map((profile) => Padding(
                              padding: const EdgeInsets.only(bottom: 12),
                              child: _buildProfileCard(profile),
                            )).toList(),
                      );
                    }),
                    const SizedBox(height: 16),
                    OutlinedButton.icon(
                      icon: const Icon(Icons.add_rounded, size: 20),
                      label: const Text('Add Another Server Profile'),
                      style: OutlinedButton.styleFrom(
                        padding: const EdgeInsets.symmetric(vertical: 14),
                        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                      ),
                      onPressed: () => _addOrEditProfile(),
                    ),
                  ],
                ),
              ),
            ),
    );
  }
}
