param([string]$ConfigPath = "$PSScriptRoot\agent.json")
$ErrorActionPreference = 'Stop'
$config = Get-Content -LiteralPath $ConfigPath -Raw | ConvertFrom-Json
$instance = Get-CimInstance Win32_ComputerSystem
$os = Get-CimInstance Win32_OperatingSystem
$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$disk = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='$($config.SystemDrive)'" | Select-Object -First 1
$virtualization = if ($instance.Model -match 'VMware|Virtual|KVM|Hyper-V|VirtualBox') { $instance.Model } else { 'none' }
$assetType = if ($virtualization -eq 'none') { 'physical-server' } else { 'virtual-machine' }
$ips = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object { $_.IPAddress -notlike '127.*' -and $_.PrefixOrigin -ne 'WellKnown' } | Select-Object -ExpandProperty IPAddress
$payload = @{
  agentId = $env:COMPUTERNAME
  hostname = $env:COMPUTERNAME
  type = if ($config.AssetType) { $config.AssetType } else { $assetType }
  ip = ($ips | Select-Object -First 1)
  os = [string]$os.Caption
  kernel = [string]$os.Version
  architecture = $env:PROCESSOR_ARCHITECTURE
  cpuCount = [int]$instance.NumberOfLogicalProcessors
  memoryBytes = [int64]$instance.TotalPhysicalMemory
  diskBytes = if ($disk) { [int64]$disk.Size } else { 0 }
  bootTime = ([Management.ManagementDateTimeConverter]::ToDateTime($os.LastBootUpTime)).ToUniversalTime().ToString('o')
  version = 'windows-1.0.0'
  virtualization = $virtualization
} | ConvertTo-Json -Compress
$headers = @{ Authorization = "Bearer $($config.AgentToken)"; 'Content-Type' = 'application/json' }
Invoke-RestMethod -Method Post -Uri "$($config.GatewayURL.TrimEnd('/'))/api/v1/agent/report" -Headers $headers -Body $payload -TimeoutSec 30 | Out-Host
