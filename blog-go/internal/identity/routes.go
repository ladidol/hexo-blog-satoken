package identity

import "blog-go/internal/platform"

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	handlers := map[string]platform.Endpoint{
		"Login": s.Login, "Logout": s.Logout, "WeiboLogin": s.WeiboLogin, "QqLogin": s.QqLogin,
		"ListMenus": s.ListMenus, "SaveOrUpdateMenu": s.SaveOrUpdateMenu, "DeleteMenu": s.DeleteMenu, "ListMenuOptions": s.ListMenuOptions, "ListUserMenus": s.ListUserMenus,
		"ListResources": s.ListResources, "DeleteResource": s.DeleteResource, "SaveOrUpdateResource": s.SaveOrUpdateResource, "ListResourceOption": s.ListResourceOption,
		"ListUserRoles": s.ListUserRoles, "ListRoles": s.ListRoles, "SaveOrUpdateRole": s.SaveOrUpdateRole, "DeleteRoles": s.DeleteRoles,
		"SendCode": s.SendCode, "ListUsers": s.ListUsers, "Register": s.Register, "UpdatePassword": s.UpdatePassword, "UpdateAdminPassword": s.UpdateAdminPassword,
		"UpdateUserInfo": s.UpdateUserInfo, "UpdateUserAvatar": s.UpdateUserAvatar, "SaveUserEmail": s.SaveUserEmail, "UpdateUserRole": s.UpdateUserRole, "UpdateUserDisable": s.UpdateUserDisable, "ListOnlineUsers": s.ListOnlineUsers, "RemoveOnlineUser": s.RemoveOnlineUser,
	}
	routes := make([]platform.Route, 0, len(RouteSpecs))
	for _, spec := range RouteSpecs {
		routes = append(routes, platform.Route{Spec: spec, Handle: handlers[spec.Operation]})
	}
	return routes
}

// 用途：作为账号、资料与管理授权路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "POST", Path: "/login", Operation: "Login", Policy: "public", Audit: false},
	{Method: "ANY", Path: "/logout", Operation: "Logout", Policy: "public", Audit: false},
	{Method: "POST", Path: "/users/oauth/weibo", Operation: "WeiboLogin", Policy: "public", Audit: false},
	{Method: "POST", Path: "/users/oauth/qq", Operation: "QqLogin", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/menus", Operation: "ListMenus", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/menus", Operation: "SaveOrUpdateMenu", Policy: "admin", Audit: false},
	{Method: "DELETE", Path: "/admin/menus/{menuId}", Operation: "DeleteMenu", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/role/menus", Operation: "ListMenuOptions", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/user/menus", Operation: "ListUserMenus", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/resources", Operation: "ListResources", Policy: "admin", Audit: false},
	{Method: "DELETE", Path: "/admin/resources/{resourceId}", Operation: "DeleteResource", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/resources", Operation: "SaveOrUpdateResource", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/role/resources", Operation: "ListResourceOption", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/users/role", Operation: "ListUserRoles", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/roles", Operation: "ListRoles", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/role", Operation: "SaveOrUpdateRole", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/roles", Operation: "DeleteRoles", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/users/code", Operation: "SendCode", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/users", Operation: "ListUsers", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/register", Operation: "Register", Policy: "public", Audit: false},
	{Method: "PUT", Path: "/users/password", Operation: "UpdatePassword", Policy: "public", Audit: false},
	{Method: "PUT", Path: "/admin/users/password", Operation: "UpdateAdminPassword", Policy: "admin", Audit: false},
	{Method: "PUT", Path: "/users/info", Operation: "UpdateUserInfo", Policy: "user", Audit: false},
	{Method: "POST", Path: "/users/avatar", Operation: "UpdateUserAvatar", Policy: "user", Audit: false},
	{Method: "POST", Path: "/users/email", Operation: "SaveUserEmail", Policy: "user", Audit: false},
	{Method: "PUT", Path: "/admin/users/role", Operation: "UpdateUserRole", Policy: "admin", Audit: true},
	{Method: "PUT", Path: "/admin/users/disable", Operation: "UpdateUserDisable", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/admin/users/online", Operation: "ListOnlineUsers", Policy: "admin", Audit: false},
	{Method: "DELETE", Path: "/admin/users/{userInfoId}/online", Operation: "RemoveOnlineUser", Policy: "admin", Audit: false},
}

// P-002 HTTP POST /login
// 响应变化：删除 UserDetailDTO.password，其他字段沿用。

// P-003 HTTP ANY /logout

// P-004 HTTP POST /users/oauth/weibo

// P-005 HTTP POST /users/oauth/qq

// P-006 HTTP GET /admin/menus

// P-007 HTTP POST /admin/menus

// P-008 HTTP DELETE /admin/menus/{menuId}

// P-009 HTTP GET /admin/role/menus

// P-010 HTTP GET /admin/user/menus

// P-011 HTTP GET /admin/resources

// P-012 HTTP DELETE /admin/resources/{resourceId}

// P-013 HTTP POST /admin/resources

// P-014 HTTP GET /admin/role/resources

// P-015 HTTP GET /admin/users/role

// P-016 HTTP GET /admin/roles

// P-017 HTTP POST /admin/role

// P-018 HTTP DELETE /admin/roles

// P-019 HTTP GET /users/code

// P-020 HTTP GET /admin/users

// P-021 HTTP POST /register

// P-022 HTTP PUT /users/password

// P-023 HTTP PUT /admin/users/password

// P-024 HTTP PUT /users/info

// P-025 HTTP POST /users/avatar

// P-026 HTTP POST /users/email

// P-027 HTTP PUT /admin/users/role

// P-028 HTTP PUT /admin/users/disable

// P-029 HTTP GET /admin/users/online

// P-030 HTTP DELETE /admin/users/{userInfoId}/online
