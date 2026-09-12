package catalog

import "blog-go/internal/platform"

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	handlers := map[string]platform.Endpoint{
		"ListCategories": s.ListCategories, "ListBackCategories": s.ListBackCategories,
		"ListCategoriesBySearch": s.ListCategoriesBySearch, "SaveOrUpdateCategory": s.SaveOrUpdateCategory,
		"DeleteCategories": s.DeleteCategories, "ListFriendLinks": s.ListFriendLinks,
		"ListFriendLinkDTO": s.ListFriendLinkDTO, "SaveOrUpdateFriendLink": s.SaveOrUpdateFriendLink,
		"DeleteFriendLink": s.DeleteFriendLink, "ListTags": s.ListTags,
		"ListTagBackDTO": s.ListTagBackDTO, "ListTagsBySearch": s.ListTagsBySearch,
		"SaveOrUpdateTag": s.SaveOrUpdateTag, "DeleteTag": s.DeleteTag,
	}
	routes := make([]platform.Route, 0, len(RouteSpecs))
	for _, spec := range RouteSpecs {
		routes = append(routes, platform.Route{Spec: spec, Handle: handlers[spec.Operation]})
	}
	return routes
}

// 用途：作为分类、标签与友链路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "GET", Path: "/categories", Operation: "ListCategories", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/categories", Operation: "ListBackCategories", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/categories/search", Operation: "ListCategoriesBySearch", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/categories", Operation: "SaveOrUpdateCategory", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/categories", Operation: "DeleteCategories", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/links", Operation: "ListFriendLinks", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/links", Operation: "ListFriendLinkDTO", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/links", Operation: "SaveOrUpdateFriendLink", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/links", Operation: "DeleteFriendLink", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/tags", Operation: "ListTags", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/tags", Operation: "ListTagBackDTO", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/tags/search", Operation: "ListTagsBySearch", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/tags", Operation: "SaveOrUpdateTag", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/tags", Operation: "DeleteTag", Policy: "admin", Audit: true},
}

// P-045 HTTP GET /categories

// P-046 HTTP GET /admin/categories

// P-047 HTTP GET /admin/categories/search

// P-048 HTTP POST /admin/categories

// P-049 HTTP DELETE /admin/categories

// P-050 HTTP GET /links

// P-051 HTTP GET /admin/links

// P-052 HTTP POST /admin/links

// P-053 HTTP DELETE /admin/links

// P-054 HTTP GET /tags

// P-055 HTTP GET /admin/tags

// P-056 HTTP GET /admin/tags/search

// P-057 HTTP POST /admin/tags

// P-058 HTTP DELETE /admin/tags
