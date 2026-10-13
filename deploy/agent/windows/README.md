# CMDB Windows Agent

Windows Agent 使用 PowerShell + 计划任务实现，不依赖 WSL 或额外运行时。

## 安装

以管理员 PowerShell 执行：

```powershell
.\install-agent.ps1 -GatewayURL http://172.28.69.164:30080 -AgentToken YOUR_TOKEN -SigningKey YOUR_SIGNING_KEY
```

可选参数：

- `-SystemDrive C:`：磁盘容量采集盘符。
- `-AssetType physical-server|virtual-machine`：强制资产类型；默认自动识别。
- `-SigningKey`：可选，配置后与网关 `AGENT_SIGNING_KEY` 对齐并启用 HMAC 上报签名。

安装后会每分钟采集一次主机信息并调用 `/api/v1/agent/report`。

## 卸载

```powershell
.\uninstall-agent.ps1
```
