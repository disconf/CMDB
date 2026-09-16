package httpapi

import (
	"net/http"
	"strings"

	"cmdb/gateway-bff/internal/assetgovernance"
	"cmdb/gateway-bff/internal/auth"
	"cmdb/gateway-bff/internal/cmdb"
)

func assetScopeForUser(user auth.User) cmdb.AssetQuery {
	switch strings.TrimSpace(user.DataScope) {
	case "all":
		return cmdb.AssetQuery{ScopeAll: true}
	case "none":
		return cmdb.AssetQuery{ScopeEmpty: true}
	default:
		if containsUserRole(user.Roles, "platform-admin") {
			return cmdb.AssetQuery{ScopeAll: true}
		}
		if len(user.ProjectGroups) == 0 && len(user.ScopeTags) == 0 {
			return cmdb.AssetQuery{ScopeEmpty: true}
		}
		return cmdb.AssetQuery{ScopeProjectGroups: append([]string(nil), user.ProjectGroups...), ScopeTags: append([]string(nil), user.ScopeTags...)}
	}
}

func assetQueryForRequest(r *http.Request, service *auth.Service) (cmdb.AssetQuery, auth.User, bool) {
	user, err := service.CurrentUser(bearerToken(r))
	if err != nil {
		return cmdb.AssetQuery{}, auth.User{}, false
	}
	return assetScopeForUser(user), user, true
}

func canAccessAsset(user auth.User, asset cmdb.Asset) bool {
	return cmdb.AssetWithinScope(asset, assetScopeForUser(user))
}

func authorizeAssetAccess(w http.ResponseWriter, r *http.Request, service *auth.Service, asset cmdb.Asset) (auth.User, bool) {
	user, err := service.CurrentUser(bearerToken(r))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED", "message": "登录状态已失效"})
		return auth.User{}, false
	}
	if !canAccessAsset(user, asset) {
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "DATA_SCOPE_FORBIDDEN", "message": "没有该资产所在项目组的数据权限"})
		return auth.User{}, false
	}
	return user, true
}

func containsUserRole(roles []string, target string) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}

func filterGovernanceIssues(items []assetgovernance.Issue, cmdbService *cmdb.Service, scope cmdb.AssetQuery) []assetgovernance.Issue {
	result := make([]assetgovernance.Issue, 0, len(items))
	for _, item := range items {
		asset, err := cmdbService.GetAsset(item.AssetID)
		if err != nil || !cmdb.AssetWithinScope(asset, scope) {
			continue
		}
		result = append(result, item)
	}
	return result
}

func governanceAssetsAllowed(ids []string, cmdbService *cmdb.Service, scope cmdb.AssetQuery) bool {
	for _, id := range ids {
		asset, err := cmdbService.GetAsset(id)
		if err != nil || !cmdb.AssetWithinScope(asset, scope) {
			return false
		}
	}
	return true
}

func hasGlobalAssetScope(user auth.User) bool {
	scope := assetScopeForUser(user)
	return scope.ScopeAll
}

func requireGlobalAssetScope(w http.ResponseWriter, r *http.Request, service *auth.Service) (auth.User, bool) {
	user, err := service.CurrentUser(bearerToken(r))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "UNAUTHORIZED"})
		return auth.User{}, false
	}
	if !hasGlobalAssetScope(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "GLOBAL_SCOPE_REQUIRED", "message": "该操作需要全部资产数据权限"})
		return auth.User{}, false
	}
	return user, true
}
