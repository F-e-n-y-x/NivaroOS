import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/file_entry.dart';
import 'package:nivaroos_mobile/screens/files/file_ops.dart';

FileEntry f(String name, {bool dir = false, int size = 0, DateTime? mod}) =>
    FileEntry(name: name, path: '/DATA/$name', isDir: dir, size: size, modified: mod);

void main() {
  group('paths', () {
    test('baseName and parentOf', () {
      expect(baseName('/DATA/a/b.txt'), 'b.txt');
      expect(baseName('/DATA/a/'), 'a');
      expect(baseName('/'), '/');
      expect(parentOf('/DATA/a/b.txt'), '/DATA/a');
      expect(parentOf('/DATA'), '/');
      expect(parentOf('/'), '/');
      expect(joinPath('/', 'x'), '/x');
      expect(joinPath('/DATA', 'x'), '/DATA/x');
    });

    test('isWithin does not match a sibling with the same prefix', () {
      expect(isWithin('/DATA/tank/a', '/DATA/tank'), isTrue);
      expect(isWithin('/DATA/tank', '/DATA/tank'), isTrue);
      expect(isWithin('/DATA/tanker', '/DATA/tank'), isFalse);
      expect(isWithin('/anything', '/'), isTrue);
    });
  });

  group('uniqueName', () {
    test('numbers like the server keeps both', () {
      expect(uniqueName('photo.jpg', {}), 'photo.jpg');
      expect(uniqueName('photo.jpg', {'photo.jpg'}), 'photo (2).jpg');
      expect(uniqueName('photo.jpg', {'photo.jpg', 'photo (2).jpg'}), 'photo (3).jpg');
      expect(uniqueName('Makefile', {'Makefile'}), 'Makefile (2)');
      expect(uniqueName('.env', {'.env'}), '.env (2)');
    });

    test('folders keep dots in their names', () {
      expect(uniqueName('v1.2', {'v1.2'}, isDir: true), 'v1.2 (2)');
    });
  });

  group('validateName', () {
    test('rejects empty, slashes, dots and taken names', () {
      expect(validateName(''), isNotNull);
      expect(validateName('   '), isNotNull);
      expect(validateName('a/b'), isNotNull);
      expect(validateName('..'), isNotNull);
      expect(validateName('x', taken: {'x'}), isNotNull);
      expect(validateName('x', taken: {'x'}, current: 'x'), isNull);
      expect(validateName('Notes 2026.md'), isNull);
    });
  });

  group('sorting', () {
    final a = f('a10.txt', size: 5, mod: DateTime(2026, 1, 3));
    final b = f('A2.txt', size: 50, mod: DateTime(2026, 1, 1));
    final c = f('b.zip', size: 1, mod: DateTime(2026, 1, 2));
    final dir = f('zeta', dir: true);

    test('folders first, names in natural order', () {
      expect(sortEntries([a, b, c, dir], FileSort.name).map((e) => e.name), ['zeta', 'A2.txt', 'a10.txt', 'b.zip']);
    });

    test('by size and date, both directions, folders stay first', () {
      expect(sortEntries([a, b, c, dir], FileSort.size, ascending: false).map((e) => e.name), ['zeta', 'A2.txt', 'a10.txt', 'b.zip']);
      expect(sortEntries([a, b, c, dir], FileSort.modified).map((e) => e.name), ['zeta', 'A2.txt', 'b.zip', 'a10.txt']);
      expect(sortEntries([a, b, c], FileSort.type).map((e) => e.name), ['A2.txt', 'a10.txt', 'b.zip']);
    });

    test('newest and largest first by default', () {
      expect(defaultAscending(FileSort.name), isTrue);
      expect(defaultAscending(FileSort.modified), isFalse);
      expect(defaultAscending(FileSort.size), isFalse);
    });

    test('visibleEntries hides dot files and filters by name', () {
      final list = [f('.git', dir: true), f('Notes.md'), f('photo.jpg')];
      expect(visibleEntries(list).map((e) => e.name), ['Notes.md', 'photo.jpg']);
      expect(visibleEntries(list, showHidden: true).length, 3);
      expect(visibleEntries(list, query: 'NOT').map((e) => e.name), ['Notes.md']);
    });
  });

  group('locations', () {
    const data = FileLocation(label: 'DATA', path: '/DATA', kind: LocationKind.storage);
    const tank = FileLocation(label: 'tank', path: '/DATA/tank', kind: LocationKind.storage);
    const phone = FileLocation(label: 'This phone', path: '/storage/emulated/0', kind: LocationKind.thisPhone);

    test('the longest matching root wins, per side', () {
      expect(locationFor('/DATA/tank/films', [data, tank, phone], isLocal: false), tank);
      expect(locationFor('/DATA/Documents', [data, tank, phone], isLocal: false), data);
      expect(locationFor('/DATA/tanker', [data, tank], isLocal: false), data);
      expect(locationFor('/storage/emulated/0/DCIM', [data, phone], isLocal: true), phone);
      expect(locationFor('/etc', [data], isLocal: false), isNull);
    });

    test('breadcrumbs start at the location', () {
      expect(breadcrumbs('/DATA/tank/films/2024', root: '/DATA/tank', rootLabel: 'tank'), const [
        Crumb('tank', '/DATA/tank'),
        Crumb('films', '/DATA/tank/films'),
        Crumb('2024', '/DATA/tank/films/2024'),
      ]);
      expect(breadcrumbs('/DATA', root: '/DATA', rootLabel: 'DATA'), const [Crumb('DATA', '/DATA')]);
      expect(breadcrumbs('/etc/nginx', root: '/', rootLabel: 'Root'), const [
        Crumb('Root', '/'),
        Crumb('etc', '/etc'),
        Crumb('nginx', '/etc/nginx'),
      ]);
    });

    test('hidden mounts include the VM Manager share (plan M-14)', () {
      expect(isHiddenMount('/DATA/VMs/share'), isTrue);
      expect(isHiddenMount('/DATA/VMs/share/win11'), isTrue);
      expect(isHiddenMount('/data/vm-shares/x'), isTrue);
      expect(isHiddenMount('/boot/efi'), isTrue);
      expect(isHiddenMount('/storage/emulated/0'), isTrue);
      expect(isHiddenMount('/DATA/VMs'), isFalse);
      expect(isHiddenMount('/DATA/tank'), isFalse);
    });

    test('storageLocations turns the system disk into DATA and merges USB', () {
      final list = storageLocations([
        {'mount_point': '/', 'is_system': true, 'size_bytes': 100, 'used_bytes': 40},
        {'mount_point': '/DATA/tank', 'label': 'tank', 'size_bytes': 10, 'used_bytes': 1},
        {'mount_point': '/boot/efi', 'label': ''},
        {'mount_point': '/DATA/VMs/share', 'label': 'share'},
      ], usb: [
        {
          'name': 'SanDisk Ultra',
          'children': [
            {'mount_point': '/media/SANDISK', 'label': 'SANDISK', 'size_bytes': 64, 'used_bytes': 32},
          ],
        },
        {'mount_point': '/DATA/tank'}, // already listed
      ]);
      expect(list.map((l) => (l.label, l.path, l.kind)), [
        ('DATA', '/DATA', LocationKind.storage),
        ('tank', '/DATA/tank', LocationKind.storage),
        ('SANDISK', '/media/SANDISK', LocationKind.usb),
      ]);
      expect(list.first.totalBytes, 100);
      expect(list.last.detail, 'SanDisk Ultra');
    });
  });

  group('conflicts', () {
    test('findConflicts ignores copies into the item’s own folder', () {
      expect(
        findConflicts(['/DATA/a/x.txt', '/DATA/b/y.txt', '/DATA/b/z.txt'], '/DATA/b', {'x.txt', 'y.txt'}),
        ['/DATA/a/x.txt'],
      );
    });

    test('planTransfer: one keep-both job for everything by default', () {
      expect(planTransfer(['/a', '/b'], {}), const [TransferBatch('rename', ['/a', '/b'])]);
    });

    test('planTransfer: replaced items get their own overwrite job, skipped are left out', () {
      final plan = planTransfer(
        ['/a', '/b', '/c', '/d'],
        {'/b': ConflictChoice.replace, '/c': ConflictChoice.skip, '/d': ConflictChoice.keepBoth},
      );
      expect(plan, const [
        TransferBatch('rename', ['/a', '/d']),
        TransferBatch('overwrite', ['/b']),
      ]);
    });

    test('planTransfer: never overwrites without an answer', () {
      final plan = planTransfer(['/a'], {});
      expect(plan.single.style, isNot('overwrite'));
      expect(planTransfer(['/a'], {'/a': ConflictChoice.skip}), isEmpty);
    });

    test('resolveLocalName', () {
      expect(resolveLocalName('a.txt', {'b.txt'}, null), 'a.txt');
      expect(resolveLocalName('a.txt', {'a.txt'}, ConflictChoice.keepBoth), 'a (2).txt');
      expect(resolveLocalName('a.txt', {'a.txt'}, ConflictChoice.replace), 'a.txt');
      expect(resolveLocalName('a.txt', {'a.txt'}, ConflictChoice.skip), isNull);
    });
  });

  group('archives', () {
    test('archive and extract names', () {
      expect(defaultArchiveName(['/DATA/Photos'], {}), 'Photos.zip');
      expect(defaultArchiveName(['/DATA/report.pdf'], {'report.zip'}), 'report (2).zip');
      expect(defaultArchiveName(['/a', '/b'], {}), 'Archive.zip');
      expect(defaultExtractFolder('photos.tar.gz', {}), 'photos');
      expect(defaultExtractFolder('backup.zip', {'backup'}), 'backup (2)');
    });
  });
}
