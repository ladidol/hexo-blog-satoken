package article

import "blog-go/internal/platform"

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	handlers := map[string]platform.Endpoint{
		"ListArchives": s.ListArchives, "ListArticles": s.ListArticles,
		"ListArticleBacks": s.ListArticleBacks, "SaveOrUpdateArticle": s.SaveOrUpdateArticle,
		"UpdateArticleTop": s.UpdateArticleTop, "UpdateArticleDelete": s.UpdateArticleDelete,
		"DeleteArticles": s.DeleteArticles, "GetArticleBackById": s.GetArticleBackById,
		"GetArticleById": s.GetArticleById, "ListArticlesByCondition": s.ListArticlesByCondition,
		"ListArticlesBySearch": s.ListArticlesBySearch, "SaveArticleLike": s.SaveArticleLike,
		"ExportArticles": s.ExportArticles, "ImportArticles": s.ImportArticles,
	}
	routes := make([]platform.Route, 0, len(RouteSpecs))
	for _, spec := range RouteSpecs {
		routes = append(routes, platform.Route{Spec: spec, Handle: handlers[spec.Operation]})
	}
	return routes
}

// 用途：作为文章、检索与文章文件交换路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "GET", Path: "/articles/archives", Operation: "ListArchives", Policy: "public", Audit: false},
	{Method: "GET", Path: "/articles", Operation: "ListArticles", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/articles", Operation: "ListArticleBacks", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/articles", Operation: "SaveOrUpdateArticle", Policy: "admin", Audit: true},
	{Method: "PUT", Path: "/admin/articles/top", Operation: "UpdateArticleTop", Policy: "admin", Audit: true},
	{Method: "PUT", Path: "/admin/articles", Operation: "UpdateArticleDelete", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/articles", Operation: "DeleteArticles", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/admin/articles/{articleId}", Operation: "GetArticleBackById", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/articles/{articleId}", Operation: "GetArticleById", Policy: "public", Audit: false},
	{Method: "GET", Path: "/articles/condition", Operation: "ListArticlesByCondition", Policy: "public", Audit: false},
	{Method: "GET", Path: "/articles/search", Operation: "ListArticlesBySearch", Policy: "public", Audit: false},
	{Method: "POST", Path: "/articles/{articleId}/like", Operation: "SaveArticleLike", Policy: "user", Audit: false},
	{Method: "POST", Path: "/admin/articles/export", Operation: "ExportArticles", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/articles/import", Operation: "ImportArticles", Policy: "admin", Audit: false},
}

// P-031 HTTP GET /articles/archives

// P-032 HTTP GET /articles

// P-033 HTTP GET /admin/articles

// P-034 HTTP POST /admin/articles

// P-035 HTTP PUT /admin/articles/top

// P-036 HTTP PUT /admin/articles

// P-037 HTTP DELETE /admin/articles

// P-038 HTTP GET /admin/articles/{articleId}

// P-039 HTTP GET /articles/{articleId}

// P-040 HTTP GET /articles/condition

// P-041 HTTP GET /articles/search

// P-042 HTTP POST /articles/{articleId}/like

// P-043 HTTP POST /admin/articles/export

// P-044 HTTP POST /admin/articles/import
