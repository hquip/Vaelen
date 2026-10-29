$ErrorActionPreference = 'Stop'
$vsmTestScript = Join-Path $PSScriptRoot 'install.ps1'
$vsmFixtureDir = Join-Path ([IO.Path]::GetTempPath()) ('vsm-installer-fixture-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($vsmFixtureDir) | Out-Null
$vsmFixtureSource = Join-Path $vsmFixtureDir 'main.go'
[IO.File]::WriteAllText($vsmFixtureSource, @'
package main
import("fmt";"os";"strings")
func main(){
  args:=strings.Join(os.Args[1:]," ")
  expected:="path-init --command-only"
  if os.Getenv("VSM_INSTALL_TEST_LEGACY")=="1" { expected="path-init cmd" }
  if args=="help path-init" { fmt.Println(expected);return }
  if args!=expected { os.Exit(9) }
  if os.WriteFile(os.Getenv("VSM_INSTALL_TEST_MARKER"),[]byte("registered"),0600)!=nil { os.Exit(8) }
  if os.Getenv("VSM_INSTALL_TEST_FAIL")=="1" { os.Exit(7) }
}
'@)
$vsmFixtureExe = Join-Path $vsmFixtureDir 'fixture.exe'
& go build -o $vsmFixtureExe $vsmFixtureSource
if ($LASTEXITCODE -ne 0) { throw 'Failed to build installer CLI fixture.' }
$vsmFixtureBytes = [IO.File]::ReadAllBytes($vsmFixtureExe)
function Test-VsmInstaller([string]$Mode) {
  $vsmTestDir = Join-Path ([IO.Path]::GetTempPath()) ('vsm-install-test-' + [Guid]::NewGuid().ToString('N'))
  [IO.Directory]::CreateDirectory($vsmTestDir) | Out-Null
  $vsmTestTarget = Join-Path $vsmTestDir 'vsm.exe'
  [IO.File]::WriteAllText($vsmTestTarget, 'old-program')
  $vsmTestBytes = $vsmFixtureBytes
  $vsmTestHash = [BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash($vsmTestBytes)).Replace('-', '').ToLowerInvariant()
  $vsmInstalledHash = $vsmTestHash
  if ($Mode -eq 'bad-checksum') { $vsmTestHash = '0' * 64 }
  function Invoke-RestMethod { param($Uri,$Headers,$TimeoutSec)
    if ($Uri -ne 'https://api.github.com/repos/hquip/Vaelen/releases/tags/v1.0.8') { throw 'Unexpected API request' }
    $vsmTestAssetName = 'vsm-windows-amd64.exe'
    try { if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() -eq 'Arm64') { $vsmTestAssetName = 'vsm-windows-arm64.exe' } } catch {}
    return [pscustomobject]@{tag_name='v1.0.8';draft=$false;prerelease=$false;assets=@([pscustomobject]@{name=$vsmTestAssetName;size=$vsmTestBytes.Length;browser_download_url=('https://github.com/hquip/Vaelen/releases/download/v1.0.8/'+$vsmTestAssetName)})}
  }
  function Invoke-WebRequest { param([switch]$UseBasicParsing,$Uri,$OutFile,$TimeoutSec)
    if ($Uri -notlike 'https://github.com/hquip/Vaelen/releases/download/v1.0.8/*') { throw 'Unexpected download' }
    if ($Uri.EndsWith('/SHA256SUMS')) {
      $vsmTestAssetName = 'vsm-windows-amd64.exe'
      try { if ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString() -eq 'Arm64') { $vsmTestAssetName = 'vsm-windows-arm64.exe' } } catch {}
      [IO.File]::WriteAllText($OutFile, $vsmTestHash + '  ' + $vsmTestAssetName)
    } else { [IO.File]::WriteAllBytes($OutFile, $vsmTestBytes) }
  }
  function Move-Item { param($LiteralPath,$Destination)
    if ($Mode -eq 'rollback' -and [IO.Path]::GetFileName($LiteralPath) -like 'vsm-windows-*') { throw 'Simulated replacement failure' }
    Microsoft.PowerShell.Management\Move-Item -LiteralPath $LiteralPath -Destination $Destination
  }
  $vsmTestFailed = $false
  $vsmBeforePath = $env:Path
  $vsmBeforeMarker = $env:VSM_INSTALL_TEST_MARKER
  $vsmBeforeFailure = $env:VSM_INSTALL_TEST_FAIL
  $vsmBeforeLegacy = $env:VSM_INSTALL_TEST_LEGACY
  $vsmBeforeBoundary = $env:VSM_PROJECT_BOUNDARY
  $vsmBeforeExpected = $env:VSM_MCP_EXPECT_MODE
  $vsmBeforeDirectory = (Get-Location).Path
  $vsmTestMarker = Join-Path $vsmTestDir 'registered.txt'
  $env:VSM_INSTALL_TEST_MARKER = $vsmTestMarker
  $env:VSM_INSTALL_TEST_FAIL = if ($Mode -eq 'registration-failure') { '1' } else { '0' }
  $env:VSM_INSTALL_TEST_LEGACY = if ($Mode -eq 'legacy-cli') { '1' } else { '0' }
  try {
    try { & $vsmTestScript -Version v1.0.8 -InstallDir $vsmTestDir } catch { $vsmTestFailed = $true }
    if ($Mode -in @('success','legacy-cli','registration-failure')) {
      if ($vsmTestFailed -or (Get-FileHash -LiteralPath $vsmTestTarget -Algorithm SHA256).Hash -ne $vsmInstalledHash) { throw 'Installer did not install verified program' }
      if (-not (Test-Path -LiteralPath $vsmTestMarker)) { throw 'Installer did not register the installed CLI automatically' }
      if ($Mode -in @('success','legacy-cli') -and -not $env:Path.StartsWith($vsmTestDir + ';')) { throw 'Installer did not make vsm available in the current PowerShell' }
      if ($Mode -eq 'registration-failure' -and $env:Path -ne $vsmBeforePath) { throw 'Failed registration was reported as ready' }
    } else {
      if (-not $vsmTestFailed -or [IO.File]::ReadAllText($vsmTestTarget) -ne 'old-program') { throw ('Installer damaged existing program: ' + $Mode) }
      if (Test-Path -LiteralPath $vsmTestMarker) { throw 'Installer registered an unverified or failed installation' }
    }
    if ((Get-Location).Path -ne $vsmBeforeDirectory -or $env:VSM_PROJECT_BOUNDARY -ne $vsmBeforeBoundary -or $env:VSM_MCP_EXPECT_MODE -ne $vsmBeforeExpected) { throw 'Installer changed the calling project environment' }
  } finally {
    $env:Path = $vsmBeforePath
    $env:VSM_INSTALL_TEST_MARKER = $vsmBeforeMarker
    $env:VSM_INSTALL_TEST_FAIL = $vsmBeforeFailure
    $env:VSM_INSTALL_TEST_LEGACY = $vsmBeforeLegacy
  }
}
foreach ($vsmTestMode in @('success','legacy-cli','bad-checksum','rollback','registration-failure')) { Test-VsmInstaller $vsmTestMode }
Write-Output 'Windows installer checksum, rollback, and automatic command registration checks passed.'
