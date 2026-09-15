# Ansible Runner 执行服务接入约定

网关默认使用安全模拟执行器。只有配置 `ANSIBLE_RUNNER_URL` 后，任务才会发送到真实执行服务。网关同时提供 `GET /internal/runner/cmdb/assets/{id}`，供Runner按资产ID读取IP和SSH端口；该接口只接受与 `ANSIBLE_RUNNER_TOKEN` 相同的Bearer Token。

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

Runner服务负责：根据CMDB资产ID调用内部接口生成临时Inventory、从挂载的Secret/Vault读取SSH凭据、限制Playbook和模块白名单、隔离运行目录、清理临时文件，并将完整输出归档至MinIO。Runner env中的 `CMDB_INTERNAL_TOKEN` 必须与网关的 `ANSIBLE_RUNNER_TOKEN` 一致；优先使用专用SSH密钥，密码仅作为受限环境的回退方式。
