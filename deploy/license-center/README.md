# CMDB 授权签发中心

授权签发中心是独立管理应用，不随正式运维门户一起对外开放。它持有 Ed25519
签发私钥，平台网关只内置公钥，因此客户环境无法自行伪造商业许可证。

## 1. 生成签发密钥

在仓库根目录执行：

```powershell
cd apps/gateway-bff
go run ./cmd/license-keygen `
  -private D:\Devops\CMDB\.license-keys\license-private.pem `
  -public D:\Devops\CMDB\.license-keys\license-public.pem
```

私钥只能保存在授权签发环境，不得提交 Git，也不得挂载到正式平台网关。
仓库已忽略 `.license-keys/`。

首次生成后需要将公钥写入
`apps/gateway-bff/internal/license/service.go` 的
`defaultPublicKeyPEM`，或通过网关环境变量 `LICENSE_PUBLIC_KEY` 注入。

## 2. Docker Compose 启动

创建本地环境文件：

```bash
cat > deploy/license-center/.env <<'EOF'
LICENSE_CENTER_ADMIN_USERNAME=license-admin
LICENSE_CENTER_ADMIN_PASSWORD=replace-with-a-strong-password
LICENSE_CENTER_PORT=8090
EOF
docker compose -f deploy/license-center/docker-compose.yml up -d --build
```

访问 `http://服务器IP:8090`。

## 3. Kubernetes 部署

先创建两个 Secret：

```bash
kubectl -n ops-qa create secret generic license-center-env \
  --from-literal=LICENSE_CENTER_ADMIN_USERNAME=license-admin \
  --from-literal=LICENSE_CENTER_ADMIN_PASSWORD='replace-with-a-strong-password'

kubectl -n ops-qa create secret generic license-center-signing \
  --from-file=license-signing-key=.license-keys/license-private.pem
```

构建并推送镜像后应用清单：

```bash
docker build -f apps/gateway-bff/Dockerfile.license-center \
  -t harbor.example.com/cmdb/license-center:1.0.0 apps/gateway-bff
kubectl apply -f deploy/k8s/license-center.yaml
```

需要重新读取管理员密码时：

```bash
kubectl -n ops-qa get secret license-center-env \
  -o jsonpath='{.data.LICENSE_CENTER_ADMIN_PASSWORD}' | base64 -d
```

清单使用 NodePort `30081`。生产环境建议改为内网 Ingress、HTTPS 和 IP 白名单。

## 4. 签发流程

1. 客户管理员在平台“系统管理 → 授权与用量”下载授权申请文件并取得 Deployment ID；
2. 授权管理员进入签发中心，填写客户名称、Deployment ID、资产额度和有效期；
3. 签发中心生成以 `CMDB-LIC-v1.` 开头的离线许可证；
4. 客户将许可证粘贴回平台“授权与用量”，点击“验证并激活”；
5. 平台校验签名、部署身份和有效期，并把授权状态持久化到 PostgreSQL。

许可证记录默认保存在 `/data/licenses.json`。Kubernetes 清单使用单独 PVC；
Docker Compose 使用命名卷。

## 5. 资产额度规则

- 免费社区版：最多 200 个资产；
- 商业授权：额度必须大于 200；
- 新资产插入时由 PostgreSQL advisory lock 锁定并重新计数；
- 已有资产编辑、删除、查询不受限制；
- 授权到期后默认有 30 天宽限期，可用 `LICENSE_GRACE_DAYS` 调整；
- 额度不足不会删除已有数据，只阻止新增资产。
