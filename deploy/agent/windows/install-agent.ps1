param(
  [Parameter(Mandatory=$true)][string]$GatewayURL,
  [Parameter(Mandatory=$true)][string]$AgentToken,
  [string]$InstallDir = 'C:\Program Files\CMDB Agent',
  [string]$SystemDrive = 'C:',
  [ValidateSet('','physical-server','virtual-machine')][string]$AssetType = ''
)
$ErrorActionPreference = 'Stop'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
if (-not ([Security.Principal.WindowsPrincipal]$identity).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw '请使用管理员 PowerShell 执行' }
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item -LiteralPath "$PSScriptRoot\cmdb-agent.ps1" -Destination "$InstallDir\cmdb-agent.ps1" -Force
@{ GatewayURL=$GatewayURL; AgentToken=$AgentToken; SystemDrive=$SystemDrive; AssetType=$AssetType } | ConvertTo-Json | Set-Content -LiteralPath "$InstallDir\agent.json" -Encoding UTF8
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -ExecutionPolicy Bypass -File `"$InstallDir\cmdb-agent.ps1`""
$trigger = New-ScheduledTaskTrigger -Once -At (Get-Date).AddMinutes(1) -RepetitionInterval (New-TimeSpan -Minutes 1) -RepetitionDuration ([TimeSpan]::MaxValue)
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName 'CMDB-Agent' -Action $action -Trigger $trigger -Settings $settings -Description 'CMDB Windows Agent heartbeat and inventory reporter' -Force | Out-Null
Start-ScheduledTask -TaskName 'CMDB-Agent'
Get-ScheduledTask -TaskName 'CMDB-Agent' | Select-Object TaskName,State
