# Windows PowerShell 5.1+. Without a switch this only previews the import.
[CmdletBinding(DefaultParameterSetName = 'Preview')]
param(
    [Parameter(ParameterSetName = 'Apply', Mandatory)][switch]$Apply,
    [Parameter(ParameterSetName = 'Interactive', Mandatory)][switch]$Interactive
)

$ErrorActionPreference = 'Stop'
$roaming = [IO.Path]::GetFullPath($env:APPDATA)
$source = Join-Path $roaming 'MyGo Music'
$target = Join-Path $roaming 'MyGo Music Plus'
$backups = Join-Path $roaming 'MyGo Music Plus Migration Backups'
$marker = Join-Path $backups 'pending.txt'
$profileItems = @('settings.json', 'session.json', 'window-state.json', 'local', 'models')
$pluginGroups = @('plugins', 'plugin-data')

function Assert-Regular($entry) {
    if ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        throw "Linked profile entries cannot be imported: $($entry.FullName)"
    }
}

function File-Digest([string]$path) {
    $hash = [Security.Cryptography.SHA256]::Create()
    try {
        $stream = [IO.File]::OpenRead($path)
        try { return [Convert]::ToBase64String($hash.ComputeHash($stream)) }
        finally { $stream.Dispose() }
    } finally { $hash.Dispose() }
}

function Copy-Entry($entry, [string]$destination) {
    Assert-Regular $entry
    if ($entry.PSIsContainer) {
        [void][IO.Directory]::CreateDirectory($destination)
        foreach ($child in Get-ChildItem -Force -LiteralPath $entry.FullName) {
            Copy-Entry $child (Join-Path $destination $child.Name)
        }
    } else {
        [IO.File]::Copy($entry.FullName, $destination, $false)
        if ((File-Digest $destination) -cne (File-Digest $entry.FullName)) {
            throw "File changed while copying: $($entry.FullName)"
        }
    }
}

function Read-Settings([string]$path) {
    $settings = ConvertFrom-Json -InputObject ([IO.File]::ReadAllText($path))
    if ($settings -isnot [pscustomobject]) { throw "Expected a settings object in $path" }
    return $settings
}

function Assert-PlayersClosed {
    $running = @(Get-Process -Name 'MyGo Music', 'MyGo Music Plus' -ErrorAction SilentlyContinue)
    if ($running.Count) {
        throw "Quit these players, including their tray icons, and try again: $($running.ProcessName -join ', ')"
    }
}

function Get-PluginId($entry) {
    Assert-Regular $entry
    if (!$entry.PSIsContainer) { return }
    $manifest = Join-Path $entry.FullName 'plugin.json'
    if (Test-Path -LiteralPath $manifest -PathType Leaf) {
        Assert-Regular (Get-Item -Force -LiteralPath $manifest)
        return (Read-Settings $manifest).id
    }
}

function Prepare-Profile([string]$stage) {
    [void][IO.Directory]::CreateDirectory($stage)
    $original = @(Get-ChildItem -Force -LiteralPath $source |
        Where-Object { $_.Name -in ($profileItems + $pluginGroups) })
    if (!$original.Count) { throw 'No upstream settings, library or plugins to import.' }
    foreach ($item in $original) {
        Assert-Regular $item
        if ($item.PSIsContainer -eq $item.Name.EndsWith('.json')) { throw "Unexpected profile entry: $($item.Name)" }
    }
    $existing = @()
    if (Test-Path -LiteralPath $target) { $existing = @(Get-ChildItem -Force -LiteralPath $target) }
    foreach ($item in $existing) {
        Assert-Regular $item
        if ($item.Name -notin $original.Name -and $item.Name -notin $pluginGroups) {
            Copy-Entry $item (Join-Path $stage $item.Name)
        }
    }
    foreach ($item in $original) {
        if ($item.Name -in $pluginGroups) { continue }
        $dest = Join-Path $stage $item.Name
        if ($item.Name -eq 'settings.json') {
            $incoming = Read-Settings $item.FullName
            $previous = Join-Path $target $item.Name
            if (!(Test-Path -LiteralPath $previous)) {
                Copy-Entry $item $dest
                continue
            }
            $merged = Read-Settings $previous
            foreach ($field in $incoming.PSObject.Properties) {
                Add-Member -InputObject $merged -NotePropertyName $field.Name -NotePropertyValue $field.Value -Force
            }
            [IO.File]::WriteAllText($dest, (ConvertTo-Json -Depth 100 -InputObject $merged))
        } else {
            Copy-Entry $item $dest
        }
    }
    # A plugin and its private storage travel together. In particular, a
    # replaced plugin must not inherit an old process grant from Plus.
    $ids = @{}
    foreach ($root in @($source, $target)) {
        $ids[$root] = @{}
        $dir = Join-Path $root 'plugins'
        if (Test-Path -LiteralPath $dir) {
            Assert-Regular (Get-Item -Force -LiteralPath $dir)
            foreach ($item in Get-ChildItem -Force -LiteralPath $dir) {
                if ($item.Name.StartsWith('.')) { continue }
                $pluginId = Get-PluginId $item
                if ($pluginId) {
                    if ($ids[$root].ContainsKey($pluginId)) { throw "Duplicate plugin ID in $dir : $pluginId" }
                    $ids[$root][$pluginId] = $item.Name
                }
            }
        }
    }
    foreach ($group in $pluginGroups) {
        foreach ($root in @($target, $source)) {
            $dir = Join-Path $root $group
            if (!(Test-Path -LiteralPath $dir)) { continue }
            Assert-Regular (Get-Item -Force -LiteralPath $dir)
            foreach ($item in Get-ChildItem -Force -LiteralPath $dir) {
                if ($root -eq $source -and $item.Name.StartsWith('.')) { continue }
                if ($group -eq 'plugins' -and $root -eq $target) {
                    $pluginId = Get-PluginId $item
                    if ($pluginId -and $ids[$source].ContainsKey($pluginId)) { continue }
                    if (Test-Path -LiteralPath (Join-Path (Join-Path $source $group) $item.Name)) {
                        throw "Two different plugins use the folder $($item.Name). The Plus profile is intact."
                    }
                }
                if ($group -eq 'plugin-data') {
                    $id = $item.Name
                    $targetOnly = $ids[$target].ContainsKey($id) -and !$ids[$source].ContainsKey($id)
                    if ($root -eq $source -and $targetOnly) { continue }
                    $incomingData = Join-Path (Join-Path $source $group) $id
                    if ($root -eq $target -and !$targetOnly -and ($ids[$source].ContainsKey($id) -or (Test-Path -LiteralPath $incomingData))) { continue }
                }
                $dest = Join-Path $stage $group
                [void][IO.Directory]::CreateDirectory($dest)
                Copy-Entry $item (Join-Path $dest $item.Name)
            }
        }
    }
}

