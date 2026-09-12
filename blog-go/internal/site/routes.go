package site

import "blog-go/internal/platform"

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	handlers := map[string]platform.Endpoint{
		"GetBlogHomeInfo":     s.GetBlogHomeInfo,
		"UpdateWebsiteConfig": s.UpdateWebsiteConfig,
		"GetWebsiteConfig":    s.GetWebsiteConfig,
		"GetAbout":            s.GetAbout,
		"UpdateAbout":         s.UpdateAbout,
		"DeletePage":          s.DeletePage,
		"SaveOrUpdatePage":    s.SaveOrUpdatePage,
		"ListPages":           s.ListPages,
	}
	routes := make([]platform.Route, 0, len(RouteSpecs))
	for _, spec := range RouteSpecs {
		routes = append(routes, platform.Route{Spec: spec, Handle: handlers[spec.Operation]})
	}
	return routes
}

// 用途：作为站点配置、关于与页面路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "GET", Path: "/", Operation: "GetBlogHomeInfo", Policy: "public", Audit: false},
	{Method: "PUT", Path: "/admin/website/config", Operation: "UpdateWebsiteConfig", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/website/config", Operation: "GetWebsiteConfig", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/about", Operation: "GetAbout", Policy: "public", Audit: false},
	{Method: "PUT", Path: "/admin/about", Operation: "UpdateAbout", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/pages/{pageId}", Operation: "DeletePage", Policy: "admin", Audit: true},
	{Method: "POST", Path: "/admin/pages", Operation: "SaveOrUpdatePage", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/admin/pages", Operation: "ListPages", Policy: "admin", Audit: false},
}

// P-092 HTTP GET /

// P-093 HTTP PUT /admin/website/config

// P-094 HTTP GET /admin/website/config

// P-095 HTTP GET /about

// P-096 HTTP PUT /admin/about

// P-097 HTTP DELETE /admin/pages/{pageId}

// P-098 HTTP POST /admin/pages

// P-099 HTTP GET /admin/pages
