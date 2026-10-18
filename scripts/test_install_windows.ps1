$ErrorActionPreference = 'Stop'
$vsmTestScript = Join-Path $PSScriptRoot 'install.ps1'
function Test-VsmInstaller([string]$Mode) {
  $vsmTestDir = Join-Path ([IO.Path]::GetTempPath()) ('vsm-install-test-' + [Guid]::NewGuid().ToString('N'))
  [IO.Directory]::CreateDirectory($vsmTestDir) | Out-Null
  $vsmTestTarget = Join-Path $vsmTestDir 'vsm.exe'
  [IO.File]::WriteAllText($vsmTestTarget, 'old-program')
  $vsmTestBytes = [Text.Encoding]::UTF8.GetBytes('new-program')
  $vsmTestHash = [BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash($vsmTestBytes)).Replace('-', '').ToLowerInvariant()
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
  try { & $vsmTestScript -Version v1.0.8 -InstallDir $vsmTestDir } catch { $vsmTestFailed = $true }
  if ($Mode -eq 'success') {
    if ($vsmTestFailed -or [IO.File]::ReadAllText($vsmTestTarget) -ne 'new-program') { throw 'Installer did not install verified program' }
  } else {
    if (-not $vsmTestFailed -or [IO.File]::ReadAllText($vsmTestTarget) -ne 'old-program') { throw ('Installer damaged existing program: ' + $Mode) }
  }
}
foreach ($vsmTestMode in @('success','bad-checksum','rollback')) { Test-VsmInstaller $vsmTestMode }
Write-Output 'Windows installer checksum and rollback checks passed.'
