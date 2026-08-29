[CmdletBinding()]
param(
    [string]$BundlePath,
    [string]$BundleSha256,
    [string]$Version = $env:IPGW_VERSION,
    [string]$InstallRoot = $env:IPGW_INSTALL_ROOT,
    [string]$BinDir = $env:IPGW_BIN_DIR
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
Set-StrictMode -Version Latest

$MaximumArchiveBytes = 100MB
$MaximumChecksumBytes = 1MB
$MaximumExpandedBytes = (3 * 64MB) + 4MB + (3 * 64KB)
$MaximumCompressionRatio = 200
$EntryNames = @('ipgw.exe', 'ipgw-meta.exe', 'ipgw-legacy.exe')
$ArchiveNames = @(
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
$AllowedForwardFailpoints = @(
    'after_verified_stage',
    'after_version_publish',
    'after_old_active_detach',
    'after_active_switch',
    'after_entry_1',
    'after_entry_2',
    'after_launcher_publish',
    'after_path_update',
    'before_commit'
)
$AllowedRollbackFailpoints = @(
    'before_restore_active',
    'before_restore_entry_1',
    'before_remove_new_version'
)
$CurrentUserSid = [Security.Principal.WindowsIdentity]::GetCurrent().User
$SystemSid = New-Object Security.Principal.SecurityIdentifier('S-1-5-18')
$AdministratorsSid = New-Object Security.Principal.SecurityIdentifier('S-1-5-32-544')
$AllowedPrivateSids = @($CurrentUserSid.Value, $SystemSid.Value, $AdministratorsSid.Value)

function Test-StringPresent {
    param([AllowNull()][string]$Value)
    return -not [string]::IsNullOrWhiteSpace($Value)
}

function Get-LocalFixedPath {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$Label
    )
    if ([string]::IsNullOrWhiteSpace($LiteralPath) -or
        $LiteralPath -match '[\x00-\x1F]' -or
        $LiteralPath.StartsWith('\\') -or
        $LiteralPath.StartsWith('//') -or
        $LiteralPath -notmatch '^[A-Za-z]:[\\/]') {
        throw "$Label must be an absolute path on a local Windows volume"
    }
    $RawComponents = @($LiteralPath.Substring(3) -split '[\\/]')
    foreach ($Component in $RawComponents) {
        if ($Component -ceq '.' -or $Component -ceq '..' -or $Component.Contains(':')) {
            throw "$Label contains an unsafe path component"
        }
    }
    $FullPath = [IO.Path]::GetFullPath($LiteralPath)
    $Root = [IO.Path]::GetPathRoot($FullPath)
    if ([string]::IsNullOrWhiteSpace($Root) -or
        $FullPath.TrimEnd([char[]]'\/') -ieq $Root.TrimEnd([char[]]'\/')) {
        throw "$Label must not be a volume root"
    }
    try {
        $Drive = New-Object IO.DriveInfo($Root)
    } catch {
        throw "$Label does not identify a Windows disk volume"
    }
    if ($Drive.DriveType -ne [IO.DriveType]::Fixed) {
        throw "$Label must be on a fixed local disk"
    }
    return $FullPath.TrimEnd([char[]]'\/')
}

function Test-PathWithin {
    param(
        [Parameter(Mandatory = $true)][string]$Child,
        [Parameter(Mandatory = $true)][string]$Parent
    )
    $Prefix = $Parent.TrimEnd([char[]]'\/') + '\'
    return -not [string]::Equals($Child, $Parent, [StringComparison]::OrdinalIgnoreCase) -and
        $Child.StartsWith($Prefix, [StringComparison]::OrdinalIgnoreCase)
}

function Test-PathsOverlap {
    param(
        [Parameter(Mandatory = $true)][string]$Left,
        [Parameter(Mandatory = $true)][string]$Right
    )
    return [string]::Equals($Left, $Right, [StringComparison]::OrdinalIgnoreCase) -or
        (Test-PathWithin -Child $Left -Parent $Right) -or
        (Test-PathWithin -Child $Right -Parent $Left)
}

function Assert-DirectoryAncestors {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$Label
    )
    $FullPath = [IO.Path]::GetFullPath($LiteralPath)
    $Root = [IO.Path]::GetPathRoot($FullPath)
    $Current = $Root.TrimEnd([char[]]'\/')
    $Relative = $FullPath.Substring($Root.Length)
    foreach ($Component in @($Relative -split '[\\/]')) {
        if ([string]::IsNullOrEmpty($Component)) { continue }
        $Current = Join-Path $Current $Component
        $Item = Get-Item -LiteralPath $Current -Force -ErrorAction SilentlyContinue
        if ($null -eq $Item) { continue }
        if (-not $Item.PSIsContainer -or
            (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
            throw "$Label contains a non-directory or reparse-point component"
        }
    }
}

function New-PrivateDirectorySecurity {
    $Acl = New-Object Security.AccessControl.DirectorySecurity
    $Acl.SetOwner($CurrentUserSid)
    $Acl.SetAccessRuleProtection($true, $false)
    $Inheritance = [Security.AccessControl.InheritanceFlags]::ContainerInherit -bor
        [Security.AccessControl.InheritanceFlags]::ObjectInherit
    foreach ($Sid in @($CurrentUserSid, $SystemSid, $AdministratorsSid)) {
        $Rule = New-Object Security.AccessControl.FileSystemAccessRule(
            $Sid,
            [Security.AccessControl.FileSystemRights]::FullControl,
            $Inheritance,
            [Security.AccessControl.PropagationFlags]::None,
            [Security.AccessControl.AccessControlType]::Allow
        )
        [void]$Acl.AddAccessRule($Rule)
    }
    return $Acl
}

function New-PrivateFileSecurity {
    $Acl = New-Object Security.AccessControl.FileSecurity
    $Acl.SetOwner($CurrentUserSid)
    $Acl.SetAccessRuleProtection($true, $false)
    foreach ($Sid in @($CurrentUserSid, $SystemSid, $AdministratorsSid)) {
        $Rule = New-Object Security.AccessControl.FileSystemAccessRule(
            $Sid,
            [Security.AccessControl.FileSystemRights]::FullControl,
            [Security.AccessControl.AccessControlType]::Allow
        )
        [void]$Acl.AddAccessRule($Rule)
    }
    return $Acl
}

function Set-PrivateDirectoryAcl {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    [IO.Directory]::SetAccessControl($LiteralPath, (New-PrivateDirectorySecurity))
}

function Set-PrivateFileAcl {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    [IO.File]::SetAccessControl($LiteralPath, (New-PrivateFileSecurity))
}

function Assert-PrivateDirectory {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$Label
    )
    $Item = Get-Item -LiteralPath $LiteralPath -Force -ErrorAction Stop
    if (-not $Item.PSIsContainer -or
        (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw "$Label must be a real directory"
    }
    $Acl = Get-Acl -LiteralPath $LiteralPath
    $Owner = $Acl.GetOwner([Security.Principal.SecurityIdentifier]).Value
    if ($Owner -cne $CurrentUserSid.Value -or -not $Acl.AreAccessRulesProtected) {
        throw "$Label must be owned by the current user with protected ACL inheritance"
    }
    foreach ($Rule in $Acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])) {
        if ($Rule.AccessControlType -eq [Security.AccessControl.AccessControlType]::Allow -and
            $AllowedPrivateSids -cnotcontains $Rule.IdentityReference.Value) {
            throw "$Label grants access to an unexpected identity"
        }
    }
}

