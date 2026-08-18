<#
.SYNOPSIS
  Removes the firewall rules Kanpachi's binaries left behind on this machine.

.DESCRIPTION
  # What those rules are and why they are wrong

  Upstream EasyTier writes ALLOW rules into the Windows Firewall over COM while
  it creates the virtual adapter, from inside the network startup. There are two
  kinds and the second one is the bad one:

    EasyTier kanpachi0 - ALL Protocol (Inbound)   allow EVERYTHING over kanpachi0
    EasyTier <path to exe> (Inbound)              allow EVERYTHING towards the
                                                  engine, on EVERY interface of
                                                  the machine, home network
                                                  included

  No port, no source address, in all three profiles, and they survive a reboot
  and uninstalling the program that caused them.

  Kanpachi opens only the ports of the active game profile, only towards the
  addresses of the members present. An allow-everything over the same interface
  undoes that in the very layer Kanpachi uses to grant.

  **The engine no longer writes them.** It consumes an EasyTier fork with those
  two calls deleted, so on a clean machine there is nothing to remove. This
  script exists for machines that ran the OLD engine, which today is the
  development one and tomorrow anybody's who tried an earlier version.

  # What it deletes, and what it does not

  Only what Kanpachi caused, in three shapes:

    - the program path names a Kanpachi binary. This covers BOTH what the old
      engine wrote and what WINDOWS writes on its own: the system adds an allow
      rule for any binary that starts listening, named after the executable,
      with no group, one per distinct path. The uninstaller does not take those
      (it purges by group) and nothing in the product can see them either;
    - the rule name names a Kanpachi adapter;
    - the rule name is one an older build of the product used.

  A user's own EasyTier install, with its own adapter and its own executable,
  matches NEITHER and is left untouched. Deleting somebody else's software is
  exactly the kind of thing this product promises not to do.

  It does not touch the Kanpachi group or Kanpachi-base either: the daemon
  purges the first one on every start, and the second one is the quarantine,
  which is worth precisely because it stays in place with Kanpachi switched off.
  That is checked explicitly and not left to the matchers.

  # Why the UNINSTALLER does not do this

  Because Kanpachi does not delete rules it did not write. It is the same rule
  that makes a game's own leftover rule get switched off with consent and put
  back on the way out, instead of deleted. An uninstaller sweeping by executable
  name would be exercising exactly the capability the product promises not to
  have. So it is a tool, run on purpose, that says what it will do first.

  # Dry by default

  It lists what it would remove and removes nothing. -Apply is needed to delete,
  because this modifies the machine's firewall and a script that deletes by
  default is a script somebody runs without reading.

.PARAMETER Apply
  Actually delete. Without it, it only lists.
