import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';

import '../services/api_client.dart';
import '../services/avatar_service.dart';
import '../ui/ui.dart';

/// Size of the square the app uploads (the server keeps 512 px).
const avatarOutput = 512;

/// Crop geometry, the same as the web's (ui/src/utils/avatar.js): a square
/// view of [view] px over a [w] x [h] picture at [zoom] (1 = it just covers
/// the view); [offset] is the picture centre's shift from the view centre.
@visibleForTesting
double coverScale(double w, double h, double view, double zoom) => math.max(view / w, view / h) * zoom;

@visibleForTesting
Offset clampOffset(Offset o, double w, double h, double view, double zoom) {
  final s = coverScale(w, h, view, zoom);
  final mx = math.max(0.0, (w * s - view) / 2), my = math.max(0.0, (h * s - view) / 2);
  return Offset(o.dx.clamp(-mx, mx), o.dy.clamp(-my, my));
}

/// The source square (picture px) the view shows.
@visibleForTesting
Rect cropRect(Offset o, double w, double h, double view, double zoom) {
  final s = coverScale(w, h, view, zoom);
  final side = view / s;
  return Rect.fromLTWH(w / 2 - o.dx / s - side / 2, h / 2 - o.dy / s - side / 2, side, side);
}

/// Change or remove the signed-in account's picture: a sheet with
/// gallery / camera / remove, then [AvatarCropScreen], then the upload.
Future<void> changeProfilePicture(BuildContext context, {ImagePicker? picker}) async {
  final hasPicture = AvatarService.instance.current.value != null;
  final choice = await showModalBottomSheet<String>(
    context: context,
    showDragHandle: true,
    useSafeArea: true,
    builder: (context) => SafeArea(
      child: Column(mainAxisSize: MainAxisSize.min, children: [
        ListTile(
          leading: const Icon(Icons.photo_library_outlined),
          title: const Text('Choose from gallery'),
          onTap: () => Navigator.pop(context, 'gallery'),
        ),
        ListTile(
          leading: const Icon(Icons.photo_camera_outlined),
          title: const Text('Take a photo'),
          onTap: () => Navigator.pop(context, 'camera'),
        ),
        if (hasPicture)
          ListTile(
            leading: const Icon(Icons.delete_outline),
            title: const Text('Remove picture'),
            onTap: () => Navigator.pop(context, 'remove'),
          ),
      ]),
    ),
  );
  if (choice == null || !context.mounted) return;
  final messenger = ScaffoldMessenger.of(context);
  try {
    if (choice == 'remove') {
      await AvatarService.instance.remove();
      messenger.showSnackBar(const SnackBar(content: Text('Profile picture removed')));
      return;
    }
    // Big enough to crop well, small enough to decode on any phone.
    final file = await (picker ?? ImagePicker()).pickImage(
      source: choice == 'camera' ? ImageSource.camera : ImageSource.gallery,
      maxWidth: 2048,
      maxHeight: 2048,
      requestFullMetadata: false,
    );
    if (file == null || !context.mounted) return;
    final bytes = await file.readAsBytes();
    if (!context.mounted) return;
    final png = await Navigator.of(context).push<Uint8List>(MaterialPageRoute(builder: (_) => AvatarCropScreen(bytes: bytes)));
    if (png == null) return;
    await AvatarService.instance.upload(png);
    messenger.showSnackBar(const SnackBar(content: Text('Profile picture updated')));
  } on ApiException catch (e) {
    messenger.showSnackBar(SnackBar(content: Text(e.message)));
  } catch (e) {
    messenger.showSnackBar(const SnackBar(content: Text("Couldn't use that picture")));
  }
}

/// Square crop inside a circle: drag to move, pinch or the slider to zoom.
/// Pops with the cropped [avatarOutput] px PNG.
class AvatarCropScreen extends StatefulWidget {
  const AvatarCropScreen({super.key, required this.bytes});

  final Uint8List bytes;

  @override
  State<AvatarCropScreen> createState() => _AvatarCropScreenState();
}

class _AvatarCropScreenState extends State<AvatarCropScreen> {
  ui.Image? _image;
  bool _failed = false;
  bool _saving = false;
  double _zoom = 1;
  Offset _offset = Offset.zero;
  double _startZoom = 1;
  double _view = 300;