function Assert-RegularFile {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$Label
    )
    $Item = Get-Item -LiteralPath $LiteralPath -Force -ErrorAction Stop
    if ($Item.PSIsContainer -or
        (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw "$Label must be a non-reparse regular file"
    }
    return $Item
}

function Assert-PrivateFileAcl {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$Label
    )
    [void](Assert-RegularFile -LiteralPath $LiteralPath -Label $Label)
    $Acl = Get-Acl -LiteralPath $LiteralPath
    foreach ($Rule in $Acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])) {
        if ($Rule.AccessControlType -eq [Security.AccessControl.AccessControlType]::Allow -and
            $AllowedPrivateSids -cnotcontains $Rule.IdentityReference.Value) {
            throw "$Label grants access to an unexpected identity"
        }
    }
}

function Assert-UntrustedPrincipalsCannotWrite {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    $Untrusted = @('S-1-1-0', 'S-1-5-11', 'S-1-5-32-545')
    $WriteMask = [Security.AccessControl.FileSystemRights]::Write -bor
        [Security.AccessControl.FileSystemRights]::Modify -bor
        [Security.AccessControl.FileSystemRights]::FullControl -bor
        [Security.AccessControl.FileSystemRights]::Delete -bor
        [Security.AccessControl.FileSystemRights]::ChangePermissions -bor
        [Security.AccessControl.FileSystemRights]::TakeOwnership
    $Acl = Get-Acl -LiteralPath $LiteralPath
    foreach ($Rule in $Acl.GetAccessRules($true, $true, [Security.Principal.SecurityIdentifier])) {
        if ($Rule.AccessControlType -eq [Security.AccessControl.AccessControlType]::Allow -and
            $Untrusted -ccontains $Rule.IdentityReference.Value -and
            (($Rule.FileSystemRights -band $WriteMask) -ne 0)) {
            throw 'Offline bundle must not be writable by Users, Authenticated Users, or Everyone'
        }
    }
}

