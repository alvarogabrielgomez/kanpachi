/// The methods of the local API, and how long each one is given to answer.
///
/// # Why the names live here instead of loose across the repository
///
/// Because they are the contract with the daemon, and typing them by hand at
/// every call site turns a typo into a `bad_request` that reaches the screen
/// looking like a product failure. Here they get written once.
///
/// The list is closed on the daemon side: `protocol.métodos` rejects anything
/// outside it, and it does so BEFORE checking the greeting.
library;

/// The 27 methods, with the exact name that travels on the wire.
abstract final class DaemonMethods {
  static const String hello = 'hello';

  static const String createRoom = 'create_room';
  static const String joinRoom = 'join_room';
  static const String leaveRoom = 'leave_room';
  static const String activateProfile = 'activate_profile';
  static const String kickMember = 'kick_member';
  static const String rotateInviteCode = 'rotate_invite_code';
  static const String renameRoom = 'rename_room';
  static const String inviteLink = 'invite_link';
  static const String status = 'status';

  static const String listGames = 'list_games';
  static const String rejectedGames = 'rejected_games';
  static const String saveProfile = 'save_profile';
  static const String importCatalog = 'import_catalog';
  static const String exportCatalog = 'export_catalog';
  static const String markVerified = 'mark_verified';

  static const String foreignRules = 'foreign_rules_for';
  static const String suspendForeignRules = 'suspend_foreign_rules';
  static const String diagReport = 'diag_report';
  static const String observeGame = 'observe_game';
  static const String exposure = 'exposure';
  static const String probeHost = 'probe_host';

  static const String reapplyProtection = 'reapply_protection';

  static const String pendingRoom = 'pending_room';
  static const String resumeRoom = 'resume_room';
  static const String discardPendingRoom = 'discard_pending_room';
  static const String lastRoom = 'last_room';

  /// Steps of the long operation in flight.
  ///
  /// Asked down a SECOND connection: one connection's loop is sequential, so
  /// down the same one this would queue behind what it wants to watch. See
  /// `DaemonConnector.spare`.
  static const String progress = 'progress';

  // The three that are about the PROCESS rather than the room. The daemon
  // answers these from its host, not from the session use case.

  /// Shut everything down, in order, and stop the daemon.
  static const String shutdown = 'shutdown';

  /// Read, and optionally change, whether Kanpachi starts with Windows.
  static const String autostart = 'autostart';

  /// Cut short the long operation in flight. Same rule as [progress]: it goes
  /// down a connection of its own, because the one carrying the operation is
  /// busy carrying it.
  static const String cancel = 'cancel';

  /// Show the window. The app never calls this one — the DAEMON does, when the
  /// shortcut is double-clicked with Kanpachi already running. It is listed
  /// here because the wire name has to live in one place.
  static const String showUi = 'show_ui';
}

/// The default budget: what something answered from memory or with one small
/// disk write takes.
const Duration kDefaultTimeout = Duration(seconds: 10);

/// How long each method gets, when it is not the default.
///
/// # Why one number for everything does not work
///
/// Because `create_room` legitimately takes around a minute. The engine gives
/// itself up to thirty seconds PER ADAPTER to take an address, a room raises
/// two of them (the room and the lobby), and the MTU probe runs on top. With
/// ten seconds for everything, creating a room times out ALWAYS against a
/// daemon that is working correctly, and the symptom is the worst one
/// available: the room exists, the screen says it failed, and the user creates
/// it again. `kanpctl` already uses ninety seconds for this exact reason, with
/// the reason written next to it.
///
/// The thirty seconds of the firewall group are not generosity: enumerating the
/// Windows rule store over COM is hundreds of round trips, and on a machine
/// with a slow antivirus it shows.
const Map<String, Duration> kMethodTimeouts = <String, Duration>{
  DaemonMethods.createRoom: Duration(seconds: 90),
  DaemonMethods.joinRoom: Duration(seconds: 90),
  DaemonMethods.resumeRoom: Duration(seconds: 90),

  DaemonMethods.activateProfile: Duration(seconds: 30),
  DaemonMethods.reapplyProtection: Duration(seconds: 30),
  DaemonMethods.suspendForeignRules: Duration(seconds: 30),
  DaemonMethods.kickMember: Duration(seconds: 30),
  DaemonMethods.rotateInviteCode: Duration(seconds: 30),
  DaemonMethods.renameRoom: Duration(seconds: 30),
  DaemonMethods.leaveRoom: Duration(seconds: 30),
  DaemonMethods.probeHost: Duration(seconds: 30),

  DaemonMethods.exposure: Duration(seconds: 20),
  DaemonMethods.foreignRules: Duration(seconds: 20),
  DaemonMethods.diagReport: Duration(seconds: 20),
  DaemonMethods.importCatalog: Duration(seconds: 20),
  DaemonMethods.exportCatalog: Duration(seconds: 20),
  DaemonMethods.observeGame: Duration(seconds: 20),
};

/// The budget of a method.
///
/// The greeting deliberately stays on the default. The daemon hangs up after
/// five seconds if nobody greets, so with ten on this side the one reporting a
/// slow greeting is the daemon closing the pipe, which is the more accurate of
/// the two messages.
Duration defaultTimeoutFor(String method) =>
    kMethodTimeouts[method] ?? kDefaultTimeout;
