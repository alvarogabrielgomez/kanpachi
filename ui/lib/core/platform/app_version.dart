/// The version this build is, written in at compile time.
///
/// # Where it comes from
///
/// `scripts/build-production.ps1` passes `--dart-define=KANPACHI_VERSION`, and
/// the release workflow feeds it the tag it is publishing, without the `v`. It
/// is the same number the installer stamps on the `.exe`, so what the window
/// believes it is and what Windows says it is cannot drift apart.
///
/// # Why `dev` is an answer and not a placeholder
///
/// A build compiled by hand has no tag to be, and the honest thing to say is
/// so. Everything that compares versions treats `dev` as "do not compare":
/// nagging whoever compiled this to go download an installer is both wrong and
/// insulting. See `features/update`.
const String kAppVersion = String.fromEnvironment(
  'KANPACHI_VERSION',
  defaultValue: 'dev',
);
