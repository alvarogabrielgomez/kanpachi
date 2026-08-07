/// Comparing two version numbers, which is the whole decision behind the
/// notice.
///
/// Small on purpose, and here rather than in the cubit: this is the one part
/// that can be wrong in a way nobody notices. A comparison that says yes too
/// easily nags forever; one that says no too easily never announces anything,
/// and looks exactly like a working feature.
library;

/// Whether [candidate] names a version later than [running].
///
/// Both may carry a leading `v`: the seed hands back the GitHub tag verbatim
/// (`v0.1.5`) and the build is stamped without it (`0.1.5`). Normalising here
/// rather than at each call site is what stops one of the two forgetting.
///
/// # What is deliberately NOT newer
///
///  - **Anything unparseable, on either side.** `dev` builds land here, and so
///    would a tag somebody names by hand. The answer is no: an app that
///    announces a version it could not read is announcing noise.
///  - **A prerelease of the version already running.** `0.2.0-rc1` against
///    `0.2.0` compares as the same three numbers, so it is not newer. That is
///    the right answer for the only thing this drives — a download button that
///    would otherwise send a stable user to a release candidate.
///  - **The same version.** Obvious, and worth stating: the check runs on every
///    start, so "equal" is the answer it gives almost every time.
bool isNewerVersion(String candidate, String running) {
  final List<int>? nuevo = _parse(candidate);
  final List<int>? actual = _parse(running);
  if (nuevo == null || actual == null) return false;
  for (int i = 0; i < 3; i++) {
    if (nuevo[i] != actual[i]) return nuevo[i] > actual[i];
  }
  return false;
}

/// Whether [version] is a number this can reason about at all.
///
/// Exists so the caller can skip the network request entirely rather than
/// making it and throwing the answer away. `dev` builds are the case.
bool isComparableVersion(String version) => _parse(version) != null;

/// `MAJOR.MINOR.PATCH` as three numbers, or null when it is not that.
///
/// The `-rc1` suffix is dropped rather than ordered. Ordering prereleases needs
/// the whole SemVer precedence table, and the one case it would decide —
/// telling somebody on `0.2.0-rc1` that `0.2.0` is out — is not one Kanpachi
/// creates: prereleases are not published to the download page.
List<int>? _parse(String raw) {
  String texto = raw.trim();
  if (texto.startsWith('v') || texto.startsWith('V')) {
    texto = texto.substring(1);
  }
  final int guion = texto.indexOf('-');
  if (guion >= 0) texto = texto.substring(0, guion);

  final List<String> partes = texto.split('.');
  if (partes.length != 3) return null;
  final List<int> numeros = <int>[];
  for (final String parte in partes) {
    final int? n = int.tryParse(parte);
    if (n == null || n < 0) return null;
    numeros.add(n);
  }
  return numeros;
}
