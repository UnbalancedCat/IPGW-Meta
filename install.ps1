[CmdletBinding()]
param(
    [string]$Version = $env:IPGW_VERSION,
    [string]$InstallRoot = $env:IPGW_INSTALL_ROOT,
    [string]$BinDir = $env:IPGW_BIN_DIR
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Set-StrictMode -Version Latest

if ([string]::IsNullOrWhiteSpace($InstallRoot)) {
    $LocalAppData = [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
    if ([string]::IsNullOrWhiteSpace($LocalAppData)) {
        throw 'Unable to resolve the Windows local application data directory'
    }
    $InstallRoot = Join-Path (Join-Path $LocalAppData 'Programs') 'IPGW-Meta'
}
if ([string]::IsNullOrWhiteSpace($BinDir)) {
    $BinDir = Join-Path $InstallRoot 'bin'
}
foreach ($CandidatePath in @($InstallRoot, $BinDir)) {
    if (-not [IO.Path]::IsPathRooted($CandidatePath) -or $CandidatePath.StartsWith('\\')) {
        throw 'Install paths must be absolute paths on a local Windows volume'
    }
}
$InstallRoot = [IO.Path]::GetFullPath($InstallRoot)
$BinDir = [IO.Path]::GetFullPath($BinDir)
if ($InstallRoot.TrimEnd('\') -ieq [IO.Path]::GetPathRoot($InstallRoot).TrimEnd('\') -or
    $BinDir.TrimEnd('\') -ieq [IO.Path]::GetPathRoot($BinDir).TrimEnd('\')) {
    throw 'Install paths cannot be a volume root'
}

$NativeArch = if ($env:PROCESSOR_ARCHITEW6432) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}
switch ($NativeArch.ToUpperInvariant()) {
    'AMD64' { $GoArch = 'amd64' }
    'ARM64' { $GoArch = 'arm64' }
    default { throw "Unsupported Windows architecture: $NativeArch" }
}

$Target = "windows-$GoArch"
$ArchiveName = "ipgw-meta-$Target.zip"
$RepositoryURL = 'https://github.com/UnbalancedCat/ipgw-meta'
if ([string]::IsNullOrWhiteSpace($Version)) {
    $ReleaseBase = "$RepositoryURL/releases/latest/download"
} else {
    if ($Version -notmatch '^v?[0-9A-Za-z][0-9A-Za-z._+-]*$') {
        throw 'Invalid IPGW_VERSION'
    }
    $ReleaseBase = "$RepositoryURL/releases/download/$Version"
}

[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

function Remove-ExactPath {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    $Item = Get-Item -LiteralPath $LiteralPath -Force -ErrorAction SilentlyContinue
    if ($null -eq $Item) { return }
    if (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        Remove-Item -LiteralPath $LiteralPath -Force
    } elseif ($Item.PSIsContainer) {
        Remove-Item -LiteralPath $LiteralPath -Recurse -Force
    } else {
        Remove-Item -LiteralPath $LiteralPath -Force
    }
}

function Set-PrivateDirectoryAcl {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    $Identity = [Security.Principal.WindowsIdentity]::GetCurrent().User
    $Acl = New-Object Security.AccessControl.DirectorySecurity
    $Acl.SetOwner($Identity)
    $Acl.SetAccessRuleProtection($true, $false)
    $Inheritance = [Security.AccessControl.InheritanceFlags]::ContainerInherit -bor
        [Security.AccessControl.InheritanceFlags]::ObjectInherit
    $Rule = New-Object Security.AccessControl.FileSystemAccessRule(
        $Identity,
        [Security.AccessControl.FileSystemRights]::FullControl,
        $Inheritance,
        [Security.AccessControl.PropagationFlags]::None,
        [Security.AccessControl.AccessControlType]::Allow
    )
    [void]$Acl.AddAccessRule($Rule)
    Set-Acl -LiteralPath $LiteralPath -AclObject $Acl
}

function Write-PrivateFile {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$Content
    )
    $Encoding = New-Object Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($LiteralPath, $Content, $Encoding)
    $Stream = [IO.File]::Open(
        $LiteralPath,
        [IO.FileMode]::Open,
        [IO.FileAccess]::ReadWrite,
        [IO.FileShare]::None
    )
    try {
        $Stream.Flush($true)
    } finally {
        $Stream.Dispose()
    }
}

function Ensure-RealDirectory {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    $Item = Get-Item -LiteralPath $LiteralPath -Force -ErrorAction SilentlyContinue
    if ($null -eq $Item) {
        [void](New-Item -ItemType Directory -Path $LiteralPath -Force)
        $Item = Get-Item -LiteralPath $LiteralPath -Force -ErrorAction Stop
    }
    if (-not $Item.PSIsContainer -or
        (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw "Expected a real directory, not a file or reparse point: $LiteralPath"
    }
}

function Get-RealFileIfPresent {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    $Item = Get-Item -LiteralPath $LiteralPath -Force -ErrorAction SilentlyContinue
    if ($null -eq $Item) { return $null }
    if ($Item.PSIsContainer -or
        (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw "Expected a regular file, not a directory or reparse point: $LiteralPath"
    }
    return $Item
}

function Get-ManagedJunctionTarget {
    param(
        [Parameter(Mandatory = $true)]$Item,
        [Parameter(Mandatory = $true)][string]$VersionsPath
    )
    if (-not $Item.PSIsContainer -or
        (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0) -or
        $null -eq $Item.PSObject.Properties['LinkType'] -or
        $Item.LinkType -cne 'Junction' -or
        $null -eq $Item.PSObject.Properties['Target']) {
        throw 'Existing binary path is not an IPGW-Meta managed junction'
    }
    $Targets = @($Item.Target)
    if ($Targets.Count -ne 1 -or [string]::IsNullOrWhiteSpace([string]$Targets[0]) -or
        -not [IO.Path]::IsPathRooted([string]$Targets[0])) {
        throw 'Existing binary junction has an invalid target'
    }
    $TargetPath = [IO.Path]::GetFullPath([string]$Targets[0])
    $VersionsPrefix = [IO.Path]::GetFullPath($VersionsPath).TrimEnd('\') + '\'
    if (-not $TargetPath.StartsWith($VersionsPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Existing binary junction is outside the managed versions directory'
    }
    $VersionName = $TargetPath.Substring($VersionsPrefix.Length)
    if ($VersionName -cnotmatch '^[0-9a-f]{16}-[0-9]{14}-[0-9a-f]{8}$') {
        throw 'Existing binary junction does not target one managed version directory'
    }
    $TargetItem = Get-Item -LiteralPath $TargetPath -Force -ErrorAction Stop
    if (-not $TargetItem.PSIsContainer -or
        (($TargetItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw 'Existing binary junction target is not a real version directory'
    }
    return $TargetPath
}

function Invoke-HttpsDownload {
    param(
        [Parameter(Mandatory = $true)][Uri]$Uri,
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][long]$MaxBytes
    )
    if (-not $Uri.IsAbsoluteUri -or $Uri.Scheme -ine [Uri]::UriSchemeHttps) {
        throw 'Release downloads require an absolute HTTPS URI'
    }
    if ($MaxBytes -le 0) {
        throw 'Download size limit must be positive'
    }
    if (Test-Path -LiteralPath $LiteralPath) {
        throw 'Refusing to overwrite an existing download path'
    }

    Add-Type -AssemblyName System.Net.Http
    $Handler = New-Object System.Net.Http.HttpClientHandler
    $Handler.AllowAutoRedirect = $false
    $Client = New-Object System.Net.Http.HttpClient($Handler)
    $Client.Timeout = [TimeSpan]::FromMinutes(5)
    try {
        $CurrentUri = $Uri
        for ($RedirectCount = 0; $RedirectCount -le 5; $RedirectCount++) {
            $Request = New-Object System.Net.Http.HttpRequestMessage(
                [System.Net.Http.HttpMethod]::Get,
                $CurrentUri
            )
            $Response = $null
            try {
                $Response = $Client.SendAsync(
                    $Request,
                    [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead
                ).GetAwaiter().GetResult()
                $StatusCode = [int]$Response.StatusCode
                if ($StatusCode -in @(301, 302, 303, 307, 308)) {
                    if ($RedirectCount -ge 5) {
                        throw 'Release download exceeded the five-redirect limit'
                    }
                    $Location = $Response.Headers.Location
                    if ($null -eq $Location) {
                        throw 'Release redirect omitted its destination'
                    }
                    $NextUri = if ($Location.IsAbsoluteUri) {
                        $Location
                    } else {
                        [Uri]::new($CurrentUri, $Location)
                    }
                    if ($NextUri.Scheme -ine [Uri]::UriSchemeHttps) {
                        throw 'Release redirect attempted to leave HTTPS'
                    }
                    $CurrentUri = $NextUri
                    continue
                }
                if (-not $Response.IsSuccessStatusCode) {
                    throw "Release download failed with HTTP status $StatusCode"
                }

                $DeclaredLength = $Response.Content.Headers.ContentLength
                if ($null -ne $DeclaredLength -and [long]$DeclaredLength -gt $MaxBytes) {
                    throw 'Release download exceeds its declared size limit'
                }

                $InputStream = $null
                $OutputStream = $null
                try {
                    $InputStream = $Response.Content.ReadAsStreamAsync().GetAwaiter().GetResult()
                    $OutputStream = [IO.File]::Open(
                        $LiteralPath,
                        [IO.FileMode]::CreateNew,
                        [IO.FileAccess]::Write,
                        [IO.FileShare]::None
                    )
                    $Buffer = [byte[]]::new(81920)
                    [long]$Total = 0
                    while (($Read = $InputStream.Read($Buffer, 0, $Buffer.Length)) -gt 0) {
                        $Total += $Read
                        if ($Total -gt $MaxBytes) {
                            throw 'Release download exceeded its actual size limit'
                        }
                        $OutputStream.Write($Buffer, 0, $Read)
                    }
                    $OutputStream.Flush($true)
                } finally {
                    if ($null -ne $OutputStream) { $OutputStream.Dispose() }
                    if ($null -ne $InputStream) { $InputStream.Dispose() }
                }
                return
            } finally {
                if ($null -ne $Response) { $Response.Dispose() }
                $Request.Dispose()
            }
        }
        throw 'Release download exceeded the five-redirect limit'
    } finally {
        $Client.Dispose()
        $Handler.Dispose()
    }
}

function Expand-VerifiedBundleArchive {
    param(
        [Parameter(Mandatory = $true)][string]$ArchivePath,
        [Parameter(Mandatory = $true)][string]$DestinationPath
    )

    $DestinationItem = Get-Item -LiteralPath $DestinationPath -Force -ErrorAction Stop
    if (-not $DestinationItem.PSIsContainer -or
        (($DestinationItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw 'Bundle extraction destination must be a real directory'
    }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $ExpectedNames = @(
        'ipgw.exe',
        'ipgw-meta.exe',
        'ipgw-legacy.exe',
        'LICENSE',
        'launcher-default.yaml',
        'bundle-manifest.json',
        'SHA256SUMS'
    )
    $EntryLimits = @{
        'ipgw.exe'              = 64MB
        'ipgw-meta.exe'         = 64MB
        'ipgw-legacy.exe'       = 64MB
        'LICENSE'               = 4MB
        'launcher-default.yaml' = 64KB
        'bundle-manifest.json'  = 64KB
        'SHA256SUMS'            = 64KB
    }
    [long]$MaximumTotalBytes = (3 * 64MB) + 4MB + (3 * 64KB)
    $DestinationPrefix = [IO.Path]::GetFullPath($DestinationPath).TrimEnd('\') + '\'
    $EntriesByName = @{}
    [long]$DeclaredTotal = 0

    $Zip = [IO.Compression.ZipFile]::OpenRead($ArchivePath)
    try {
        foreach ($Entry in $Zip.Entries) {
            $Name = $Entry.FullName
            if ($ExpectedNames -cnotcontains $Name -or $Entry.Name -cne $Name) {
                throw 'Release archive contains a non-root or unexpected entry'
            }
            if ($EntriesByName.ContainsKey($Name)) {
                throw 'Release archive contains a duplicate entry'
            }

            $UnixMode = (([long]$Entry.ExternalAttributes -shr 16) -band 0xFFFF)
            $UnixType = ($UnixMode -band 0xF000)
            if ($UnixType -notin @(0, 0x8000)) {
                throw "Release archive entry is not a regular file: $Name"
            }
            $DOSAttributes = ([long]$Entry.ExternalAttributes -band 0xFFFF)
            if (($DOSAttributes -band [int][IO.FileAttributes]::Directory) -ne 0 -or
                ($DOSAttributes -band [int][IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Release archive entry has directory or reparse semantics: $Name"
            }

            [long]$EntryLimit = $EntryLimits[$Name]
            if ($Entry.Length -le 0 -or $Entry.Length -gt $EntryLimit) {
                throw "Release archive entry exceeds its declared size limit: $Name"
            }
            $DeclaredTotal += $Entry.Length
            if ($DeclaredTotal -gt $MaximumTotalBytes) {
                throw 'Release archive exceeds its total decompressed size limit'
            }
            $EntriesByName[$Name] = $Entry
        }
        if ($EntriesByName.Count -ne $ExpectedNames.Count) {
            throw 'Release archive does not contain exactly seven required entries'
        }

        [long]$ActualTotal = 0
        foreach ($Name in $ExpectedNames) {
            $Entry = $EntriesByName[$Name]
            [long]$EntryLimit = $EntryLimits[$Name]
            $TargetPath = Join-Path $DestinationPath $Name
            $TargetFullPath = [IO.Path]::GetFullPath($TargetPath)
            if (-not $TargetFullPath.StartsWith($DestinationPrefix, [StringComparison]::OrdinalIgnoreCase)) {
                throw 'Release archive entry escaped the extraction directory'
            }

            $InputStream = $null
            $OutputStream = $null
            [long]$EntryTotal = 0
            try {
                $InputStream = $Entry.Open()
                $OutputStream = [IO.File]::Open(
                    $TargetFullPath,
                    [IO.FileMode]::CreateNew,
                    [IO.FileAccess]::Write,
                    [IO.FileShare]::None
                )
                $Buffer = [byte[]]::new(81920)
                while (($Read = $InputStream.Read($Buffer, 0, $Buffer.Length)) -gt 0) {
                    $EntryTotal += $Read
                    $ActualTotal += $Read
                    if ($EntryTotal -gt $EntryLimit -or $ActualTotal -gt $MaximumTotalBytes) {
                        throw "Release archive entry exceeded its actual size limit: $Name"
                    }
                    $OutputStream.Write($Buffer, 0, $Read)
                }
                $OutputStream.Flush($true)
            } finally {
                if ($null -ne $OutputStream) { $OutputStream.Dispose() }
                if ($null -ne $InputStream) { $InputStream.Dispose() }
            }
            if ($EntryTotal -ne $Entry.Length) {
                throw "Release archive entry length changed during extraction: $Name"
            }
            $ExtractedItem = Get-Item -LiteralPath $TargetFullPath -Force -ErrorAction Stop
            if ($ExtractedItem.PSIsContainer -or
                (($ExtractedItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
                throw "Extracted bundle member is not a regular file: $Name"
            }
        }
        if ($ActualTotal -ne $DeclaredTotal) {
            throw 'Release archive decompressed size did not match its declaration'
        }
    } finally {
        $Zip.Dispose()
    }
}

$DownloadDir = Join-Path ([IO.Path]::GetTempPath()) ("ipgw-meta-download-" + [guid]::NewGuid().ToString('N'))
$ArchiveFile = Join-Path $DownloadDir $ArchiveName
$ChecksumsFile = Join-Path $DownloadDir 'SHA256SUMS'
$Stage = $null
$VersionDir = $null
$VersionInstalled = $false
$NextLink = $null
$BackupActive = $null
$LauncherTemp = $null
$LauncherInstalled = $false
$OldActiveMoved = $false
$ActiveSwitched = $false
$PathChanged = $false
$Committed = $false
$ManagedBinTarget = $null
$OldUserPath = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
$OldProcessPath = $env:Path

try {
    Ensure-RealDirectory -LiteralPath $InstallRoot
    $VersionsDir = Join-Path $InstallRoot 'versions'
    Ensure-RealDirectory -LiteralPath $VersionsDir
    Set-PrivateDirectoryAcl -LiteralPath $VersionsDir
    if ($BinDir.TrimEnd('\') -ieq $InstallRoot.TrimEnd('\') -or
        $BinDir.TrimEnd('\') -ieq $VersionsDir.TrimEnd('\')) {
        throw 'Binary directory must be distinct from install and versions directories'
    }
    $BinParent = Split-Path -Parent $BinDir
    if ([string]::IsNullOrWhiteSpace($BinParent)) {
        throw 'IPGW_BIN_DIR must have a parent directory'
    }
    Ensure-RealDirectory -LiteralPath $BinParent
    $ExistingBinItem = Get-Item -LiteralPath $BinDir -Force -ErrorAction SilentlyContinue
    if ($null -ne $ExistingBinItem) {
        $ManagedBinTarget = Get-ManagedJunctionTarget -Item $ExistingBinItem -VersionsPath $VersionsDir
    }

    New-Item -ItemType Directory -Path $DownloadDir | Out-Null
    Set-PrivateDirectoryAcl -LiteralPath $DownloadDir
    Invoke-HttpsDownload -Uri "$ReleaseBase/SHA256SUMS" -LiteralPath $ChecksumsFile -MaxBytes 1MB
    Invoke-HttpsDownload -Uri "$ReleaseBase/$ArchiveName" -LiteralPath $ArchiveFile -MaxBytes 100MB

    if ((Get-Item -LiteralPath $ChecksumsFile).Length -gt 1MB) {
        throw 'Release checksum file exceeds the 1 MiB limit'
    }
    if ((Get-Item -LiteralPath $ArchiveFile).Length -gt 100MB) {
        throw 'Release archive exceeds the 100 MiB limit'
    }

    $AllowedOuter = @(
        'install.sh',
        'install.ps1',
        'ipgw-meta-darwin-amd64.tar.gz',
        'ipgw-meta-darwin-arm64.tar.gz',
        'ipgw-meta-linux-amd64.tar.gz',
        'ipgw-meta-linux-arm64.tar.gz',
        'ipgw-meta-windows-amd64.zip',
        'ipgw-meta-windows-arm64.zip'
    )
    $SeenOuter = @{}
    $ExpectedArchiveHash = $null
    foreach ($Line in Get-Content -LiteralPath $ChecksumsFile) {
        if ($Line -cnotmatch '^([0-9A-Fa-f]{64})  ([0-9A-Za-z.-]+)$') {
            throw 'Invalid release checksum record'
        }
        $Hash = $Matches[1].ToLowerInvariant()
        $Name = $Matches[2]
        if ($AllowedOuter -cnotcontains $Name) {
            throw 'Unexpected release checksum target'
        }
        if ($SeenOuter.ContainsKey($Name)) {
            throw 'Duplicate release checksum target'
        }
        $SeenOuter[$Name] = $true
        if ($Name -ceq $ArchiveName) {
            $ExpectedArchiveHash = $Hash
        }
    }
    if ($SeenOuter.Count -ne $AllowedOuter.Count -or $null -eq $ExpectedArchiveHash) {
        throw 'Release checksum manifest is incomplete'
    }
    $ActualHash = (Get-FileHash -LiteralPath $ArchiveFile -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($ActualHash -cne $ExpectedArchiveHash) {
        throw 'Downloaded archive failed SHA-256 verification'
    }

    $HadInstall =
        (Test-Path -LiteralPath (Join-Path $BinDir 'ipgw.exe')) -or
        ($null -ne (Get-Command ipgw -ErrorAction SilentlyContinue))
    $ConfigBase = [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)
    if ([string]::IsNullOrWhiteSpace($ConfigBase)) {
        throw 'Unable to resolve the Windows user configuration directory'
    }
    $ConfigDir = Join-Path $ConfigBase 'ipgw-meta'
    $LauncherFile = Join-Path $ConfigDir 'launcher.yaml'
    $LegacyMetaConfig = Join-Path (Join-Path $ConfigBase 'ipgw') 'config.yaml'
    $LegacyUpstream = Join-Path $HOME '.ipgw'
    if ((Test-Path -LiteralPath $LegacyMetaConfig) -or (Test-Path -LiteralPath $LegacyUpstream)) {
        $HadInstall = $true
    }
    Ensure-RealDirectory -LiteralPath $ConfigDir
    $ExistingLauncherItem = Get-RealFileIfPresent -LiteralPath $LauncherFile

    $Stage = Join-Path $InstallRoot ('.staging-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $Stage | Out-Null
    Set-PrivateDirectoryAcl -LiteralPath $Stage
    Expand-VerifiedBundleArchive -ArchivePath $ArchiveFile -DestinationPath $Stage

    $AllowedInner = @(
        'ipgw.exe',
        'ipgw-meta.exe',
        'ipgw-legacy.exe',
        'LICENSE',
        'launcher-default.yaml',
        'bundle-manifest.json'
    )
    $Seen = @{}
    foreach ($Line in Get-Content -LiteralPath (Join-Path $Stage 'SHA256SUMS')) {
        if ($Line -cnotmatch '^([0-9A-Fa-f]{64})  ([0-9A-Za-z.-]+)$') {
            throw 'Invalid internal checksum record'
        }
        $Expected = $Matches[1].ToLowerInvariant()
        $Name = $Matches[2]
        if ($AllowedInner -cnotcontains $Name -or $Seen.ContainsKey($Name)) {
            throw 'Unexpected or duplicate internal checksum target'
        }
        $Member = Join-Path $Stage $Name
        $Item = Get-Item -LiteralPath $Member -Force -ErrorAction Stop
        if ($Item.PSIsContainer -or (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
            throw "Bundle member is not a regular file: $Name"
        }
        $MemberHash = (Get-FileHash -LiteralPath $Member -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($MemberHash -cne $Expected) {
            throw "Bundle member failed SHA-256 verification: $Name"
        }
        $Seen[$Name] = $true
    }
    if ($Seen.Count -ne $AllowedInner.Count) {
        throw 'Bundle checksum manifest is incomplete'
    }

    $ManifestPath = Join-Path $Stage 'bundle-manifest.json'
    $ManifestRaw = [IO.File]::ReadAllText($ManifestPath)
    try {
        $Manifest = $ManifestRaw | ConvertFrom-Json -ErrorAction Stop
    } catch {
        throw 'Bundle manifest is not valid JSON'
    }
    $ExpectedRootProperties = @(
        'schema_version',
        'product',
        'module',
        'version',
        'platform',
        'entries',
        'launcher_default',
        'layout',
        'self_update',
        'uninstall'
    )
    $ActualRootProperties = @($Manifest.PSObject.Properties | ForEach-Object { $_.Name })
    $RootDifference = @(Compare-Object -CaseSensitive -ReferenceObject $ExpectedRootProperties -DifferenceObject $ActualRootProperties)
    if ($ActualRootProperties.Count -ne $ExpectedRootProperties.Count -or $RootDifference.Count -ne 0) {
        throw 'Bundle manifest has missing, duplicate, or unknown root properties'
    }
    if ($Manifest.schema_version -ne 1 -or
        $Manifest.product -cne 'ipgw-meta' -or
        $Manifest.module -cne 'github.com/UnbalancedCat/ipgw-meta' -or
        $Manifest.version -isnot [string] -or
        $Manifest.version -cnotmatch '^[0-9A-Za-z._+-]+$' -or
        $Manifest.platform -cne $Target -or
        $Manifest.launcher_default -cne 'meta' -or
        $Manifest.layout -cne 'versioned-bundle-v1' -or
        $Manifest.self_update -isnot [bool] -or
        $Manifest.self_update -ne $false) {
        throw 'Bundle manifest is incompatible with this installer'
    }
    if (-not [string]::IsNullOrWhiteSpace($Version) -and $Manifest.version -cne $Version) {
        throw 'Bundle manifest version does not match the pinned installer'
    }

    $ExpectedUninstallProperties = @('remove_all_three_entries', 'preserve_user_config')
    $ActualUninstallProperties = @($Manifest.uninstall.PSObject.Properties | ForEach-Object { $_.Name })
    $UninstallDifference = @(Compare-Object -CaseSensitive -ReferenceObject $ExpectedUninstallProperties -DifferenceObject $ActualUninstallProperties)
    if ($ActualUninstallProperties.Count -ne $ExpectedUninstallProperties.Count -or
        $UninstallDifference.Count -ne 0 -or
        $Manifest.uninstall.remove_all_three_entries -isnot [bool] -or
        $Manifest.uninstall.remove_all_three_entries -ne $true -or
        $Manifest.uninstall.preserve_user_config -isnot [bool] -or
        $Manifest.uninstall.preserve_user_config -ne $true) {
        throw 'Bundle manifest uninstall metadata is invalid'
    }

    $ManifestEntries = @($Manifest.entries)
    $ExpectedBinaries = @('ipgw.exe', 'ipgw-meta.exe', 'ipgw-legacy.exe')
    if ($ManifestEntries.Count -ne $ExpectedBinaries.Count) {
        throw 'Bundle manifest does not describe all three entry points'
    }

    $ExpectedEntryProperties = @('path', 'sha256', 'size')
    $EntryHashes = @()
    $EntrySizes = @()
    for ($Index = 0; $Index -lt $ExpectedBinaries.Count; $Index++) {
        $Entry = $ManifestEntries[$Index]
        $EntryProperties = @($Entry.PSObject.Properties | ForEach-Object { $_.Name })
        $EntryDifference = @(Compare-Object -CaseSensitive -ReferenceObject $ExpectedEntryProperties -DifferenceObject $EntryProperties)
        if ($EntryProperties.Count -ne $ExpectedEntryProperties.Count -or $EntryDifference.Count -ne 0) {
            throw 'Bundle manifest entry has missing, duplicate, or unknown properties'
        }
        $ExpectedPath = $ExpectedBinaries[$Index]
        $EntryFile = Join-Path $Stage $ExpectedPath
        $ActualEntryHash = (Get-FileHash -LiteralPath $EntryFile -Algorithm SHA256).Hash.ToLowerInvariant()
        [long]$ActualEntrySize = (Get-Item -LiteralPath $EntryFile -Force).Length
        if ($Entry.path -isnot [string] -or $Entry.path -cne $ExpectedPath -or
            $Entry.sha256 -isnot [string] -or $Entry.sha256 -cnotmatch '^[0-9a-f]{64}$' -or
            $Entry.sha256 -cne $ActualEntryHash -or
            ($Entry.size -isnot [int] -and $Entry.size -isnot [long]) -or
            [long]$Entry.size -ne $ActualEntrySize) {
            throw "Bundle manifest entry does not bind the actual path, hash, and size: $ExpectedPath"
        }
        $EntryHashes += $ActualEntryHash
        $EntrySizes += $ActualEntrySize
    }

    $CanonicalManifestLines = @(
        '{',
        '  "schema_version": 1,',
        '  "product": "ipgw-meta",',
        '  "module": "github.com/UnbalancedCat/ipgw-meta",',
        ('  "version": "' + $Manifest.version + '",'),
        ('  "platform": "' + $Target + '",'),
        '  "entries": [',
        ('    {"path": "ipgw.exe", "sha256": "' + $EntryHashes[0] + '", "size": ' + $EntrySizes[0] + '},'),
        ('    {"path": "ipgw-meta.exe", "sha256": "' + $EntryHashes[1] + '", "size": ' + $EntrySizes[1] + '},'),
        ('    {"path": "ipgw-legacy.exe", "sha256": "' + $EntryHashes[2] + '", "size": ' + $EntrySizes[2] + '}'),
        '  ],',
        '  "launcher_default": "meta",',
        '  "layout": "versioned-bundle-v1",',
        '  "self_update": false,',
        '  "uninstall": {"remove_all_three_entries": true, "preserve_user_config": true}',
        '}'
    )
    $CanonicalManifest = ($CanonicalManifestLines -join "`n") + "`n"
    if (-not [string]::Equals($ManifestRaw, $CanonicalManifest, [StringComparison]::Ordinal)) {
        throw 'Bundle manifest is not in the canonical v1 format'
    }

    $ModeMetadata = [IO.File]::ReadAllText((Join-Path $Stage 'launcher-default.yaml'))
    if (-not [string]::Equals(
        $ModeMetadata,
        "schema_version: 1`nmode: meta`ncohort: new-install`n",
        [StringComparison]::Ordinal
    )) {
        throw 'Bundle launcher metadata is invalid'
    }

    if ($null -eq $ExistingLauncherItem) {
        Set-PrivateDirectoryAcl -LiteralPath $ConfigDir
        $Mode = if ($HadInstall) { 'legacy' } else { 'meta' }
        $Cohort = if ($HadInstall) { 'existing-install' } else { 'new-install' }
        $LauncherTemp = Join-Path $ConfigDir ('.launcher-' + [guid]::NewGuid().ToString('N'))
        $ChosenAt = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
        $LauncherContent = "schema_version: 1`nmode: $Mode`ncohort: $Cohort`nchosen_at: $ChosenAt`n"
        Write-PrivateFile -LiteralPath $LauncherTemp -Content $LauncherContent
    }

    $VersionID = $ActualHash.Substring(0, 16) + '-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
    $VersionDir = Join-Path $VersionsDir $VersionID
    Move-Item -LiteralPath $Stage -Destination $VersionDir
    $Stage = $null
    $VersionInstalled = $true

    $CurrentActive = Get-Item -LiteralPath $BinDir -Force -ErrorAction SilentlyContinue
    if ($null -eq $ManagedBinTarget -and $null -ne $CurrentActive) {
        throw 'Binary path appeared during installation; refusing to replace it'
    }
    if ($null -ne $ManagedBinTarget) {
        if ($null -eq $CurrentActive) {
            throw 'Managed binary junction disappeared during installation'
        }
        $CurrentManagedTarget = Get-ManagedJunctionTarget -Item $CurrentActive -VersionsPath $VersionsDir
        if (-not [string]::Equals($ManagedBinTarget, $CurrentManagedTarget, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Managed binary junction target changed during installation'
        }
    }

    $NextLink = Join-Path $BinParent ('.ipgw-active-next-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Junction -Path $NextLink -Target $VersionDir | Out-Null

    if ($null -ne $CurrentActive) {
        $BackupActive = Join-Path $BinParent ('.ipgw-active-backup-' + [guid]::NewGuid().ToString('N'))
        Move-Item -LiteralPath $BinDir -Destination $BackupActive
        $OldActiveMoved = $true
    }
    Move-Item -LiteralPath $NextLink -Destination $BinDir
    $NextLink = $null
    $ActiveSwitched = $true

    $CurrentLauncherItem = Get-RealFileIfPresent -LiteralPath $LauncherFile
    if ($null -ne $LauncherTemp) {
        if ($null -ne $CurrentLauncherItem) {
            Remove-ExactPath -LiteralPath $LauncherTemp
        } else {
            Move-Item -LiteralPath $LauncherTemp -Destination $LauncherFile
            $LauncherInstalled = $true
        }
        $LauncherTemp = $null
    }

    $PathParts = @($OldUserPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if (-not ($PathParts | Where-Object { $_.TrimEnd('\') -ieq $BinDir.TrimEnd('\') })) {
        $NewUserPath = if ([string]::IsNullOrWhiteSpace($OldUserPath)) { $BinDir } else { "$OldUserPath;$BinDir" }
        [Environment]::SetEnvironmentVariable('Path', $NewUserPath, [EnvironmentVariableTarget]::User)
        $env:Path = if ([string]::IsNullOrWhiteSpace($env:Path)) { $BinDir } else { "$env:Path;$BinDir" }
        $PathChanged = $true
    }

    $Committed = $true
    Write-Output "IPGW-Meta installed as one three-entry bundle in $VersionDir"
    Write-Output 'Launcher mode was preserved for existing installs; new installs default to meta.'
    if ($null -ne $BackupActive) {
        Write-Output "The previous complete install is preserved at $BackupActive"
    }
} catch {
    $OriginalError = $_
    $RollbackErrors = @()
    if ($PathChanged) {
        try {
            [Environment]::SetEnvironmentVariable('Path', $OldUserPath, [EnvironmentVariableTarget]::User)
            $env:Path = $OldProcessPath
        } catch { $RollbackErrors += $_.Exception.Message }
    }
    if ($LauncherInstalled) {
        try { Remove-ExactPath -LiteralPath $LauncherFile } catch { $RollbackErrors += $_.Exception.Message }
    }
    if ($ActiveSwitched) {
        try { Remove-ExactPath -LiteralPath $BinDir } catch { $RollbackErrors += $_.Exception.Message }
    }
    if ($OldActiveMoved -and $null -ne $BackupActive -and (Test-Path -LiteralPath $BackupActive)) {
        try { Move-Item -LiteralPath $BackupActive -Destination $BinDir } catch { $RollbackErrors += $_.Exception.Message }
    }
    if ($VersionInstalled -and $null -ne $VersionDir) {
        try {
            $VersionFullPath = [IO.Path]::GetFullPath($VersionDir)
            $VersionsPrefix = [IO.Path]::GetFullPath($VersionsDir).TrimEnd('\') + '\'
            $VersionLeaf = Split-Path -Leaf $VersionFullPath
            $VersionItem = Get-Item -LiteralPath $VersionFullPath -Force -ErrorAction Stop
            if (-not $VersionFullPath.StartsWith($VersionsPrefix, [StringComparison]::OrdinalIgnoreCase) -or
                $VersionLeaf -cnotmatch '^[0-9a-f]{16}-[0-9]{14}-[0-9a-f]{8}$' -or
                -not $VersionItem.PSIsContainer -or
                (($VersionItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
                throw 'Refusing to remove an unexpected rollback version path'
            }
            Remove-ExactPath -LiteralPath $VersionFullPath
            $VersionInstalled = $false
        } catch { $RollbackErrors += $_.Exception.Message }
    }
    if ($RollbackErrors.Count -gt 0) {
        Write-Warning ('Rollback was incomplete: ' + ($RollbackErrors -join '; '))
        if ($null -ne $BackupActive) { Write-Warning "Recovery data was preserved at $BackupActive" }
    }
    throw $OriginalError
} finally {
    if ($null -ne $NextLink -and (Test-Path -LiteralPath $NextLink)) {
        Remove-ExactPath -LiteralPath $NextLink
    }
    if ($null -ne $LauncherTemp -and (Test-Path -LiteralPath $LauncherTemp)) {
        Remove-ExactPath -LiteralPath $LauncherTemp
    }
    if ($null -ne $Stage -and (Test-Path -LiteralPath $Stage)) {
        Remove-ExactPath -LiteralPath $Stage
    }
    if (Test-Path -LiteralPath $DownloadDir) {
        Remove-ExactPath -LiteralPath $DownloadDir
    }
}
