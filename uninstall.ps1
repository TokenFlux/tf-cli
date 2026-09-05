#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$InstallDir = $env:TF_INSTALL_DIR,
    [switch]$Purge
)

& {
    param($InstallDir, $Purge)
    $ErrorActionPreference = 'Stop'
    Set-StrictMode -Version 2.0
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'This uninstaller requires Windows.'
    }
    if (-not $env:USERPROFILE) { throw 'USERPROFILE is not set.' }
    if (-not $InstallDir) { $InstallDir = Join-Path $env:USERPROFILE '.local\bin' }
    function Resolve-TfPath([string]$Path) {
        $provider = $null
        $drive = $null
        $resolved = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Path, [ref]$provider, [ref]$drive)
        if ($provider.Name -ne 'FileSystem') { throw 'Expected a filesystem path.' }
        return $resolved
    }
    $InstallDir = Resolve-TfPath $InstallDir
    function Get-TfItem([string]$Path) {
        try { Get-Item -LiteralPath $Path -Force -ErrorAction Stop }
        catch [System.Management.Automation.ItemNotFoundException] { return $null }
    }
    $targets = @((Join-Path $InstallDir 'tf.exe'), (Join-Path $InstallDir 'tf.exe.old'))
    foreach ($target in $targets) {
        $item = Get-TfItem $target
        if ($item -and ($item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint))) {
            throw "Refusing to remove a directory or link in place of a binary: $target"
        }
    }

    $config = if ($env:XDG_CONFIG_HOME) { Join-Path $env:XDG_CONFIG_HOME 'tf' } else { Join-Path $env:USERPROFILE '.tf' }
    $cache = if ($env:XDG_CACHE_HOME) { Join-Path $env:XDG_CACHE_HOME 'tf' } else { Join-Path $config 'cache' }
    $config = Resolve-TfPath $config
    $cache = Resolve-TfPath $cache
    foreach ($path in @($config, $cache)) {
        if ($path.TrimEnd('\') -ieq ([IO.Path]::GetPathRoot($path)).TrimEnd('\') -or
            $path.TrimEnd('\') -ieq $env:USERPROFILE.TrimEnd('\')) {
            throw "Refusing an unsafe configuration path: $path"
        }
    }

    function Remove-TfTree([string]$Path) {
        $item = Get-TfItem $Path
        if (-not $item) { return }
        # PowerShell 5.1 recursive removal can traverse junctions. Unlink them only.
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
            if ($item.PSIsContainer) { [IO.Directory]::Delete($Path, $false) } else { [IO.File]::Delete($Path) }
        } elseif ($item.PSIsContainer) {
            foreach ($child in [IO.Directory]::GetFileSystemEntries($Path)) { Remove-TfTree $child }
            [IO.Directory]::Delete($Path, $false)
        } else {
            [IO.File]::Delete($Path)
        }
    }

    foreach ($target in $targets) {
        if ([IO.File]::Exists($target)) {
            [IO.File]::Delete($target)
            Write-Host "Removed: $target"
        }
    }
    if ($Purge) {
        # A nested cache is removed by the config traversal, which also handles links.
        if (-not $cache.StartsWith($config.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase) -and $cache -ine $config) {
            Remove-TfTree $cache
        }
        Remove-TfTree $config
        Write-Host 'Removed tf credentials, configuration and cache.'
    } else {
        Write-Host "Credentials and configuration kept: $config"
        Write-Host "Cache kept: $cache"
        Write-Host 'Use -Purge to remove them too.'
    }
} $InstallDir $Purge
