import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';

/// El offline ocupa el hueco donde iría el ping, y en las tres caras es igual.
///
/// Un miembro con ficha viva al que el motor no ve NO se fue: su silla sigue
/// puesta y volverá a ella con su misma dirección. Lo que no tiene es una
/// medición de ida y vuelta, y dejar ese hueco vacío se lee como «todavía no se
/// midió», que es lo que el host dijo durante treinta y tres horas mientras
/// nadie podía entrar.
///
/// Se llamaba AFK hasta el 2026-08-26. AFK afirma que la persona se levantó de
/// la silla, y lo único medido es que el motor dejó de verla.
void main() {
  group('Member.fromJson lee la ausencia del cable', () {
    test('presente con medición', () {
      final Member m = Member.fromJson(<String, Object?>{
        'name': 'pericoman',
        'ip': '100.93.137.2',
        'path': 'direct',
        'rtt_ms': 42,
      });
      expect(m.isAway, isFalse);
      expect(m.latencyMs, 42);
      expect(m.meta, '100.93.137.2 · directo · 42 ms');
    });

    test('ausente hace minutos', () {
      final Member m = Member.fromJson(<String, Object?>{
        'name': 'wololo',
        'ip': '100.93.137.4',
        'path': 'direct',
        'away': true,
        'away_for_ms': 3 * 60 * 1000,
        'seat_frees_in_ms': 20 * 60 * 60 * 1000,
      });
      expect(m.isAway, isTrue);
      expect(m.awayLabel, 'offline 3m');
      expect(m.latencyMs, isNull);
      expect(m.meta, contains('offline 3m'));
      expect(m.meta, isNot(contains('ms')));
    });

    test('ausente sin saber desde cuándo', () {
      final Member m = Member.fromJson(<String, Object?>{
        'name': 'jorungador',
        'ip': '100.93.137.5',
        'away': true,
      });
      expect(m.awayLabel, 'offline');
    });

    test('ausente hace horas', () {
      final Member m = Member.fromJson(<String, Object?>{
        'name': 'kek',
        'ip': '100.93.137.6',
        'away': true,
        'away_for_ms': 5 * 3600 * 1000,
      });
      expect(m.awayLabel, 'offline 5h');
    });
  });
}