function Ensure-PrivateDirectory {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    Assert-DirectoryAncestors -LiteralPath $LiteralPath -Label 'directory target'
    $FullPath = [IO.Path]::GetFullPath($LiteralPath)
    $Root = [IO.Path]::GetPathRoot($FullPath)
    $Current = $Root.TrimEnd([char[]]'\/')
    $Relative = $FullPath.Substring($Root.Length)
    foreach ($Component in @($Relative -split '[\\/]')) {
        if ([string]::IsNullOrEmpty($Component)) { continue }
        $Current = Join-Path $Current $Component
        $Item = Get-Item -LiteralPath $Current -Force -ErrorAction SilentlyContinue
        if ($null -eq $Item) {
            [void](New-Item -ItemType Directory -Path $Current)
            Set-PrivateDirectoryAcl -LiteralPath $Current
            $Item = Get-Item -LiteralPath $Current -Force -ErrorAction Stop
        }
        if (-not $Item.PSIsContainer -or
            (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
            throw "Refusing a non-directory or reparse-point target: $Current"
        }
    }
    Set-PrivateDirectoryAcl -LiteralPath $FullPath
    Assert-PrivateDirectory -LiteralPath $FullPath -Label 'private directory'
}

function Remove-SafeTree {
    param([Parameter(Mandatory = $true)][string]$LiteralPath)
    $Item = Get-Item -LiteralPath $LiteralPath -Force -ErrorAction SilentlyContinue
    if ($null -eq $Item) { return }
    if (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        if ($Item.PSIsContainer) {
            [IO.Directory]::Delete($LiteralPath)
        } else {
            [IO.File]::Delete($LiteralPath)
        }
        return
    }
    if (-not $Item.PSIsContainer) {
        [IO.File]::Delete($LiteralPath)
        return
    }
    foreach ($Child in @(Get-ChildItem -LiteralPath $LiteralPath -Force)) {
        Remove-SafeTree -LiteralPath $Child.FullName
    }
    [IO.Directory]::Delete($LiteralPath)
}

function Write-PrivateFile {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [AllowEmptyString()][Parameter(Mandatory = $true)][string]$Content
    )
    $Encoding = New-Object Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($LiteralPath, $Content, $Encoding)
    Set-PrivateFileAcl -LiteralPath $LiteralPath
    $Stream = [IO.File]::Open($LiteralPath, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    try { $Stream.Flush($true) } finally { $Stream.Dispose() }
}

function Test-FilesMatch {
    param(
        [Parameter(Mandatory = $true)][string]$Left,
        [Parameter(Mandatory = $true)][string]$Right
    )
    $LeftItem = Assert-RegularFile -LiteralPath $Left -Label 'managed entry'
    $RightItem = Assert-RegularFile -LiteralPath $Right -Label 'version entry'
    if ($LeftItem.Length -ne $RightItem.Length) { return $false }
    $LeftHash = (Get-FileHash -LiteralPath $Left -Algorithm SHA256).Hash
    $RightHash = (Get-FileHash -LiteralPath $Right -Algorithm SHA256).Hash
    return [string]::Equals($LeftHash, $RightHash, [StringComparison]::OrdinalIgnoreCase)
}

function Get-ManagedJunctionTarget {
    param(
        [Parameter(Mandatory = $true)][string]$LiteralPath,
        [Parameter(Mandatory = $true)][string]$VersionsPath,
        [Parameter(Mandatory = $true)][string]$Label
    )
    $Item = Get-Item -LiteralPath $LiteralPath -Force -ErrorAction Stop
    if (-not $Item.PSIsContainer -or
        (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -eq 0) -or
        $null -eq $Item.PSObject.Properties['LinkType'] -or
        $Item.LinkType -cne 'Junction' -or
        $null -eq $Item.PSObject.Properties['Target']) {
        throw "$Label is not a managed junction"
    }
    $Targets = @($Item.Target)
    if ($Targets.Count -ne 1 -or -not [IO.Path]::IsPathRooted([string]$Targets[0])) {
        throw "$Label has an invalid target"
    }
    $TargetPath = [IO.Path]::GetFullPath([string]$Targets[0]).TrimEnd([char[]]'\/')
    $VersionsPrefix = [IO.Path]::GetFullPath($VersionsPath).TrimEnd([char[]]'\/') + '\'
    if (-not $TargetPath.StartsWith($VersionsPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw "$Label targets a path outside the managed versions directory"
    }
    $Leaf = $TargetPath.Substring($VersionsPrefix.Length)
    if ($Leaf -cnotmatch '^[0-9a-f]{16}-[0-9]{14}-[0-9a-f]{8}$' -or $Leaf.Contains('\')) {
        throw "$Label does not target one managed version directory"
    }
    $TargetItem = Get-Item -LiteralPath $TargetPath -Force -ErrorAction Stop
    if (-not $TargetItem.PSIsContainer -or
        (($TargetItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw "$Label target is not a real version directory"
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
    Add-Type -AssemblyName System.Net.Http
    $Handler = New-Object System.Net.Http.HttpClientHandler
    $Handler.AllowAutoRedirect = $false
    $Client = New-Object System.Net.Http.HttpClient($Handler)
    $Client.Timeout = [TimeSpan]::FromMinutes(5)
    try {
        $CurrentUri = $Uri
        for ($RedirectCount = 0; $RedirectCount -le 5; $RedirectCount++) {
            $Request = New-Object System.Net.Http.HttpRequestMessage([System.Net.Http.HttpMethod]::Get, $CurrentUri)
            $Response = $null
            try {
                $Response = $Client.SendAsync($Request, [System.Net.Http.HttpCompletionOption]::ResponseHeadersRead).GetAwaiter().GetResult()
                $StatusCode = [int]$Response.StatusCode
                if ($StatusCode -in @(301, 302, 303, 307, 308)) {
                    if ($RedirectCount -ge 5 -or $null -eq $Response.Headers.Location) {
                        throw 'Release download exceeded its redirect policy'
                    }
                    $NextUri = if ($Response.Headers.Location.IsAbsoluteUri) {
                        $Response.Headers.Location
                    } else {
                        [Uri]::new($CurrentUri, $Response.Headers.Location)
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
                    $OutputStream = [IO.File]::Open($LiteralPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
                    $Buffer = [byte[]]::new(81920)
                    [long]$Total = 0
                    while (($Read = $InputStream.Read($Buffer, 0, $Buffer.Length)) -gt 0) {
                        $Total += $Read
                        if ($Total -gt $MaxBytes) { throw 'Release download exceeded its actual size limit' }
                        $OutputStream.Write($Buffer, 0, $Read)
                    }
                    $OutputStream.Flush($true)
                } finally {
                    if ($null -ne $OutputStream) { $OutputStream.Dispose() }
                    if ($null -ne $InputStream) { $InputStream.Dispose() }
                }
                Set-PrivateFileAcl -LiteralPath $LiteralPath
                return
            } finally {
                if ($null -ne $Response) { $Response.Dispose() }
                $Request.Dispose()
            }
        }
    } finally {
        $Client.Dispose()
        $Handler.Dispose()
    }
    throw 'Release download exceeded the five-redirect limit'
}

function Expand-VerifiedBundleArchive {
    param(
        [Parameter(Mandatory = $true)][string]$ArchivePath,
        [Parameter(Mandatory = $true)][string]$DestinationPath
    )
    [void](Assert-RegularFile -LiteralPath $ArchivePath -Label 'acquired archive')
    Assert-PrivateDirectory -LiteralPath $DestinationPath -Label 'bundle extraction destination'
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $EntriesByName = @{}
    [long]$DeclaredTotal = 0
    [long]$ArchiveLength = (Get-Item -LiteralPath $ArchivePath -Force).Length
    $Zip = [IO.Compression.ZipFile]::OpenRead($ArchivePath)
    try {
        foreach ($Entry in $Zip.Entries) {
            $Name = $Entry.FullName
            if ($ArchiveNames -cnotcontains $Name -or $Entry.Name -cne $Name) {
                throw 'Release archive contains a non-root or unexpected entry'
            }
            if ($EntriesByName.ContainsKey($Name)) { throw 'Release archive contains a duplicate or colliding entry' }
            $UnixMode = (([long]$Entry.ExternalAttributes -shr 16) -band 0xFFFF)
            $UnixType = ($UnixMode -band 0xF000)
            if ($UnixType -notin @(0, 0x8000)) { throw "Release archive entry is not a regular file: $Name" }
            $DOSAttributes = ([long]$Entry.ExternalAttributes -band 0xFFFF)
            if (($DOSAttributes -band [int][IO.FileAttributes]::Directory) -ne 0 -or
                ($DOSAttributes -band [int][IO.FileAttributes]::ReparsePoint) -ne 0) {
                throw "Release archive entry has directory or reparse semantics: $Name"
            }
            [long]$Limit = $EntryLimits[$Name]
            if ($Entry.Length -le 0 -or $Entry.Length -gt $Limit) {
                throw "Release archive entry exceeds its declared size limit: $Name"
            }
            $DeclaredTotal += $Entry.Length
            if ($DeclaredTotal -gt $MaximumExpandedBytes) { throw 'Release archive exceeds its total decompressed size limit' }
            $EntriesByName[$Name] = $Entry
        }
        if ($EntriesByName.Count -ne $ArchiveNames.Count) {
            throw 'Release archive does not contain exactly seven required entries'
        }
        if ($DeclaredTotal -gt ($ArchiveLength * $MaximumCompressionRatio)) {
            throw 'Release archive exceeds the maximum compression ratio'
        }
        [long]$ActualTotal = 0
        $DestinationPrefix = [IO.Path]::GetFullPath($DestinationPath).TrimEnd([char[]]'\/') + '\'
        foreach ($Name in $ArchiveNames) {
            $Entry = $EntriesByName[$Name]
            [long]$Limit = $EntryLimits[$Name]
            $TargetPath = [IO.Path]::GetFullPath((Join-Path $DestinationPath $Name))
            if (-not $TargetPath.StartsWith($DestinationPrefix, [StringComparison]::OrdinalIgnoreCase)) {
                throw 'Release archive entry escaped the extraction directory'
            }
            $InputStream = $null
            $OutputStream = $null
            [long]$EntryTotal = 0
            try {
                $InputStream = $Entry.Open()
                $OutputStream = [IO.File]::Open($TargetPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
                $Buffer = [byte[]]::new(81920)
                while (($Read = $InputStream.Read($Buffer, 0, $Buffer.Length)) -gt 0) {
                    $EntryTotal += $Read
                    $ActualTotal += $Read
                    if ($EntryTotal -gt $Limit -or $ActualTotal -gt $MaximumExpandedBytes) {
                        throw "Release archive entry exceeded its actual size limit: $Name"
                    }
                    $OutputStream.Write($Buffer, 0, $Read)
                }
                $OutputStream.Flush($true)
            } finally {
                if ($null -ne $OutputStream) { $OutputStream.Dispose() }
                if ($null -ne $InputStream) { $InputStream.Dispose() }
            }
            if ($EntryTotal -ne $Entry.Length) { throw "Release archive entry length changed during extraction: $Name" }
            Set-PrivateFileAcl -LiteralPath $TargetPath
            [void](Assert-RegularFile -LiteralPath $TargetPath -Label "extracted bundle member $Name")
        }
        if ($ActualTotal -ne $DeclaredTotal -or $ActualTotal -gt ($ArchiveLength * $MaximumCompressionRatio)) {
            throw 'Release archive decompressed size did not match its bounded declaration'
        }
    } finally {
        $Zip.Dispose()
    }
}

function Invoke-MaybeFail {
    param([Parameter(Mandatory = $true)][string]$Point)
    if ($script:TestMode -and $script:TestFailpoint -ceq $Point) {
        throw "installer test failpoint triggered: $Point"
    }
}

function Test-RollbackFailpoint {
    param([Parameter(Mandatory = $true)][string]$Point)
    return $script:TestMode -and $script:TestRollbackFailpoint -ceq $Point
}

function Write-Journal {
    param([Parameter(Mandatory = $true)][string]$Phase)
    $VersionName = if ($null -eq $script:VersionDir) { '' } else { Split-Path -Leaf $script:VersionDir }
    $BackupName = if ($null -eq $script:BackupDir) { '' } else { Split-Path -Leaf $script:BackupDir }
    $Content = "schema_version=1`nphase=$Phase`nversion_name=$VersionName`nhad_active=$([int]$script:HadActive)`nentry_count=$($script:InstalledEntries.Count)`nbackup_name=$BackupName`n"
    $Next = Join-Path $script:TransactionDir ('.journal.next.' + [guid]::NewGuid().ToString('N'))
    Write-PrivateFile -LiteralPath $Next -Content $Content
    if (Test-Path -LiteralPath $script:JournalFile) {
        $Previous = Join-Path $script:TransactionDir ('.journal.previous.' + [guid]::NewGuid().ToString('N'))
        [IO.File]::Replace($Next, $script:JournalFile, $Previous)
        Remove-Item -LiteralPath $Previous -Force
    } else {
        Move-Item -LiteralPath $Next -Destination $script:JournalFile
    }
}

function Test-JournalCommitted {
    if ($null -eq $script:JournalFile) { return $false }
    $Item = Get-Item -LiteralPath $script:JournalFile -Force -ErrorAction SilentlyContinue
    if ($null -eq $Item -or $Item.PSIsContainer -or
        (($Item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or $Item.Length -gt 1024) {
        return $false
    }
    return ([IO.File]::ReadAllText($script:JournalFile) -split "`n") -ccontains 'phase=committed'
}

$HasBundlePath = Test-StringPresent $BundlePath
$HasBundleSha = Test-StringPresent $BundleSha256
if ($HasBundlePath -xor $HasBundleSha) {
    throw '-BundlePath and -BundleSha256 must be provided together'
}
$Offline = $HasBundlePath -and $HasBundleSha
if ($HasBundleSha -and $BundleSha256 -cnotmatch '^[0-9A-Fa-f]{64}$') {
    throw '-BundleSha256 must be exactly 64 hexadecimal characters'
}
if ((Test-StringPresent $Version) -and $Version -cnotmatch '^v?[0-9A-Za-z][0-9A-Za-z._+-]*$') {
    throw 'Invalid expected version'
}

$LocalAppData = if (Test-StringPresent $env:LOCALAPPDATA) {
    $env:LOCALAPPDATA
} else {
    [Environment]::GetFolderPath([Environment+SpecialFolder]::LocalApplicationData)
}
if (-not (Test-StringPresent $LocalAppData)) { throw 'Unable to resolve the Windows local application data directory' }
if (-not (Test-StringPresent $InstallRoot)) {
    $InstallRoot = Join-Path (Join-Path $LocalAppData 'Programs') 'IPGW-Meta'
}
if (-not (Test-StringPresent $BinDir)) {
    $BinDir = Join-Path (Join-Path $LocalAppData 'IPGW-Meta') 'bin'
}
$ConfigBase = if (Test-StringPresent $env:APPDATA) {
    $env:APPDATA
} else {
    [Environment]::GetFolderPath([Environment+SpecialFolder]::ApplicationData)
}
if (-not (Test-StringPresent $ConfigBase)) { throw 'Unable to resolve the Windows user configuration directory' }
$UserHome = if (Test-StringPresent $env:USERPROFILE) {
    $env:USERPROFILE
} else {
    [Environment]::GetFolderPath([Environment+SpecialFolder]::UserProfile)
}

$InstallRoot = Get-LocalFixedPath -LiteralPath $InstallRoot -Label 'install root'
$BinDir = Get-LocalFixedPath -LiteralPath $BinDir -Label 'binary directory'
$ConfigBase = Get-LocalFixedPath -LiteralPath $ConfigBase -Label 'configuration directory'
$ConfigDir = Get-LocalFixedPath -LiteralPath (Join-Path $ConfigBase 'ipgw-meta') -Label 'launcher directory'
if ($Offline) { $BundlePath = Get-LocalFixedPath -LiteralPath $BundlePath -Label 'bundle path' }
if (Test-StringPresent $UserHome) { $UserHome = [IO.Path]::GetFullPath($UserHome).TrimEnd([char[]]'\/') }

foreach ($PathCheck in @(
    @($InstallRoot, 'install root'),
    @($BinDir, 'binary directory'),
    @($ConfigDir, 'launcher directory')
)) {
    Assert-DirectoryAncestors -LiteralPath $PathCheck[0] -Label $PathCheck[1]
}
if ($Offline) {
    Assert-DirectoryAncestors -LiteralPath (Split-Path -Parent $BundlePath) -Label 'bundle path'
}
if ((Test-PathsOverlap -Left $InstallRoot -Right $BinDir) -or
    (Test-PathsOverlap -Left $InstallRoot -Right $ConfigDir) -or
    (Test-PathsOverlap -Left $BinDir -Right $ConfigDir)) {
    throw 'Install root, binary directory, and launcher directory must not overlap'
}
if ((Test-StringPresent $UserHome) -and @($InstallRoot, $BinDir, $ConfigDir) -icontains $UserHome) {
    throw 'Refusing to use the user profile directory as an installation target'
}
$RepositoryRoot = [IO.Path]::GetFullPath($PSScriptRoot).TrimEnd([char[]]'\/')
if (Test-Path -LiteralPath (Join-Path $RepositoryRoot '.git')) {
    foreach ($TargetPath in @($InstallRoot, $BinDir, $ConfigDir)) {
        if (Test-PathsOverlap -Left $TargetPath -Right $RepositoryRoot) {
            throw 'Refusing an installation target that overlaps the repository'
        }
    }
}
if (-not [string]::Equals(
    [IO.Path]::GetPathRoot($InstallRoot),
    [IO.Path]::GetPathRoot($BinDir),
    [StringComparison]::OrdinalIgnoreCase
)) {
    throw 'Install root and binary directory must be on the same volume for atomic entry publication'
}

$script:TestRoot = $env:IPGW_INSTALL_TEST_ROOT
$TestToken = $env:IPGW_INSTALL_TEST_TOKEN
$script:TestFailpoint = $env:IPGW_INSTALL_TEST_FAILPOINT
$script:TestRollbackFailpoint = $env:IPGW_INSTALL_TEST_ROLLBACK_FAILPOINT
$script:TestMode = $false
$AnyTestControl = (Test-StringPresent $script:TestRoot) -or
    (Test-StringPresent $TestToken) -or
    (Test-StringPresent $script:TestFailpoint) -or
    (Test-StringPresent $script:TestRollbackFailpoint)
if ($AnyTestControl) {
    if (-not $Offline) { throw 'Installer test controls require offline mode' }
    if (-not (Test-StringPresent $script:TestRoot) -or
        $TestToken -cnotmatch '^[0-9A-Za-z._+-]{1,128}$') {
        throw 'Installer test root and safe token are required'
    }
    $script:TestRoot = Get-LocalFixedPath -LiteralPath $script:TestRoot -Label 'installer test root'
    Assert-DirectoryAncestors -LiteralPath $script:TestRoot -Label 'installer test root'
    Assert-PrivateDirectory -LiteralPath $script:TestRoot -Label 'installer test root'
    $TokenFile = Join-Path $script:TestRoot '.ipgw-install-test-token'
    Assert-PrivateFileAcl -LiteralPath $TokenFile -Label 'installer test token file'
    $TokenItem = Get-Item -LiteralPath $TokenFile -Force
    if ($TokenItem.Length -lt 1 -or $TokenItem.Length -gt 128 -or
        -not [string]::Equals([IO.File]::ReadAllText($TokenFile), $TestToken, [StringComparison]::Ordinal)) {
        throw 'Installer test token does not match'
    }
    if ((Test-StringPresent $script:TestFailpoint) -and $AllowedForwardFailpoints -cnotcontains $script:TestFailpoint) {
        throw 'Invalid installer failpoint'
    }
    if ((Test-StringPresent $script:TestRollbackFailpoint) -and $AllowedRollbackFailpoints -cnotcontains $script:TestRollbackFailpoint) {
        throw 'Invalid installer rollback failpoint'
    }
    foreach ($TestPath in @($BundlePath, $InstallRoot, $BinDir, $ConfigDir)) {
        if (-not (Test-PathWithin -Child $TestPath -Parent $script:TestRoot)) {
            throw 'All installer test inputs and targets must be inside the private test root'
        }
    }
    $script:TestMode = $true
}

if ($Offline) {
    $BundleItem = Assert-RegularFile -LiteralPath $BundlePath -Label 'offline bundle'
    if ($BundleItem.Length -lt 1 -or $BundleItem.Length -gt $MaximumArchiveBytes) {
        throw 'Offline bundle size must be between 1 byte and 100 MiB'
    }
    Assert-UntrustedPrincipalsCannotWrite -LiteralPath $BundlePath
}

$NativeArch = if (Test-StringPresent $env:PROCESSOR_ARCHITEW6432) {
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

$ReleaseBase = $null
if (-not $Offline) {
    $RepositoryURL = 'https://github.com/UnbalancedCat/ipgw-meta'
    if (Test-StringPresent $Version) {
        $ReleaseBase = "$RepositoryURL/releases/download/$Version"
    } else {
        $ReleaseBase = "$RepositoryURL/releases/latest/download"
    }
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
}

$TempParent = if ($script:TestMode) {
    Join-Path $script:TestRoot '.installer-tmp'
} else {
    Get-LocalFixedPath -LiteralPath ([IO.Path]::GetTempPath()) -Label 'temporary directory'
}
if ($script:TestMode) {
    Ensure-PrivateDirectory -LiteralPath $TempParent
} else {
    Assert-DirectoryAncestors -LiteralPath $TempParent -Label 'temporary directory'
    $TempItem = Get-Item -LiteralPath $TempParent -Force -ErrorAction Stop
    if (-not $TempItem.PSIsContainer -or (($TempItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)) {
        throw 'Temporary directory must be an existing real directory'
    }
}

$AcquisitionDir = Join-Path $TempParent ('ipgw-meta-acquire.' + [guid]::NewGuid().ToString('N'))
[void](New-Item -ItemType Directory -Path $AcquisitionDir)
Set-PrivateDirectoryAcl -LiteralPath $AcquisitionDir
$ArchiveFile = Join-Path $AcquisitionDir $ArchiveName
$ChecksumsFile = Join-Path $AcquisitionDir 'SHA256SUMS'
$script:Stage = $null
$script:TransactionDir = $null
$script:JournalFile = $null
$script:VersionDir = $null
$script:BackupDir = $null
$script:InstalledEntries = New-Object Collections.Generic.List[string]
$script:BackedEntries = New-Object Collections.Generic.List[string]
$script:HadActive = $false
$script:ActiveDetached = $false
$script:ActiveSwitched = $false
$script:VersionInstalled = $false
$script:LauncherInstalled = $false
$script:PathChanged = $false
$script:Committed = $false
$RollbackFailed = $false
$ActiveNext = $null
$EntryNext = $null
$LauncherTemp = $null
$OldActiveTarget = $null
$OldUserPath = $null
$OldUserPathExists = $false
$OldProcessPath = $env:Path
$TestPathFile = if ($script:TestMode) { Join-Path $script:TestRoot '.ipgw-user-path' } else { $null }

try {
    if ($Offline) {
        $SourceStream = $null
        $DestinationStream = $null
        try {
            $SourceStream = [IO.File]::Open($BundlePath, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::Read)
            if ($SourceStream.Length -lt 1 -or $SourceStream.Length -gt $MaximumArchiveBytes) {
                throw 'Offline bundle size changed while being opened'
            }
            $PathItem = Assert-RegularFile -LiteralPath $BundlePath -Label 'offline bundle'
            if ($PathItem.Length -ne $SourceStream.Length) { throw 'Offline bundle changed while being opened' }
            Assert-UntrustedPrincipalsCannotWrite -LiteralPath $BundlePath
            $DestinationStream = [IO.File]::Open($ArchiveFile, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
            $SourceStream.CopyTo($DestinationStream, 81920)
            $DestinationStream.Flush($true)
        } finally {
            if ($null -ne $DestinationStream) { $DestinationStream.Dispose() }
            if ($null -ne $SourceStream) { $SourceStream.Dispose() }
        }
        Set-PrivateFileAcl -LiteralPath $ArchiveFile
        if ((Get-Item -LiteralPath $ArchiveFile -Force).Length -ne $PathItem.Length) {
            throw 'Offline bundle copy did not preserve the opened source'
        }
        $ExpectedArchiveHash = $BundleSha256.ToLowerInvariant()
    } else {
        Invoke-HttpsDownload -Uri "$ReleaseBase/SHA256SUMS" -LiteralPath $ChecksumsFile -MaxBytes $MaximumChecksumBytes
        Invoke-HttpsDownload -Uri "$ReleaseBase/$ArchiveName" -LiteralPath $ArchiveFile -MaxBytes $MaximumArchiveBytes
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
            if ($Line -cnotmatch '^([0-9A-Fa-f]{64})  ([0-9A-Za-z.-]+)$') { throw 'Invalid release checksum record' }
            $Hash = $Matches[1].ToLowerInvariant()
            $Name = $Matches[2]
            if ($AllowedOuter -cnotcontains $Name) { throw 'Unexpected release checksum target' }
            if ($SeenOuter.ContainsKey($Name)) { throw 'Duplicate release checksum target' }
            $SeenOuter[$Name] = $true
            if ($Name -ceq $ArchiveName) { $ExpectedArchiveHash = $Hash }
        }
        if ($SeenOuter.Count -ne $AllowedOuter.Count -or $null -eq $ExpectedArchiveHash) {
            throw 'Release checksum manifest is incomplete'
        }
    }

    $ArchiveItem = Assert-RegularFile -LiteralPath $ArchiveFile -Label 'acquired archive'
    if ($ArchiveItem.Length -lt 1 -or $ArchiveItem.Length -gt $MaximumArchiveBytes) {
        throw 'Release archive must be between 1 byte and 100 MiB'
    }
    $ActualHash = (Get-FileHash -LiteralPath $ArchiveFile -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($ActualHash -cne $ExpectedArchiveHash) { throw 'Acquired archive failed SHA-256 verification' }

    Ensure-PrivateDirectory -LiteralPath $InstallRoot
    $VersionsDir = Join-Path $InstallRoot 'versions'
    Ensure-PrivateDirectory -LiteralPath $VersionsDir
    Ensure-PrivateDirectory -LiteralPath $BinDir
    Assert-DirectoryAncestors -LiteralPath $InstallRoot -Label 'install root'
    Assert-DirectoryAncestors -LiteralPath $BinDir -Label 'binary directory'

    foreach ($Pending in @(Get-ChildItem -LiteralPath $InstallRoot -Force | Where-Object {
        $_.Name -like '.transaction.*' -or $_.Name -like '.staging.*' -or $_.Name -like '.active-next.*'
    })) {
        throw 'An unfinished installer transaction requires recovery before continuing'
    }
    foreach ($Pending in @(Get-ChildItem -LiteralPath $BinDir -Force | Where-Object {
        $_.Name -like '.ipgw-meta-backup.*' -or $_.Name -like '.*.next.*'
    })) {
        throw 'An unfinished binary entry transaction requires recovery before continuing'
    }

    $script:Stage = Join-Path $InstallRoot ('.staging.' + [guid]::NewGuid().ToString('N'))
    [void](New-Item -ItemType Directory -Path $script:Stage)
    Set-PrivateDirectoryAcl -LiteralPath $script:Stage
    Expand-VerifiedBundleArchive -ArchivePath $ArchiveFile -DestinationPath $script:Stage

    $AllowedInner = @('ipgw.exe', 'ipgw-meta.exe', 'ipgw-legacy.exe', 'LICENSE', 'launcher-default.yaml', 'bundle-manifest.json')
    $SeenInner = @{}
    foreach ($Line in Get-Content -LiteralPath (Join-Path $script:Stage 'SHA256SUMS')) {
        if ($Line -cnotmatch '^([0-9A-Fa-f]{64})  ([0-9A-Za-z.-]+)$') { throw 'Invalid internal checksum record' }
        $Expected = $Matches[1].ToLowerInvariant()
        $Name = $Matches[2]
        if ($AllowedInner -cnotcontains $Name -or $SeenInner.ContainsKey($Name)) {
            throw 'Unexpected or duplicate internal checksum target'
        }
        $Member = Join-Path $script:Stage $Name
        [void](Assert-RegularFile -LiteralPath $Member -Label "bundle member $Name")
        if ((Get-FileHash -LiteralPath $Member -Algorithm SHA256).Hash.ToLowerInvariant() -cne $Expected) {
            throw "Bundle member failed SHA-256 verification: $Name"
        }
        $SeenInner[$Name] = $true
    }
    if ($SeenInner.Count -ne $AllowedInner.Count) { throw 'Bundle checksum manifest is incomplete' }

    $ManifestPath = Join-Path $script:Stage 'bundle-manifest.json'
    $ManifestRaw = [IO.File]::ReadAllText($ManifestPath)
    try { $Manifest = $ManifestRaw | ConvertFrom-Json -ErrorAction Stop } catch { throw 'Bundle manifest is not valid JSON' }
    $ExpectedRootProperties = @('schema_version', 'product', 'module', 'version', 'platform', 'entries', 'launcher_default', 'layout', 'self_update', 'uninstall')
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
        $Manifest.self_update -isnot [bool] -or $Manifest.self_update -ne $false) {
        throw 'Bundle manifest is incompatible with this installer'
    }
    if ((Test-StringPresent $Version) -and $Manifest.version -cne $Version) {
        throw 'Bundle manifest version does not match the pinned installer'
    }
    $ExpectedUninstallProperties = @('remove_all_three_entries', 'preserve_user_config')
    $ActualUninstallProperties = @($Manifest.uninstall.PSObject.Properties | ForEach-Object { $_.Name })
    $UninstallDifference = @(Compare-Object -CaseSensitive -ReferenceObject $ExpectedUninstallProperties -DifferenceObject $ActualUninstallProperties)
    if ($ActualUninstallProperties.Count -ne 2 -or $UninstallDifference.Count -ne 0 -or
        $Manifest.uninstall.remove_all_three_entries -isnot [bool] -or $Manifest.uninstall.remove_all_three_entries -ne $true -or
        $Manifest.uninstall.preserve_user_config -isnot [bool] -or $Manifest.uninstall.preserve_user_config -ne $true) {
        throw 'Bundle manifest uninstall metadata is invalid'
    }
    $ManifestEntries = @($Manifest.entries)
    if ($ManifestEntries.Count -ne $EntryNames.Count) { throw 'Bundle manifest does not describe all three entry points' }
    $ExpectedEntryProperties = @('path', 'sha256', 'size')
    $EntryHashes = @()
    $EntrySizes = @()
    for ($Index = 0; $Index -lt $EntryNames.Count; $Index++) {
        $Entry = $ManifestEntries[$Index]
        $Properties = @($Entry.PSObject.Properties | ForEach-Object { $_.Name })
        $Difference = @(Compare-Object -CaseSensitive -ReferenceObject $ExpectedEntryProperties -DifferenceObject $Properties)
        if ($Properties.Count -ne 3 -or $Difference.Count -ne 0) { throw 'Bundle manifest entry has invalid properties' }
        $ExpectedPath = $EntryNames[$Index]
        $EntryFile = Join-Path $script:Stage $ExpectedPath
        $Hash = (Get-FileHash -LiteralPath $EntryFile -Algorithm SHA256).Hash.ToLowerInvariant()
        [long]$Size = (Get-Item -LiteralPath $EntryFile -Force).Length
        if ($Entry.path -isnot [string] -or $Entry.path -cne $ExpectedPath -or
            $Entry.sha256 -isnot [string] -or $Entry.sha256 -cnotmatch '^[0-9a-f]{64}$' -or $Entry.sha256 -cne $Hash -or
            ($Entry.size -isnot [int] -and $Entry.size -isnot [long]) -or [long]$Entry.size -ne $Size) {
            throw "Bundle manifest entry does not bind the actual path, hash, and size: $ExpectedPath"
        }
        $EntryHashes += $Hash
        $EntrySizes += $Size
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
    if (-not [string]::Equals($ManifestRaw, (($CanonicalManifestLines -join "`n") + "`n"), [StringComparison]::Ordinal)) {
        throw 'Bundle manifest is not in the canonical v1 format'
    }
    $LauncherDefault = [IO.File]::ReadAllText((Join-Path $script:Stage 'launcher-default.yaml'))
    if (-not [string]::Equals($LauncherDefault, "schema_version: 1`nmode: meta`ncohort: new-install`n", [StringComparison]::Ordinal)) {
        throw 'Bundle launcher metadata is invalid'
    }

    Invoke-MaybeFail -Point 'after_verified_stage'

    $ActivePath = Join-Path $InstallRoot 'active'
    $ActiveItem = Get-Item -LiteralPath $ActivePath -Force -ErrorAction SilentlyContinue
    if ($null -ne $ActiveItem) {
        $script:HadActive = $true
        $OldActiveTarget = Get-ManagedJunctionTarget -LiteralPath $ActivePath -VersionsPath $VersionsDir -Label 'active path'
    }
    foreach ($Name in $EntryNames) {
        $Destination = Join-Path $BinDir $Name
        $Existing = Get-Item -LiteralPath $Destination -Force -ErrorAction SilentlyContinue
        if ($script:HadActive) {
            if ($null -eq $Existing -or $Existing.PSIsContainer -or
                (($Existing.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) -or
                -not (Test-FilesMatch -Left $Destination -Right (Join-Path $OldActiveTarget $Name))) {
                throw 'Existing binary entries do not match the managed active version'
            }
        } elseif ($null -ne $Existing) {
            throw 'Refusing to replace an unmanaged binary entry'
        }
    }

    $HadInstall = $script:HadActive
    if (-not $HadInstall) {
        $LegacyMetaConfig = Join-Path (Join-Path $ConfigBase 'ipgw') 'config.yaml'
        $LegacyUpstream = if (Test-StringPresent $UserHome) { Join-Path $UserHome '.ipgw' } else { $null }
        if ((Test-Path -LiteralPath $LegacyMetaConfig) -or
            ((Test-StringPresent $LegacyUpstream) -and (Test-Path -LiteralPath $LegacyUpstream))) {
            $HadInstall = $true
        }
    }

    Ensure-PrivateDirectory -LiteralPath $ConfigDir
    foreach ($Pending in @(Get-ChildItem -LiteralPath $ConfigDir -Force | Where-Object { $_.Name -like '.launcher.*' })) {
        throw 'An unfinished launcher transaction requires recovery before continuing'
    }
    $LauncherFile = Join-Path $ConfigDir 'launcher.yaml'
    $ExistingLauncher = Get-Item -LiteralPath $LauncherFile -Force -ErrorAction SilentlyContinue
    if ($null -ne $ExistingLauncher) {
        [void](Assert-RegularFile -LiteralPath $LauncherFile -Label 'launcher configuration')
        Set-PrivateFileAcl -LiteralPath $LauncherFile
    } else {
        $Mode = if ($HadInstall) { 'legacy' } else { 'meta' }
        $Cohort = if ($HadInstall) { 'existing-install' } else { 'new-install' }
        $LauncherTemp = Join-Path $ConfigDir ('.launcher.' + [guid]::NewGuid().ToString('N'))
        $ChosenAt = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
        Write-PrivateFile -LiteralPath $LauncherTemp -Content "schema_version: 1`nmode: $Mode`ncohort: $Cohort`nchosen_at: $ChosenAt`n"
    }

    $VersionID = $ActualHash.Substring(0, 16) + '-' + [DateTime]::UtcNow.ToString('yyyyMMddHHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
    $script:VersionDir = Join-Path $VersionsDir $VersionID
    if (Test-Path -LiteralPath $script:VersionDir) { throw 'Refusing to overwrite an existing version directory' }
    $script:TransactionDir = Join-Path $InstallRoot ('.transaction.' + [guid]::NewGuid().ToString('N'))
    [void](New-Item -ItemType Directory -Path $script:TransactionDir)
    Set-PrivateDirectoryAcl -LiteralPath $script:TransactionDir
    $script:JournalFile = Join-Path $script:TransactionDir 'journal'
    Write-Journal -Phase 'verified-stage'

    Move-Item -LiteralPath $script:Stage -Destination $script:VersionDir
    $script:Stage = $null
    $script:VersionInstalled = $true
    Set-PrivateDirectoryAcl -LiteralPath $script:VersionDir
    Write-Journal -Phase 'version-published'
    Invoke-MaybeFail -Point 'after_version_publish'

    $ActiveNext = Join-Path $InstallRoot ('.active-next.' + [guid]::NewGuid().ToString('N'))
    [void](New-Item -ItemType Junction -Path $ActiveNext -Target $script:VersionDir)
    if ($script:HadActive) {
        $CurrentTarget = Get-ManagedJunctionTarget -LiteralPath $ActivePath -VersionsPath $VersionsDir -Label 'active path'
        if (-not [string]::Equals($CurrentTarget, $OldActiveTarget, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Managed active junction changed during installation'
        }
        Move-Item -LiteralPath $ActivePath -Destination (Join-Path $script:TransactionDir 'active.previous')
        $script:ActiveDetached = $true
    }
    Write-Journal -Phase 'old-active-detached'
    Invoke-MaybeFail -Point 'after_old_active_detach'

    if (Test-Path -LiteralPath $ActivePath) { throw 'Active path appeared during installation' }
    Move-Item -LiteralPath $ActiveNext -Destination $ActivePath
    $ActiveNext = $null
    $script:ActiveSwitched = $true
    [void](Get-ManagedJunctionTarget -LiteralPath $ActivePath -VersionsPath $VersionsDir -Label 'active path')
    Write-Journal -Phase 'active-switched'
    Invoke-MaybeFail -Point 'after_active_switch'

    $script:BackupDir = Join-Path $BinDir ('.ipgw-meta-backup.' + [guid]::NewGuid().ToString('N'))
    [void](New-Item -ItemType Directory -Path $script:BackupDir)
    Set-PrivateDirectoryAcl -LiteralPath $script:BackupDir
    for ($Index = 0; $Index -lt $EntryNames.Count; $Index++) {
        $Name = $EntryNames[$Index]
        $Destination = Join-Path $BinDir $Name
        if (Test-Path -LiteralPath $Destination) {
            if (-not (Test-FilesMatch -Left $Destination -Right (Join-Path $OldActiveTarget $Name))) {
                throw 'Managed binary entry changed during installation'
            }
            Move-Item -LiteralPath $Destination -Destination (Join-Path $script:BackupDir $Name)
            [void]$script:BackedEntries.Add($Name)
        }
        $EntryNext = Join-Path $BinDir ('.' + $Name + '.next.' + [guid]::NewGuid().ToString('N'))
        [void](New-Item -ItemType HardLink -Path $EntryNext -Target (Join-Path $script:VersionDir $Name))
        [void]$script:InstalledEntries.Add($Name)
        Move-Item -LiteralPath $EntryNext -Destination $Destination
        $EntryNext = $null
        Write-Journal -Phase ('entry-' + ($Index + 1) + '-published')
        if ($Index -eq 0) { Invoke-MaybeFail -Point 'after_entry_1' }
        if ($Index -eq 1) { Invoke-MaybeFail -Point 'after_entry_2' }
    }

    if ($null -ne $LauncherTemp) {
        if (Test-Path -LiteralPath $LauncherFile) {
            Remove-Item -LiteralPath $LauncherTemp -Force
        } else {
            Move-Item -LiteralPath $LauncherTemp -Destination $LauncherFile
            $script:LauncherInstalled = $true
        }
        $LauncherTemp = $null
    }
    Write-Journal -Phase 'launcher-published'
    Invoke-MaybeFail -Point 'after_launcher_publish'

    if ($script:TestMode) {
        $OldUserPathExists = Test-Path -LiteralPath $TestPathFile
        $OldUserPath = if ($OldUserPathExists) { [IO.File]::ReadAllText($TestPathFile) } else { '' }
    } else {
        $OldUserPath = [Environment]::GetEnvironmentVariable('Path', [EnvironmentVariableTarget]::User)
        $OldUserPathExists = $null -ne $OldUserPath
    }
    $PathParts = @($OldUserPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    if (-not ($PathParts | Where-Object { $_.TrimEnd([char[]]'\/') -ieq $BinDir.TrimEnd([char[]]'\/') })) {
        $NewUserPath = if ([string]::IsNullOrWhiteSpace($OldUserPath)) { $BinDir } else { "$OldUserPath;$BinDir" }
        if ($script:TestMode) {
            Write-PrivateFile -LiteralPath $TestPathFile -Content $NewUserPath
        } else {
            [Environment]::SetEnvironmentVariable('Path', $NewUserPath, [EnvironmentVariableTarget]::User)
            $env:Path = if ([string]::IsNullOrWhiteSpace($env:Path)) { $BinDir } else { "$env:Path;$BinDir" }
        }
        $script:PathChanged = $true
    }
    Write-Journal -Phase 'path-handled'
    Invoke-MaybeFail -Point 'after_path_update'
    Write-Journal -Phase 'ready-to-commit'
    Invoke-MaybeFail -Point 'before_commit'
    Write-Journal -Phase 'committed'
    $script:Committed = $true
    $script:VersionInstalled = $false

    if ($script:HadActive) {
        Remove-SafeTree -LiteralPath (Join-Path $script:TransactionDir 'active.previous')
    }
    if ($null -ne $script:BackupDir) {
        Remove-SafeTree -LiteralPath $script:BackupDir
        $script:BackupDir = $null
    }
    Remove-SafeTree -LiteralPath $script:TransactionDir
    $script:TransactionDir = $null
    $script:JournalFile = $null

    Write-Output "IPGW-Meta installed as one three-entry bundle in $($script:VersionDir)"
    Write-Output 'Launcher mode was preserved for existing installs; new installs default to meta.'
} catch {
    $OriginalError = $_
    if (-not $script:Committed -and (Test-JournalCommitted)) {
        $script:Committed = $true
        Write-Warning 'Installation commit was persisted; recovery cleanup remains pending'
    }
    if (-not $script:Committed) {
        $RollbackErrors = New-Object Collections.Generic.List[string]
        if ($script:PathChanged) {
            try {
                if ($script:TestMode) {
                    if ($OldUserPathExists) {
                        Write-PrivateFile -LiteralPath $TestPathFile -Content $OldUserPath
                    } elseif (Test-Path -LiteralPath $TestPathFile) {
                        Remove-Item -LiteralPath $TestPathFile -Force
                    }
                } else {
                    [Environment]::SetEnvironmentVariable('Path', $(if ($OldUserPathExists) { $OldUserPath } else { $null }), [EnvironmentVariableTarget]::User)
                    $env:Path = $OldProcessPath
                }
            } catch { [void]$RollbackErrors.Add($_.Exception.Message) }
        }
        if ($script:LauncherInstalled) {
            try {
                [void](Assert-RegularFile -LiteralPath $LauncherFile -Label 'installed launcher configuration')
                Remove-Item -LiteralPath $LauncherFile -Force
            } catch { [void]$RollbackErrors.Add($_.Exception.Message) }
        }
        if ($script:InstalledEntries.Count -gt 0) {
            if (Test-RollbackFailpoint -Point 'before_restore_entry_1') {
                [void]$RollbackErrors.Add('installer rollback failpoint triggered: before_restore_entry_1')
            } else {
                for ($Index = $script:InstalledEntries.Count - 1; $Index -ge 0; $Index--) {
                    $Name = $script:InstalledEntries[$Index]
                    $Destination = Join-Path $BinDir $Name
                    try {
                        if (-not (Test-FilesMatch -Left $Destination -Right (Join-Path $script:VersionDir $Name))) {
                            throw 'Refusing to remove an entry that no longer matches the new version'
                        }
                        Remove-Item -LiteralPath $Destination -Force
                    } catch { [void]$RollbackErrors.Add($_.Exception.Message) }
                }
                for ($Index = $script:BackedEntries.Count - 1; $Index -ge 0; $Index--) {
                    $Name = $script:BackedEntries[$Index]
                    try {
                        $Destination = Join-Path $BinDir $Name
                        $BackupEntry = Join-Path $script:BackupDir $Name
                        if (Test-Path -LiteralPath $Destination) { throw 'Binary destination reappeared during rollback' }
                        [void](Assert-RegularFile -LiteralPath $BackupEntry -Label 'backed-up binary entry')
                        Move-Item -LiteralPath $BackupEntry -Destination $Destination
                    } catch { [void]$RollbackErrors.Add($_.Exception.Message) }
                }
            }
        }
        if ($RollbackErrors.Count -eq 0 -and ($script:ActiveSwitched -or $script:ActiveDetached)) {
            if (Test-RollbackFailpoint -Point 'before_restore_active') {
                [void]$RollbackErrors.Add('installer rollback failpoint triggered: before_restore_active')
            } else {
                try {
                    if ($script:ActiveSwitched) {
                        $CurrentTarget = Get-ManagedJunctionTarget -LiteralPath $ActivePath -VersionsPath $VersionsDir -Label 'active path'
                        if (-not [string]::Equals($CurrentTarget, $script:VersionDir, [StringComparison]::OrdinalIgnoreCase)) {
                            throw 'Active path no longer selects the new version'
                        }
                        Remove-SafeTree -LiteralPath $ActivePath
                    }
                    if ($script:ActiveDetached) {
                        $Previous = Join-Path $script:TransactionDir 'active.previous'
                        if (Test-Path -LiteralPath $ActivePath) { throw 'Active path reappeared during rollback' }
                        [void](Get-ManagedJunctionTarget -LiteralPath $Previous -VersionsPath $VersionsDir -Label 'previous active path')
                        Move-Item -LiteralPath $Previous -Destination $ActivePath
                    }
                } catch { [void]$RollbackErrors.Add($_.Exception.Message) }
            }
        }
        if ($RollbackErrors.Count -eq 0 -and $script:VersionInstalled) {
            if (Test-RollbackFailpoint -Point 'before_remove_new_version') {
                [void]$RollbackErrors.Add('installer rollback failpoint triggered: before_remove_new_version')
            } else {
                try {
                    $VersionsPrefix = $VersionsDir.TrimEnd([char[]]'\/') + '\'
                    $Leaf = Split-Path -Leaf $script:VersionDir
                    if (-not $script:VersionDir.StartsWith($VersionsPrefix, [StringComparison]::OrdinalIgnoreCase) -or
                        $Leaf -cnotmatch '^[0-9a-f]{16}-[0-9]{14}-[0-9a-f]{8}$') {
                        throw 'Refusing to remove an unexpected rollback version path'
                    }
                    Assert-PrivateDirectory -LiteralPath $script:VersionDir -Label 'rollback version directory'
                    Remove-SafeTree -LiteralPath $script:VersionDir
                    $script:VersionInstalled = $false
                } catch { [void]$RollbackErrors.Add($_.Exception.Message) }
            }
        }
        if ($RollbackErrors.Count -eq 0) {
            try {
                if ($null -ne $script:BackupDir -and (Test-Path -LiteralPath $script:BackupDir)) {
                    Remove-SafeTree -LiteralPath $script:BackupDir
                    $script:BackupDir = $null
                }
                if ($null -ne $script:TransactionDir -and (Test-Path -LiteralPath $script:TransactionDir)) {
                    Remove-SafeTree -LiteralPath $script:TransactionDir
                    $script:TransactionDir = $null
                    $script:JournalFile = $null
                }
            } catch { [void]$RollbackErrors.Add($_.Exception.Message) }
        }
        if ($RollbackErrors.Count -gt 0) {
            $RollbackFailed = $true
            foreach ($RollbackError in $RollbackErrors) { Write-Warning $RollbackError }
            Write-Warning 'Rollback was incomplete; recovery materials remain under the install root'
        }
    }
    throw $OriginalError
} finally {
    if ($null -ne $ActiveNext -and (Test-Path -LiteralPath $ActiveNext)) { Remove-SafeTree -LiteralPath $ActiveNext }
    if ($null -ne $EntryNext -and (Test-Path -LiteralPath $EntryNext)) { Remove-SafeTree -LiteralPath $EntryNext }
    if ($null -ne $LauncherTemp -and (Test-Path -LiteralPath $LauncherTemp)) { Remove-SafeTree -LiteralPath $LauncherTemp }
    if (-not $RollbackFailed -and $null -ne $script:Stage -and (Test-Path -LiteralPath $script:Stage)) {
        Remove-SafeTree -LiteralPath $script:Stage
    }
    if (Test-Path -LiteralPath $AcquisitionDir) { Remove-SafeTree -LiteralPath $AcquisitionDir }
    if ($script:TestMode -and (Test-Path -LiteralPath $TempParent)) {
        $Children = @(Get-ChildItem -LiteralPath $TempParent -Force)
        if ($Children.Count -eq 0) { Remove-Item -LiteralPath $TempParent -Force }
    }
}
