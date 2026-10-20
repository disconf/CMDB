package system

import (
	"sort"
	"strings"
)

type Permission struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Module      string `json:"module"`
	Action      string `json:"action"`
	Risk        string `json:"risk"`
	Description string `json:"description"`
}

var permissionCatalog = []Permission{
	{Code: "dashboard:view", Name: "查看全局态势", Module: "全局态势", Action: "view", Risk: "low", Description: "查看平台总览、容量和健康度"},
	{Code: "cmdb:view", Name: "查看资产", Module: "CMDB", Action: "view", Risk: "low", Description: "查看资产、机房和 K8s 资产"},
	{Code: "cmdb:manage", Name: "管理资产", Module: "CMDB", Action: "manage", Risk: "medium", Description: "新增、编辑、导入、删除和治理资产"},
	{Code: "discovery:view", Name: "查看发现", Module: "Agent 与发现", Action: "view", Risk: "low", Description: "查看 Agent、发现任务、凭证和采集计划"},
	{Code: "discovery:manage", Name: "管理发现", Module: "Agent 与发现", Action: "manage", Risk: "high", Description: "执行发现、部署 Agent、维护凭证和采集计划"},
	{Code: "topology:view", Name: "查看拓扑", Module: "拓扑中心", Action: "view", Risk: "low", Description: "查看业务、网络和 CI/CD 拓扑"},
	{Code: "monitor:view", Name: "查看监控", Module: "监控告警", Action: "view", Risk: "low", Description: "查看指标、告警和监控目标"},
	{Code: "monitor:manage", Name: "管理监控", Module: "监控告警", Action: "manage", Risk: "high", Description: "管理规则、静默、路由和通知渠道"},
	{Code: "job:view", Name: "查看任务", Module: "任务管理", Action: "view", Risk: "low", Description: "查看模板、执行记录和日志"},
	{Code: "job:manage", Name: "管理任务", Module: "任务管理", Action: "manage", Risk: "critical", Description: "创建、执行、审批和调度任务"},
	{Code: "ticket:view", Name: "查看工单", Module: "工单中心", Action: "view", Risk: "low", Description: "查看工单、SLA 和处理记录"},
	{Code: "ticket:manage", Name: "管理工单", Module: "工单中心", Action: "manage", Risk: "medium", Description: "创建、分派、审批和关闭工单"},
	{Code: "release:view", Name: "查看发布", Module: "CI/CD 与发布", Action: "view", Risk: "low", Description: "查看流水线、制品和发布状态"},
	{Code: "release:manage", Name: "管理发布", Module: "CI/CD 与发布", Action: "manage", Risk: "critical", Description: "创建、审批、执行和回滚发布"},
	{Code: "toolbox:use", Name: "使用排查工具", Module: "排查工具", Action: "execute", Risk: "medium", Description: "使用只读诊断和网络排查工具"},
	{Code: "aiops:view", Name: "查看 AIOps", Module: "AI 运维", Action: "view", Risk: "low", Description: "查看分析、Runbook 和知识库"},
	{Code: "aiops:execute", Name: "执行 AIOps", Module: "AI 运维", Action: "execute", Risk: "high", Description: "生成分析、发布知识和执行受控 Runbook"},
	{Code: "knowledge:view", Name: "查看知识库", Module: "知识库", Action: "view", Risk: "low", Description: "查看有权访问的知识空间和文档"},
	{Code: "knowledge:create", Name: "创建知识", Module: "知识库", Action: "create", Risk: "low", Description: "创建目录和知识文档"},
	{Code: "knowledge:edit", Name: "编辑知识", Module: "知识库", Action: "edit", Risk: "medium", Description: "编辑有权访问的知识文档"},
	{Code: "knowledge:publish", Name: "发布知识", Module: "知识库", Action: "publish", Risk: "medium", Description: "发布、归档和还原知识文档"},
	{Code: "knowledge:delete", Name: "删除知识", Module: "知识库", Action: "delete", Risk: "high", Description: "删除有权管理的知识文档"},
	{Code: "knowledge:manage_acl", Name: "管理知识权限", Module: "知识库", Action: "manage", Risk: "high", Description: "修改目录和文档 ACL"},
	{Code: "knowledge:manage_folder", Name: "管理知识目录", Module: "知识库", Action: "manage", Risk: "medium", Description: "创建、移动和删除知识目录"},
	{Code: "knowledge:manage", Name: "管理知识库", Module: "知识库", Action: "manage", Risk: "high", Description: "管理全部知识空间和目录"},
	{Code: "system:manage", Name: "管理系统", Module: "系统管理", Action: "manage", Risk: "critical", Description: "管理用户、角色、模块、参数和审计"},
}

func Permissions() []Permission {
	result := append([]Permission(nil), permissionCatalog...)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Module != result[j].Module {
			return result[i].Module < result[j].Module
		}
		return result[i].Code < result[j].Code
	})
	return result
}

func validPermission(code string) bool {
	for _, item := range permissionCatalog {
		if item.Code == code {
			return true
		}
	}
	return false
}

func normalizePermissions(values []string, allowWildcard bool) ([]string, error) {
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if value == "*" && allowWildcard {
			return []string{"*"}, nil
		}
		if !validPermission(value) {
			return nil, ErrValidation
		}
		result = mergeValues(result, []string{value})
	}
	sort.Strings(result)
	return result, nil
}
