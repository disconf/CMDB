# CMDB 智能运维平台

当前版本为 V0.1 本地开发验证版，包含科技感全局态势大屏和 Go 聚合接口。前期不依赖 Docker、Kubernetes、PostgreSQL、Redis 或 Kafka。

## 本地启动

需要 Node.js 24+、npm 11+ 和 Go 1.26+。

启动后端：

```powershell
cd apps/gateway-bff
go run ./cmd/server
```

后端默认监听 `http://localhost:8080`。

另开一个终端启动前端：

```powershell
cd apps/web-portal
npm install
npm run dev
```

浏览器访问 `http://localhost:5173`。Vite 会把 `/api` 请求代理到本地 Go 服务。

本地验证账号：

| 角色 | 用户名 | 密码 | 可见范围 |
|---|---|---|---|
| 平台管理员 | `admin` | `admin123` | 全部 10 个模块 |
| 只读用户 | `viewer` | `viewer123` | 大屏、CMDB、拓扑、监控 |

这些账号只用于无外部依赖的开发验证。接入 Keycloak 后将删除内存账号和密码。

## 验证

```powershell
cd apps/gateway-bff
go test -cover ./...
go vet ./...

cd ../web-portal
npm test
npm run build
```

## 当前功能

- 全局状态、数据中心和管理员信息
- 资产、Agent、告警和任务关键指标
- 数据中心容量与资源健康度
- 项目—应用—服务—实例—基础设施五层拓扑
- 24 小时告警趋势和高优先级告警
- 活动任务、运维时间线和发布状态
- 30 秒自动刷新、失败重试和全屏展示
- 后端健康检查和大屏聚合 API
- 登录、退出和会话恢复
- RBAC 功能权限和动态菜单
- 路由守卫与无权限跳转
- 九个业务模块入口框架
- CMDB 资产状态总览和 CI 模型分类
- 资产台账搜索、类型、状态和项目组筛选
- 资产详情、扩展属性、标签和资产关系
- 新增和编辑资产、字段校验
- 资产变更历史与差异记录
- CSV 批量导入和逐行错误反馈

当前后端使用内存演示数据。后续 CMDB、监控、任务等模块完成后，在服务层替换为真实数据源，前端接口契约保持不变。
