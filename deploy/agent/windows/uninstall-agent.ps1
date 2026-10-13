$ErrorActionPreference = 'Stop'
Unregister-ScheduledTask -TaskName 'CMDB-Agent' -Confirm:$false -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force 'C:\Program Files\CMDB Agent' -ErrorAction SilentlyContinue
Write-Output 'CMDB Windows Agent removed'
