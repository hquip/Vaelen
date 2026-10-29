param(
  [ValidatePattern('^v?\d+\.\d+\.\d+$')][string]$Version,
  [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'VSM\bin')
)
$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Use install.sh on macOS/Linux.' }
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$vsmArch = 'amd64'
try { if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() -eq 'Arm64') { $vsmArch = 'arm64' } } catch { if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { $vsmArch = 'arm64' } }
$vsmRepository = 'https://github.com/hquip/Vaelen'
$vsmEndpoint = 'https://api.github.com/repos/hquip/Vaelen/releases/latest'
if ($Version) { $Version = 'v' + $Version.TrimStart('v'); $vsmEndpoint = 'https://api.github.com/repos/hquip/Vaelen/releases/tags/' + $Version }
$vsmRelease = Invoke-RestMethod -Uri $vsmEndpoint -Headers @{'User-Agent'='vsm-installer'} -TimeoutSec 45
if ($vsmRelease.draft -or $vsmRelease.prerelease -or $vsmRelease.tag_name -notmatch '^v\d+\.\d+\.\d+$') { throw 'Expected an official stable release.' }
if ($Version -and $vsmRelease.tag_name -ne $Version) { throw 'GitHub returned a different version.' }
$vsmAsset = 'vsm-windows-' + $vsmArch + '.exe'
$vsmAssets = @($vsmRelease.assets | Where-Object name -eq $vsmAsset)
if ($vsmAssets.Count -ne 1) { throw 'The release has no unique executable for this platform.' }
$vsmBase = $vsmRepository + '/releases/download/' + $vsmRelease.tag_name + '/'
if ($vsmAssets[0].browser_download_url -ne $vsmBase + $vsmAsset) { throw 'Unexpected download source.' }
$vsmResolvedDir = [IO.Path]::GetFullPath($InstallDir)
[IO.Directory]::CreateDirectory($vsmResolvedDir) | Out-Null
$vsmStage = Join-Path $vsmResolvedDir ('.vsm-install-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($vsmStage) | Out-Null
try {
  $vsmDownloaded = Join-Path $vsmStage $vsmAsset
  Invoke-WebRequest -UseBasicParsing -Uri ($vsmBase + $vsmAsset) -OutFile $vsmDownloaded -TimeoutSec 600
  $vsmManifest = Join-Path $vsmStage 'SHA256SUMS'
  Invoke-WebRequest -UseBasicParsing -Uri ($vsmBase + 'SHA256SUMS') -OutFile $vsmManifest -TimeoutSec 60
  $vsmMatches = @([IO.File]::ReadAllLines($vsmManifest) | Where-Object { $_ -match ('^[0-9a-fA-F]{64}\s+\*?' + [Regex]::Escape($vsmAsset) + '$') })
  if ($vsmMatches.Count -ne 1) { throw 'Missing or ambiguous official checksum.' }
  $vsmExpected = ($vsmMatches[0] -split '\s+')[0]
  if ((Get-FileHash -LiteralPath $vsmDownloaded -Algorithm SHA256).Hash -ne $vsmExpected) { throw 'SHA256 verification failed; existing VSM was kept.' }
  if ((Get-Item -LiteralPath $vsmDownloaded).Length -ne $vsmAssets[0].size) { throw 'Download size mismatch.' }
  $vsmTarget = Join-Path $vsmResolvedDir 'vsm.exe'
  $vsmBackup = $null
  if (Test-Path -LiteralPath $vsmTarget) {
    $vsmExisting = Get-Item -LiteralPath $vsmTarget -Force
    if ($vsmExisting.PSIsContainer -or ($vsmExisting.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Destination must be a regular executable.' }
    $vsmBackup = $vsmTarget + '.old.' + [Guid]::NewGuid().ToString('N')
    Move-Item -LiteralPath $vsmTarget -Destination $vsmBackup
  }
  try { Move-Item -LiteralPath $vsmDownloaded -Destination $vsmTarget } catch {
    if ($vsmBackup) { Move-Item -LiteralPath $vsmBackup -Destination $vsmTarget }
    throw
  }
  Write-Output ('Installed VSM ' + $vsmRelease.tag_name + ': ' + $vsmTarget)
  $vsmRegistrationArgs = @('path-init', 'cmd')
  try {
    $vsmHelp = & $vsmTarget help path-init 2>$null
    if ($LASTEXITCODE -eq 0 -and ($vsmHelp -join "`n") -match '--command-only') { $vsmRegistrationArgs = @('path-init', '--command-only') }
    # Older releases predate --command-only. Run their cmd setup outside the
    # invoking project so local/docker isolation cannot block installation.
    $vsmPriorBoundary = $env:VSM_PROJECT_BOUNDARY
    $vsmPriorExpectedMode = $env:VSM_MCP_EXPECT_MODE
    Push-Location -LiteralPath $vsmStage
    try {
      $env:VSM_PROJECT_BOUNDARY = $vsmStage
      $env:VSM_MCP_EXPECT_MODE = $null
      & $vsmTarget @vsmRegistrationArgs
      if ($LASTEXITCODE -ne 0) { throw ('Command registration exited with code ' + $LASTEXITCODE) }
    } finally {
      Pop-Location
      $env:VSM_PROJECT_BOUNDARY = $vsmPriorBoundary
      $env:VSM_MCP_EXPECT_MODE = $vsmPriorExpectedMode
    }
    # Applies immediately when the installer is run in the current PowerShell.
    $vsmProcessParts = @($env:Path -split ';' | Where-Object { $_ -ne '' -and $_.Trim('"').TrimEnd('\','/') -ine $vsmResolvedDir.TrimEnd('\','/') })
    $env:Path = (@($vsmResolvedDir) + $vsmProcessParts) -join ';'
    Write-Output 'The vsm command is ready. Language activation: vsm path-init'
  } catch {
    Write-Warning ('VSM was installed, but automatic command registration failed: ' + $_.Exception.Message)
    Write-Warning ('Retry: & "' + $vsmTarget + '" ' + ($vsmRegistrationArgs -join ' '))
  }
  if ($vsmBackup) { Write-Output ('Previous executable: ' + $vsmBackup) }
} finally {
  # Only remove the two known staging files; never recursively remove paths.
  foreach ($vsmName in @($vsmAsset, 'SHA256SUMS')) {
    $vsmTemporary = Join-Path $vsmStage $vsmName
    if (Test-Path -LiteralPath $vsmTemporary) { Remove-Item -LiteralPath $vsmTemporary -Force }
  }
  [IO.Directory]::Delete($vsmStage, $false)
}
