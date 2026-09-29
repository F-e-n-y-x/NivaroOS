// How the server reaches a phone's files (S-04): parsed from the device
// list and worded for the Files phone locations and the Phones screen.
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/device_sync_service.dart';

void main() {
  test('route from the device list', () {
    CompanionDevice dev(Object? route) => CompanionDevice.fromJson({'id': 'd', 'name': 'P', 'connection': 'remote', 'route': ?route});
    expect(dev('tailscale').route, CompanionRoute.tailscale);
    expect(dev('tunnel').route, CompanionRoute.tunnel);
    expect(dev('tunnel_list').route, CompanionRoute.tunnelList);
    expect(dev('lan').route, CompanionRoute.lan);
    expect(dev(null).route, CompanionRoute.none, reason: 'an older server sends no route');
    expect(dev('something-new').route, CompanionRoute.none);
    expect(dev('tunnel').copyWith(name: 'Q').route, CompanionRoute.tunnel);
  });

  test('labels say how files travel, and whether they work', () {
    expect(CompanionRoute.tailscale.label, 'Direct · Tailscale');
    expect(CompanionRoute.tunnel.label, 'Through the server · slower');
    expect(CompanionRoute.tunnelList.label, contains('update the app'));
    expect(CompanionRoute.none.label, isNull);
    expect([for (final r in CompanionRoute.values) if (r.filesWork) r], [CompanionRoute.lan, CompanionRoute.tailscale, CompanionRoute.tunnel]);
  });
}
