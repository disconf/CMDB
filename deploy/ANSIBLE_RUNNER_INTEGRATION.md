# Ansible Runner 执行服务接入约定

网关默认使用安全模拟执行器。只有配置 `ANSIBLE_RUNNER_URL` 后，任务才会发送到真实执行服务。

执行服务需要提供 `POST /api/v1/run`，接受 Bearer Token，并接收任务ID、受控命令、CMDB资产ID、操作人、执行次数和超时时间。返回格式：

```json
{
  "status": "success",
  "message": "playbook completed",
  "events": [
    {"progress": 20, "level": "info", "message": "inventory resolved"},
    {"progress": 100, "level": "success", "message": "all hosts completed"}
  ]
}
```

允许的最终状态为 `success`、`failed`、`cancelled`。网关超时或用户取消时会取消HTTP请求，Runner服务应监听请求上下文并停止对应的 ansible-runner 进程。

建议Runner服务负责：根据CMDB资产ID生成临时Inventory、从Vault读取SSH凭据、限制Playbook和模块白名单、隔离运行目录、清理临时文件，并将完整输出归档至MinIO。