function Recover-Import {
    if (!(Test-Path -LiteralPath $marker)) { return }
    $id = [IO.File]::ReadAllText($marker)
    if ($id -notmatch '^[a-f0-9]{32}$') { throw "Invalid recovery marker: $marker" }
    $job = Join-Path $backups $id
    Assert-Regular (Get-Item -Force -LiteralPath $job)
    $old = Join-Path $job 'before'
    if ([IO.Directory]::Exists($old)) {
        if (![IO.Directory]::Exists($target)) {
            [IO.Directory]::Move($old, $target)
        } elseif ([IO.Directory]::Exists((Join-Path $job 'prepared'))) {
            throw "Plus was opened during an interrupted import. Its previous profile is in $old. Both copies are preserved."
        }
    }
    [IO.File]::Delete($marker)
}

if (!(Test-Path -LiteralPath $source -PathType Container)) { throw "Upstream profile not found: $source" }
foreach ($dir in @($source, $target, $backups)) {
    if (Test-Path -LiteralPath $dir) { Assert-Regular (Get-Item -Force -LiteralPath $dir) }
}
$items = @(Get-ChildItem -Force -LiteralPath $source | Where-Object { $_.Name -in ($profileItems + $pluginGroups) })
if (!$items.Count) { throw "Nothing to import from $source" }
Write-Output "From: $source" "To:   $target" "Found: $($items.Name -join ', ')"
Write-Output 'Upstream library, session, overlapping preferences and plugins take precedence. Plugin data can include sign-ins and grants.'
Write-Output "The previous Plus profile is backed up under $backups."
if ($Interactive) {
    if ((Read-Host 'Quit both players, then type IMPORT to continue') -cne 'IMPORT') { return }
} elseif (!$Apply) { return }

Assert-PlayersClosed
[void][IO.Directory]::CreateDirectory($backups)
$lock = [IO.File]::Open((Join-Path $backups 'import.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
$exeHandles = @()
try {
    # Prevent either standard installation from starting during the directory swap.
    foreach ($name in @('MyGo Music', 'MyGo Music Plus')) {
        $exe = Join-Path $env:LOCALAPPDATA "Programs\$name\$name.exe"
        if ([IO.File]::Exists($exe)) { $exeHandles += [IO.File]::Open($exe, 'Open', 'Read', 'None') }
    }
    Recover-Import
    $id = [guid]::NewGuid().ToString('N')
    $job = Join-Path $backups $id
    $stage = Join-Path $job 'prepared'
    $old = Join-Path $job 'before'
    try {
        Prepare-Profile $stage
        Assert-PlayersClosed
    } catch {
        # Only this invocation's fresh staging directory exists here. The
        # original and Plus profiles have not been renamed or written.
        if ([IO.Directory]::Exists($stage) -and [IO.Path]::GetFullPath($stage) -eq [IO.Path]::Combine($backups, $id, 'prepared')) {
            [IO.Directory]::Delete($stage, $true)
            [IO.Directory]::Delete($job)
        }
        throw
    }
    $newMarker = Join-Path $job 'pending.tmp'
    [IO.File]::WriteAllText($newMarker, $id)
    [IO.File]::Move($newMarker, $marker)
    try {
        if ([IO.Directory]::Exists($target)) { [IO.Directory]::Move($target, $old) }
        [IO.Directory]::Move($stage, $target)
    } catch {
        Recover-Import
        throw
    }
    [IO.File]::Delete($marker)
    Write-Output "Import complete: $target"
    if ([IO.Directory]::Exists($old)) { Write-Output "Previous Plus profile: $old" }
} finally {
    foreach ($handle in $exeHandles) { $handle.Dispose() }
    $lock.Dispose()
}
if ($Interactive) { [void](Read-Host 'Press Enter to close') }
