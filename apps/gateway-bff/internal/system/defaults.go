package system

func defaultUsers() []User {
	return []User{
		{ID: "u-admin", Username: "admin", DisplayName: "运维管理员", Department: "平台运维部", Roles: []string{"platform-admin"}, Status: "active", LastLogin: "15:12", DataScope: "all", ProjectGroups: []string{"华东业务组", "核心系统组"}},
		{ID: "u-viewer", Username: "viewer", DisplayName: "只读用户", Department: "研发中心", Roles: []string{"viewer"}, Status: "active", LastLogin: "昨天 18:20", DataScope: "groups", ProjectGroups: []string{"华东业务组"}},
		{ID: "u-ops-01", Username: "zhangwei", DisplayName: "张伟", Department: "平台运维部", Roles: []string{"operator"}, Status: "active", LastLogin: "14:58", DataScope: "groups", ProjectGroups: []string{"核心系统组"}},
		{ID: "u-dba-01", Username: "wutao", DisplayName: "吴涛", Department: "数据库组", Roles: []string{"dba", "approver"}, Status: "active", LastLogin: "14:32", DataScope: "groups", ProjectGroups: []string{"核心系统组"}},
		{ID: "u-dev-01", Username: "wangqiang", DisplayName: "王强", Department: "电商研发部", Roles: []string{"developer"}, Status: "locked", LastLogin: "13:06", DataScope: "groups", ProjectGroups: []string{"电商业务组"}},
	}
}

func defaultRoles() []Role {
	return []Role{
		{ID: "platform-admin", Name: "平台管理员", Description: "拥有所有平台管理权限", Users: 1, Permissions: []string{"*"}, DataScope: "all"},
		{ID: "operator", Name: "运维操作员", Description: "资产、监控、任务与工具箱操作", Users: 8, Permissions: []string{"cmdb:view", "monitor:manage", "job:manage", "toolbox:use"}, DataScope: "groups"},
		{ID: "viewer", Name: "只读用户", Description: "查看大屏、资产、拓扑和监控", Users: 12, Permissions: []string{"dashboard:view", "cmdb:view", "topology:view", "monitor:view"}, DataScope: "groups"},
		{ID: "approver", Name: "审批人", Description: "工单与发布审批", Users: 4, Permissions: []string{"ticket:manage", "release:view"}, DataScope: "groups"},
	}
}

func defaultModules() []Module {
	return []Module{
		{Code: "dashboard", Name: "全局态势", Enabled: true, Order: 1, Permission: "dashboard:view"},
		{Code: "cmdb", Name: "CMDB 资产", Enabled: true, Order: 2, Permission: "cmdb:view"},
		{Code: "topology", Name: "拓扑中心", Enabled: true, Order: 3, Permission: "topology:view"},
		{Code: "monitor", Name: "监控告警", Enabled: true, Order: 4, Permission: "monitor:view"},
		{Code: "jobs", Name: "任务管理", Enabled: true, Order: 5, Permission: "job:view"},
		{Code: "aiops", Name: "AI 运维", Enabled: true, Order: 10, Permission: "aiops:view"},
	}
}

func defaultAudits() []Audit {
	return []Audit{
		{ID: "audit-001", Time: "15:12:06", Actor: "admin", Action: "用户登录", Resource: "管理后台", IP: "10.20.1.8", Result: "success", Detail: "认证成功"},
		{ID: "audit-002", Time: "14:58:22", Actor: "zhangwei", Action: "执行诊断", Resource: "prod-api-01", IP: "10.20.1.21", Result: "success", Detail: "主机健康检查"},
	}
}

func defaultSettings() []Setting {
	return []Setting{
		{Key: "session.timeout", Name: "会话超时", Value: "30", Category: "安全", Description: "无操作自动退出分钟数"},
		{Key: "audit.retention", Name: "审计保留期", Value: "180", Category: "审计", Description: "审计日志保留天数"},
		{Key: "discovery.interval", Name: "发现周期", Value: "15", Category: "采集", Description: "自动发现间隔分钟数"},
		{Key: "alert.silence.max", Name: "最大静默时间", Value: "24", Category: "告警", Description: "告警最大静默小时数"},
	}
}