#>
[CmdletBinding()]
param(
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'

function Step($t) { Write-Host "`n=== $t ===" -ForegroundColor Cyan }
function Ok($t) { Write-Host "  OK   $t" -ForegroundColor Green }

# Elevation only to delete. Listing is read-only, and demanding UAC to see what
# is about to happen pushes people to skip the very step that exists to avoid
# deleting too much.
if ($Apply) {
    $isAdmin = ([Security.Principal.WindowsPrincipal] `
        [Security.Principal.WindowsIdentity]::GetCurrent()
    ).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $isAdmin) { throw "Deleting rules needs an elevated console. Without -Apply it only lists, and that runs unelevated." }
}

# The binaries Kanpachi runs or has run. The PATH is compared and not just the
# name, for the same reason the adapter kills orphans by full path: this runs
# elevated, and acting on any file that happens to share a name would be acting
# on somebody else's install.
$ours = @(
    'kanpachi-engine.exe', 'kanpachi-engine-spike.exe',
    # Windows writes its own allow rule for ANY binary that starts listening,
    # named after the executable and with no group, one per distinct path. Our
    # binaries get them like any other program does, the uninstaller does not
    # take them (it purges by group), and nothing in the product can see them.
    # Measured on the development machine on 2026-08-18: 82 for the engine, 44
    # for the daemon, 16 for roomprobe, with the product already uninstalled.
    'kanpachid.exe', 'roomprobe.exe'
)

# Rule names from older builds of the product, pointing at paths that no longer
# exist. They carry no group either, so nothing else finds them.
$legacyNames = @('Kanpachi service', 'Kanpachi tunnel engine', 'kanpachid', 'kanpachi-engine')

Step "looking for rules left behind by Kanpachi binaries"

# Two BULK queries and one table, never one query per rule.
#
# This machine has 977 firewall rules. `Get-NetFirewallApplicationFilter` per
# rule is one CIM query per rule, which is minutes: the first version of this
# script hung there and had to be killed. In bulk both together take ~3 seconds.
# A filter's InstanceID is its rule's Name, which is what allows joining them
# without asking again.
$programOf = @{}
foreach ($f in (Get-NetFirewallApplicationFilter -All -ErrorAction SilentlyContinue)) {
    if ($f.Program) { $programOf[$f.InstanceID] = $f.Program }
}

$candidates = Get-NetFirewallRule -ErrorAction SilentlyContinue | ForEach-Object {
    $rule = $_
    $program = $programOf[$rule.Name]

    $byProgram = $false
    if ($program -and $program -ne 'Any') {
        $leaf = Split-Path -Leaf $program
        # One of our binaries by file name, or anything living inside the
        # Kanpachi tree, which covers the vendored easytier-core used to test
        # the seed.
        $byProgram = ($ours -contains $leaf) -or ($program -like '*\kanpachi\*')
    }

    # Interface rules carry no program. They are recognised by their name, which
    # EasyTier composes from the adapter name. And NOT by the interface filter:
    # once the adapter is gone, Windows returns the GUID there instead of the
    # alias, so filtering by alias would match nothing in exactly the normal
    # case, which is a machine with no room open.
    $byName = ($rule.DisplayName -like 'EasyTier kanpachi*') -or
              ($legacyNames -contains $rule.DisplayName)

    # The two groups the product owns are OFF LIMITS, and it is checked here and
    # not trusted to the matchers above: the daemon purges its own group on
    # every start, and the quarantine is worth precisely because it stays with
    # Kanpachi switched off. Everything this script deletes has no group.
    $isOurs = $rule.Group -eq 'Kanpachi' -or $rule.Group -eq 'Kanpachi-base'

    if (($byProgram -or $byName) -and -not $isOurs) {
        [PSCustomObject]@{
            Rule    = $rule
            Name    = $rule.DisplayName
            Group   = if ($rule.Group) { $rule.Group } else { '(no group)' }
            Dir     = $rule.Direction
            Action  = $rule.Action
            Program = if ($program) { $program } else { 'Any' }
        }
    }
}
$candidates = @($candidates)

if ($candidates.Count -eq 0) {
    Ok "none left. This machine is clean."
    exit 0
}

Write-Host "  $($candidates.Count) rule(s):" -ForegroundColor Yellow
$candidates | ForEach-Object {
    Write-Host ("    [{0}] {1}  ({2} {3})" -f $_.Group, $_.Name, $_.Dir, $_.Action) -ForegroundColor Yellow
    if ($_.Program -ne 'Any') { Write-Host "        $($_.Program)" -ForegroundColor DarkGray }
}

if (-not $Apply) {
    Step "dry run"
    Write-Host "  Nothing was deleted. Run it again with -Apply to remove them." -ForegroundColor Cyan
    exit 0
}

Step "deleting"
$deleted = 0
foreach ($c in $candidates) {
    try {
        Remove-NetFirewallRule -Name $c.Rule.Name -ErrorAction Stop
        $deleted++
    }
    catch {
        # It carries on with the rest on purpose: aborting on the first one
        # would leave the cleanup half done with no way to tell how much of it
        # happened.
        Write-Host "  FAIL could not delete '$($c.Name)': $_" -ForegroundColor Red
    }
}
Ok "$deleted of $($candidates.Count) deleted"

Step "checking"
$left = @(Get-NetFirewallRule -ErrorAction SilentlyContinue |
    Where-Object { $_.DisplayName -like 'EasyTier kanpachi*' })
if ($left.Count -gt 0) {
    Write-Host "  $($left.Count) interface rule(s) still there." -ForegroundColor Red
    exit 1
}
Ok "no interface rule of the engine is left"

# The quarantine has to still be whole. This script does not touch it, and
# checking is worth it because it is the only protection that stays in place
# with Kanpachi switched off.
$base = @(Get-NetFirewallRule -Group 'Kanpachi-base' -ErrorAction SilentlyContinue)
Write-Host "  --   the base quarantine still has $($base.Count) rules" -ForegroundColor DarkGray
exit 0
