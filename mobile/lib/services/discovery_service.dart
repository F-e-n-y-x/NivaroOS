import 'dart:io';
import 'package:multicast_dns/multicast_dns.dart';

class DiscoveredServer {
  final String name;
  final String host;
  final int port;
  DiscoveredServer({required this.name, required this.host, required this.port});

  String get url => 'http://$host${port == 80 ? '' : ':$port'}';
}

/// Finds NivaroOS servers on the local network via mDNS/DNS-SD - the same
/// mechanism printers, Chromecasts, and AirPlay devices use to announce
/// themselves, and the reason this needs no configuration on the phone's
/// side at all. The server side of this is a tiny avahi service file
/// (installer/nivaroos-mdns.service, published to /etc/avahi/services/) -
/// nothing NivaroOS-specific needed there either beyond that one file.
///
/// Not every network allows multicast traffic (some guest Wi-Fi/VLAN
/// setups block it) - when nothing turns up, the UI's manual-entry
/// fallback is the real answer for those cases, not a bug in this scan.
class DiscoveryService {
  static const _serviceType = '_nivaroos._tcp.local';

  Stream<DiscoveredServer> discover({Duration timeout = const Duration(seconds: 5)}) async* {
    final client = MDnsClient();
    try {
      await client.start();

      final seen = <String>{};

      await for (final ptr in client
          .lookup<PtrResourceRecord>(ResourceRecordQuery.serverPointer(_serviceType))
          .timeout(timeout, onTimeout: (sink) => sink.close())) {
        await for (final srv in client
            .lookup<SrvResourceRecord>(ResourceRecordQuery.service(ptr.domainName))
            .timeout(const Duration(seconds: 2), onTimeout: (sink) => sink.close())) {
          String? address;
          await for (final ip in client
              .lookup<IPAddressResourceRecord>(ResourceRecordQuery.addressIPv4(srv.target))
              .timeout(const Duration(seconds: 2), onTimeout: (sink) => sink.close())) {
            address = ip.address.address;
            break;
          }
          address ??= srv.target;

          final key = '$address:${srv.port}';
          if (seen.contains(key)) continue;
          seen.add(key);

          yield DiscoveredServer(
            name: ptr.domainName.replaceAll('.$_serviceType', ''),
            host: address,
            port: srv.port,
          );
        }
      }
    } on SocketException {
      // Multicast unavailable on this network/interface - the manual
      // "enter address" fallback in the UI is the real path here, not an
      // error to surface loudly.
    } finally {
      client.stop();
    }
  }
}
