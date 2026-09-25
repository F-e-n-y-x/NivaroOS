// Tabs in Files, like a browser's: each keeps its own place, its way back
// and its selection, so something can be copied in one and pasted in
// another. The clipboard itself belongs to the Files screen, not a tab.
import 'dart:convert';

import 'package:flutter/foundation.dart';

/// Where a tab is: the locations page, a folder on the server or on this
/// phone, or the server's Trash.
@immutable
class FilesPlace {
  const FilesPlace.home()
      : path = null,
        isLocal = false,
        isTrash = false;
  const FilesPlace.folder(String this.path, {this.isLocal = false}) : isTrash = false;
  const FilesPlace.trash()
      : path = null,
        isLocal = false,
        isTrash = true;

  /// The folder; null for the locations page and the Trash.
  final String? path;
  final bool isLocal;
  final bool isTrash;

  bool get isHome => path == null && !isTrash;
  bool get isFolder => path != null;

  Map<String, Object?> toJson() => isTrash
      ? {'trash': true}
      : {
          if (path != null) 'path': path,
          if (isLocal) 'local': true,
        };

  /// Unknown or broken entries are the locations page.
  factory FilesPlace.fromJson(Object? json) {
    if (json is! Map) return const FilesPlace.home();
    if (json['trash'] == true) return const FilesPlace.trash();
    final path = json['path'];
    if (path is String && path.isNotEmpty) return FilesPlace.folder(path, isLocal: json['local'] == true);
    return const FilesPlace.home();
  }

  @override
  bool operator ==(Object other) => other is FilesPlace && other.path == path && other.isLocal == isLocal && other.isTrash == isTrash;

  @override
  int get hashCode => Object.hash(path, isLocal, isTrash);

  @override
  String toString() => isTrash ? 'Trash' : (path == null ? 'Home' : '${isLocal ? 'phone' : 'server'}:$path');
}

/// One tab: where it is, where it has been, and what is selected there.
class FilesTab {
  FilesTab(this.id, [this.place = const FilesPlace.home()]);

  /// Stable for the tab's life.
  final int id;
  FilesPlace place;

  /// How far down [place] was scrolled when the tab was last on screen.
  double offset = 0;

  /// Places to go back to, oldest first, each with how far down it was.
  final history = <({FilesPlace place, double offset})>[];

  /// Paths selected in [place]; cleared when the tab goes elsewhere.
  final selected = <String>{};

  /// How far back goes; older steps are dropped.
  static const maxHistory = 50;

  /// Goes to [to] from the top, remembering where it was (scrolled to
  /// [offset]) when [record].
  void go(FilesPlace to, {bool record = true, double offset = 0}) {
    selected.clear();
    if (to == place) return;
    if (record) {
      history.add((place: place, offset: offset));
      if (history.length > maxHistory) history.removeAt(0);
    }
    place = to;
    this.offset = 0;
  }

  /// Steps back to the previous place, where it was scrolled to; false
  /// when there is none.
  bool back() {
    if (history.isEmpty) return false;
    final last = history.removeLast();
    place = last.place;
    offset = last.offset;
    selected.clear();
    return true;
  }
}

/// The open tabs and which one is on screen. Never empty.
class FilesTabs {
  FilesTabs([FilesPlace first = const FilesPlace.home()]) : _tabs = [FilesTab(0, first)];

  final List<FilesTab> _tabs;
  int _active = 0;
  int _nextId = 1;

  List<FilesTab> get all => List.unmodifiable(_tabs);
  int get length => _tabs.length;
  int get activeIndex => _active;
  FilesTab get active => _tabs[_active];

  /// Opens a tab at [place] just after the one on screen, and shows it.
  FilesTab open(FilesPlace place) {
    final tab = FilesTab(_nextId++, place);
    _tabs.insert(_active + 1, tab);
    _active++;
    return tab;
  }

  void activate(FilesTab tab) {
    final i = _tabs.indexOf(tab);
    if (i >= 0) _active = i;
  }

  /// Closes [tab]; the last one can't be closed. Closing the tab on
  /// screen shows the one before it (or after it, for the first).
  bool close(FilesTab tab) {
    if (_tabs.length == 1) return false;
    final i = _tabs.indexOf(tab);
    if (i < 0) return false;
    _tabs.removeAt(i);
    if (i < _active || (i == _active && _active > 0)) _active--;
    return true;
  }

  /// What is saved between launches: each tab's place and the active one
  /// (not the history or selection).
  String encode() => jsonEncode({
        'active': _active,
        'tabs': [for (final t in _tabs) t.place.toJson()],
      });

  /// The saved tabs, or null when there are none (or they can't be read).
  static FilesTabs? decode(String? saved) {
    if (saved == null || saved.isEmpty) return null;
    Object? json;
    try {
      json = jsonDecode(saved);
    } on FormatException {
      return null;
    }
    if (json is! Map || json['tabs'] is! List) return null;
    final places = [for (final t in json['tabs'] as List) FilesPlace.fromJson(t)];
    if (places.isEmpty) return null;
    final tabs = FilesTabs(places.first);
    for (final p in places.skip(1)) {
      tabs.open(p);
    }
    final active = json['active'];
    tabs._active = active is int && active >= 0 && active < places.length ? active : 0;
    return tabs;
  }
}
