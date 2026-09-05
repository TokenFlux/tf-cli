#Requires -Version 5.1
[CmdletBinding()]
param(
    [string]$InstallDir = $env:TF_INSTALL_DIR,
    [uri]$Proxy
)

# Keep preferences scoped even when invoked through Invoke-Expression.
& {
    param($InstallDir, $Proxy)
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    Set-StrictMode -Version 2.0

    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'This installer requires Windows.'
    }
    $arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    if ($arch -ne 'AMD64') { throw 'Windows releases currently support x64 only.' }
    if (-not $InstallDir) {
        if (-not $env:USERPROFILE) { throw 'USERPROFILE is not set.' }
        $InstallDir = Join-Path $env:USERPROFILE '.local\bin'
    }
    $provider = $null
    $drive = $null
    $InstallDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($InstallDir, [ref]$provider, [ref]$drive)
    if ($provider.Name -ne 'FileSystem') { throw 'InstallDir must be a filesystem path.' }
    $target = Join-Path $InstallDir 'tf.exe'
    try { $existing = Get-Item -LiteralPath $target -Force -ErrorAction Stop }
    catch [System.Management.Automation.ItemNotFoundException] { $existing = $null }
    if ($existing -and ($existing.PSIsContainer -or ($existing.Attributes -band [IO.FileAttributes]::ReparsePoint))) {
        throw "Refusing to replace a directory or link: $target"
    }

    $request = @{ UserAgent = 'tf-cli-installer'; TimeoutSec = 120 }
    if (-not $Proxy -and $env:HTTPS_PROXY) { $Proxy = [uri]$env:HTTPS_PROXY }
    if ($Proxy) { $request.Proxy = $Proxy }
    $tls = [Net.ServicePointManager]::SecurityProtocol
    $temp = $null
    $staged = $null
    try {
        [Net.ServicePointManager]::SecurityProtocol = $tls -bor [Net.SecurityProtocolType]::Tls12
        Write-Host 'Checking the latest release...'
        $release = Invoke-RestMethod @request -Uri 'https://api.github.com/repos/tokenflux/tf-cli/releases/latest'
        $tag = [string]$release.tag_name
        if ($tag -cnotmatch '\Av[0-9][0-9A-Za-z.+-]*\z') { throw 'Invalid release version.' }
        $version = $tag.Substring(1)
        $asset = "tf_${version}_windows_amd64.zip"
        $base = "https://github.com/tokenflux/tf-cli/releases/download/$tag"
        $temp = Join-Path ([IO.Path]::GetTempPath()) ('tf-install-' + [guid]::NewGuid().ToString('N'))
        $null = [IO.Directory]::CreateDirectory($temp)
        $archive = Join-Path $temp $asset
        $sums = Join-Path $temp 'SHA256SUMS'
        Write-Host "Downloading $asset"
        Invoke-WebRequest @request -UseBasicParsing -Uri "$base/$asset" -OutFile $archive
        Invoke-WebRequest @request -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile $sums

        $hashes = @(foreach ($line in [IO.File]::ReadAllLines($sums)) {
            $match = [regex]::Match($line, '\A([a-fA-F0-9]{64})\s+\*?(.+?)\s*\z')
            if ($match.Success -and $match.Groups[2].Value -ceq $asset) { $match.Groups[1].Value }
        })
        if ($hashes.Count -ne 1) { throw "Expected exactly one SHA256SUMS entry for $asset." }
        if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -ine $hashes[0]) {
            throw 'Checksum mismatch; the existing installation was not changed.'
        }
        Write-Host 'SHA256 verified.'
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $zip = [IO.Compression.ZipFile]::OpenRead($archive)
        try {
            $entries = @($zip.Entries | Where-Object { $_.FullName -ceq 'tf.exe' })
            if ($entries.Count -ne 1 -or $entries[0].Length -eq 0) {
                throw 'Archive must contain exactly one nonempty tf.exe.'
            }
            $null = [IO.Directory]::CreateDirectory($InstallDir)
            $staged = Join-Path $InstallDir ('.tf-install-' + [guid]::NewGuid().ToString('N'))
            $output = [IO.File]::Open($staged, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
            try {
                $inputStream = $entries[0].Open()
                try { $inputStream.CopyTo($output) } finally { $inputStream.Dispose() }
                $output.Flush($true)
            } finally { $output.Dispose() }
        } finally { $zip.Dispose() }

        # Both files are on the same filesystem. Never remove the old binary first.
        try {
            if ([IO.File]::Exists($target)) {
                [IO.File]::Replace($staged, $target, [NullString]::Value)
            } else {
                [IO.File]::Move($staged, $target)
            }
            $staged = $null
        } catch {
            throw "Cannot replace tf.exe. Close running tf processes and retry. $($_.Exception.Message)"
        }
        Write-Host "Installed tf ${version}: $target"
        Write-Host 'Run the installed program with:'
        Write-Host ("  & '{0}' login" -f $target.Replace("'", "''"))
        if (-not ($env:Path -split ';' | Where-Object { $_.TrimEnd('\') -ieq $InstallDir.TrimEnd('\') })) {
            Write-Host 'To make tf available in this PowerShell session:'
            Write-Host ("  `$env:Path = '{0};' + `$env:Path" -f $InstallDir.Replace("'", "''"))
            Write-Host 'For future sessions, add this directory to your user PATH in Windows Environment Variables.'
        }
    } finally {
        [Net.ServicePointManager]::SecurityProtocol = $tls
        if ($staged -and [IO.File]::Exists($staged)) { [IO.File]::Delete($staged) }
        if ($temp -and [IO.Directory]::Exists($temp)) { [IO.Directory]::Delete($temp, $true) }
    }
} $InstallDir $Proxy
