import 'package:ffi/ffi.dart';
import 'package:win32/win32.dart';

/// Opens a link in whatever browser this machine uses.
///
/// # Why `ShellExecute` and not `start`
///
/// The usual shortcut is `Process.run('cmd', ['/c', 'start', '', url])`, and it
/// costs two things. It flashes a console window — small, but this is an app
/// that draws its own title bar precisely so nothing of the system leaks
/// through — and it hands a URL to the command interpreter, which has its own
/// ideas about `&` and `^`. `ShellExecute` is the API the shell itself uses:
/// no interpreter, no window, and the same resolution of the default browser.
///
/// # Why it only accepts https
///
/// Because `ShellExecute` opens ANYTHING: a `file:` path, a `.exe`, a
/// registered protocol handler. Everything this app opens is its own download
/// page, so accepting only `https` costs nothing today and means a link that
/// ever comes from outside cannot become a way to run something. The wrong
/// scheme is refused rather than corrected: silently rewriting a URL somebody
/// wrote is how the wrong page gets opened.
abstract final class SystemBrowser {
  /// Opens [url]. Returns whether Windows accepted it.
  ///
  /// A false is not worth a dialog: the user pressed a link and no browser
  /// appeared, which they can see. The caller decides, and today nobody does
  /// anything with it beyond not pretending it worked.
  static bool open(String url) {
    final Uri? destino = Uri.tryParse(url);
    if (destino == null || destino.scheme != 'https') return false;

    // Allocated and freed with the SAME allocator, said out loud: mixing the
    // two works every day and corrupts the heap on the bad one.
    final PCWSTR ancho = destino.toString().toPcwstr(allocator: malloc);
    try {
      // The documented success test, and it really is this odd: ShellExecute
      // returns a fake HINSTANCE, and anything at or below 32 is an error code
      // rather than a handle.
      return ShellExecute(null, null, ancho, null, null, SW_SHOWNORMAL).address >
          32;
    } finally {
      malloc.free(ancho);
    }
  }
}
