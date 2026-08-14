<#
.SYNOPSIS
    Prints the CHANGELOG section for one version, to be quoted in the release
    body.

.DESCRIPTION
    # Why it is extracted and not written by hand in the release

    Writing it twice means having it different twice, and the one people read
    would be the one nobody reviewed. CHANGELOG.md is already maintained entry
    by entry, in the same commit as the change; this only quotes it.

    # No entry, no publication, on purpose

    A tagged version without a line saying what changed is exactly what that
    file exists to prevent, and the moment to find out is BEFORE publishing, not
    after. So a missing or empty section fails here and takes the release with
    it.

    The manual run is the one exception: with no tag the version is 0.0.0 and
    there is nothing to quote.

    Only the notes go to stdout, so the caller can capture them whole. Anything
    else this script has to say goes to stderr.

.PARAMETER Version
    Without the leading v: 0.2.0, not v0.2.0. It is the same number the release
    workflow derives from the tag.

.EXAMPLE
    scripts\release-notes.ps1 -Version 0.2.0
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Version
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$changelog = Join-Path $root 'CHANGELOG.md'

if ($Version -eq '0.0.0') {
    Write-Output "_Manual run, no tag: there is no changelog section to quote._"
    exit 0
}

if (-not (Test-Path $changelog)) { throw "CHANGELOG.md is not at $changelog" }

# -Encoding UTF8 explicitly: Windows PowerShell 5.1 reads as the ANSI codepage
# by default, and the changelog carries em dashes and accents. Without this, a
# run outside pwsh 7 puts mojibake straight into the release body.
$lines = Get-Content $changelog -Encoding UTF8
$header = "^##\s+\[$([regex]::Escape($Version))\]"
$from = ($lines | Select-String -Pattern $header | Select-Object -First 1).LineNumber
if (-not $from) {
    throw ("CHANGELOG.md has no section for version $Version. " +
        "Before tagging, the Unreleased section becomes '## [$Version] - YYYY-MM-DD'.")
}

# `LineNumber` is 1-based and the cut goes AFTER the header, so this index
# already points at the next line.
$rest = $lines[$from..($lines.Count - 1)]
$to = ($rest | Select-String -Pattern '^##\s' | Select-Object -First 1).LineNumber

# The `-eq 1` is not defence for its own sake: with an empty section, `0..-1` in
# PowerShell is not an empty range, it returns the first element and the last
# one. So the case to detect would come out as two lines of another version
# glued on, instead of jumping.
$body = if ($to -eq 1) { @() }
        elseif ($to) { $rest[0..($to - 2)] }
        else { $rest }

# The reference links in the footer mean nothing outside the file, and a
# `## [0.1.3]` without its definition would come out as loose text.
$body = $body | Where-Object { $_ -notmatch '^\[[0-9]' }
$text = ($body -join "`n").Trim()
if (-not $text) { throw "The section for $Version in CHANGELOG.md is empty." }

Write-Output $text
