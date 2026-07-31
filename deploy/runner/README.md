# Ansible Runner Service

1. 复制 `deploy/runner.env.example` 为 `deploy/runner.env`，填写随机服务令牌和用于读取CMDB的Keycloak服务账号Token。
2. 在 `deploy/runner` 执行 `docker compose up -d --build`。
3. 网关配置相同的令牌，并设置 `ANSIBLE_RUNNER_URL=http://宿主机地址:8090`。
4. 重启网关，访问 `/api/v1/jobs/executor`，确认模式为 `ansible-runner`。

## SSH安全准备

为Runner创建独立低权限账号和专用Ed25519密钥，不要复用root密钥。将私钥放到服务器 `/opt/cmdb/secrets/runner_id_ed25519`，权限设为 `0600`。使用 `ssh-keyscan` 预先收集每台受管主机的指纹到 `/opt/cmdb/secrets/runner_known_hosts`，并人工核对指纹。服务强制启用 `StrictHostKeyChecking=yes`，不会自动接受陌生主机。

如果Playbook使用Ansible Vault，将密码文件作为额外只读Secret挂载，并配置容器内的 `VAULT_PASSWORD_FILE`。禁止把Vault密码直接写入环境变量。

配置 `S3_ENDPOINT_URL` 后，Runner会将每次执行目录压缩并写入 `jobs/<任务ID>/attempt-<次数>.zip`。建议为Runner分配只允许写入该Bucket的MinIO账号。

默认只开放健康巡检Playbook。新增能力时，先把Playbook加入镜像，再在 `RUNNER_COMMAND_MAP_JSON` 中添加白名单映射。