  @override
  void initState() {
    super.initState();
    decodeImageFromList(widget.bytes).then((img) {
      if (mounted) setState(() => _image = img);
    }, onError: (_) {
      if (mounted) setState(() => _failed = true);
    });
  }

  @override
  void dispose() {
    _image?.dispose();
    super.dispose();
  }

  void _set({double? zoom, Offset? offset}) {
    final img = _image!;
    final z = (zoom ?? _zoom).clamp(1.0, 4.0);
    setState(() {
      _zoom = z;
      _offset = clampOffset(offset ?? _offset, img.width.toDouble(), img.height.toDouble(), _view, z);
    });
  }

  Future<void> _done() async {
    final img = _image!;
    setState(() => _saving = true);
    final src = cropRect(_offset, img.width.toDouble(), img.height.toDouble(), _view, _zoom);
    final recorder = ui.PictureRecorder();
    Canvas(recorder).drawImageRect(img, src, const Rect.fromLTWH(0, 0, 512, 512), Paint()..filterQuality = FilterQuality.high);
    final out = await recorder.endRecording().toImage(avatarOutput, avatarOutput);
    final data = await out.toByteData(format: ui.ImageByteFormat.png);
    out.dispose();
    if (mounted) Navigator.of(context).pop(data?.buffer.asUint8List());
  }

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final img = _image;
    return AppScaffold(
      title: 'Move and zoom',
      actions: [
        TextButton(onPressed: img == null || _saving ? null : _done, child: const Text('Use')),
      ],
      body: _failed
          ? const EmptyState(icon: Icons.broken_image_outlined, title: "Couldn't open that picture", message: 'Pick another one: a photo or a PNG, JPEG or WebP picture.')
          : img == null
              ? const Center(child: CircularProgressIndicator())
              : LayoutBuilder(builder: (context, box) {
                  _view = math.min(box.maxWidth - 2 * Space.gutter(context), 360.0);
                  final s = coverScale(img.width.toDouble(), img.height.toDouble(), _view, _zoom);
                  return ListView(padding: const EdgeInsets.symmetric(vertical: Space.lg), children: [
                    Center(
                      child: Semantics(
                        label: 'Picture crop. Drag to move, pinch to zoom.',
                        child: GestureDetector(
                          onScaleStart: (_) => _startZoom = _zoom,
                          onScaleUpdate: (d) => _set(zoom: _startZoom * d.scale, offset: _offset + d.focalPointDelta),
                          child: ClipRRect(
                            borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.lg),
                            child: SizedBox.square(
                              dimension: _view,
                              child: Stack(fit: StackFit.expand, children: [
                                const ColoredBox(color: Colors.black),
                                OverflowBox(
                                  maxWidth: double.infinity,
                                  maxHeight: double.infinity,
                                  child: Transform.translate(
                                    offset: _offset,
                                    child: RawImage(image: img, width: img.width * s, height: img.height * s, fit: BoxFit.fill),
                                  ),
                                ),
                                const CustomPaint(painter: _CircleMask()),
                              ]),
                            ),
                          ),
                        ),
                      ),
                    ),
                    const SizedBox(height: Space.lg),
                    Padding(
                      padding: EdgeInsets.symmetric(horizontal: Space.gutter(context)),
                      child: Row(children: [
                        Icon(Icons.zoom_out_outlined, color: scheme.onSurfaceVariant),
                        Expanded(
                          child: Slider(
                            value: _zoom,
                            min: 1,
                            max: 4,
                            label: 'Zoom',
                            semanticFormatterCallback: (v) => 'Zoom ${v.toStringAsFixed(1)}x',
                            onChanged: (v) => _set(zoom: v),
                          ),
                        ),
                        Icon(Icons.zoom_in_outlined, color: scheme.onSurfaceVariant),
                      ]),
                    ),
                  ]);
                }),
    );
  }
}

class _CircleMask extends CustomPainter {
  const _CircleMask();

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    final circle = Path()..addOval(rect.deflate(1));
    canvas.drawPath(Path.combine(PathOperation.difference, Path()..addRect(rect), circle), Paint()..color = const Color(0x99000000));
    canvas.drawPath(circle, Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2
      ..color = const Color(0xE6FFFFFF));
  }

  @override
  bool shouldRepaint(_CircleMask oldDelegate) => false;
}
