package analytics

import "blog-go/internal/platform"

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	handlers := map[string]platform.Endpoint{
		"GetBlogBackInfo": s.GetBlogBackInfo, "Report": s.Report,
		"ListOperationLogs": s.ListOperationLogs, "DeleteOperationLogs": s.DeleteOperationLogs,
		"ListUserAreas": s.ListUserAreas,
	}
	routes := make([]platform.Route, 0, len(RouteSpecs))
	for _, spec := range RouteSpecs {
		routes = append(routes, platform.Route{Spec: spec, Handle: handlers[spec.Operation]})
	}
	return routes
}

// 用途：作为访问统计、日志与地区补充路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "GET", Path: "/admin", Operation: "GetBlogBackInfo", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/report", Operation: "Report", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/operation/logs", Operation: "ListOperationLogs", Policy: "admin", Audit: false},
	{Method: "DELETE", Path: "/admin/operation/logs", Operation: "DeleteOperationLogs", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/users/area", Operation: "ListUserAreas", Policy: "admin", Audit: false},
}

// P-100 HTTP GET /admin

// P-101 HTTP POST /report

// P-102 HTTP GET /admin/operation/logs

// P-103 HTTP DELETE /admin/operation/logs

// P-104 HTTP GET /admin/users/area

// Frozen from the original controllers' @Api, @ApiOperation and @OptLog.
// Matching includes method, route template and operation; caller text cannot become audit metadata.
type auditAction struct{ operation, module, kind, method, description string }

var auditActions = map[string]auditAction{
	"POST /admin/articles":                  {"SaveOrUpdateArticle", "文章模块", "新增或修改", "org.cuit.epoch.controller.ArticleController.saveOrUpdateArticle", "添加或修改文章"},
	"PUT /admin/articles/top":               {"UpdateArticleTop", "文章模块", "修改", "org.cuit.epoch.controller.ArticleController.updateArticleTop", "修改文章置顶"},
	"PUT /admin/articles":                   {"UpdateArticleDelete", "文章模块", "修改", "org.cuit.epoch.controller.ArticleController.updateArticleDelete", "恢复或删除文章"},
	"DELETE /admin/articles":                {"DeleteArticles", "文章模块", "删除", "org.cuit.epoch.controller.ArticleController.deleteArticles", "物理删除文章"},
	"PUT /admin/about":                      {"UpdateAbout", "博客信息模块", "修改", "org.cuit.epoch.controller.BlogInfoController.updateAbout", "修改关于我信息"},
	"POST /admin/categories":                {"SaveOrUpdateCategory", "分类模块", "新增或修改", "org.cuit.epoch.controller.CategoryController.saveOrUpdateCategory", "添加或修改分类"},
	"DELETE /admin/categories":              {"DeleteCategories", "分类模块", "删除", "org.cuit.epoch.controller.CategoryController.deleteCategories", "删除分类"},
	"PUT /admin/comments/review":            {"UpdateCommentsReview", "评论模块", "删除", "org.cuit.epoch.controller.CommentController.updateCommentsReview", "审核评论"},
	"DELETE /admin/comments":                {"DeleteComments", "评论模块", "删除", "org.cuit.epoch.controller.CommentController.deleteComments", "删除评论"},
	"POST /admin/links":                     {"SaveOrUpdateFriendLink", "友链模块", "新增或修改", "org.cuit.epoch.controller.FriendLinkController.saveOrUpdateFriendLink", "保存或修改友链"},
	"DELETE /admin/links":                   {"DeleteFriendLink", "友链模块", "删除", "org.cuit.epoch.controller.FriendLinkController.deleteFriendLink", "删除友链"},
	"PUT /admin/messages/review":            {"UpdateMessagesReview", "留言模块", "修改", "org.cuit.epoch.controller.MessageController.updateMessagesReview", "审核留言"},
	"DELETE /admin/messages":                {"DeleteMessages", "留言模块", "删除", "org.cuit.epoch.controller.MessageController.deleteMessages", "删除留言"},
	"DELETE /admin/pages/{pageId}":          {"DeletePage", "页面模块", "删除", "org.cuit.epoch.controller.PageController.deletePage", "删除页面"},
	"POST /admin/pages":                     {"SaveOrUpdatePage", "页面模块", "新增或修改", "org.cuit.epoch.controller.PageController.saveOrUpdatePage", "保存或更新页面"},
	"POST /admin/photos/albums":             {"SaveOrUpdatePhotoAlbum", "相册模块", "新增或修改", "org.cuit.epoch.controller.PhotoAlbumController.saveOrUpdatePhotoAlbum", "保存或更新相册"},
	"DELETE /admin/photos/albums/{albumId}": {"DeletePhotoAlbumById", "相册模块", "删除", "org.cuit.epoch.controller.PhotoAlbumController.deletePhotoAlbumById", "根据id删除相册"},
	"PUT /admin/photos":                     {"UpdatePhoto", "照片模块", "修改", "org.cuit.epoch.controller.PhotoController.updatePhoto", "更新照片信息"},
	"POST /admin/photos":                    {"SavePhotos", "照片模块", "新增", "org.cuit.epoch.controller.PhotoController.savePhotos", "保存照片信息"},
	"PUT /admin/photos/album":               {"UpdatePhotosAlbum", "照片模块", "修改", "org.cuit.epoch.controller.PhotoController.updatePhotosAlbum", "移动照片相册"},
	"PUT /admin/photos/delete":              {"UpdatePhotoDelete", "照片模块", "修改", "org.cuit.epoch.controller.PhotoController.updatePhotoDelete", "更新照片删除状态（逻辑删除和恢复）"},
	"DELETE /admin/photos":                  {"DeletePhotos", "照片模块", "删除", "org.cuit.epoch.controller.PhotoController.deletePhotos", "删除照片"},
	"POST /admin/role":                      {"SaveOrUpdateRole", "角色模块", "新增或修改", "org.cuit.epoch.controller.RoleController.saveOrUpdateRole", "保存或更新角色"},
	"DELETE /admin/roles":                   {"DeleteRoles", "角色模块", "删除", "org.cuit.epoch.controller.RoleController.deleteRoles", "删除角色"},
	"POST /admin/tags":                      {"SaveOrUpdateTag", "标签模块", "新增或修改", "org.cuit.epoch.controller.TagController.saveOrUpdateTag", "添加或修改标签"},
	"DELETE /admin/tags":                    {"DeleteTag", "标签模块", "删除", "org.cuit.epoch.controller.TagController.deleteTag", "删除标签"},
	"PUT /admin/users/role":                 {"UpdateUserRole", "用户信息模块", "修改", "org.cuit.epoch.controller.UserInfoController.updateUserRole", "后台修改用户角色和昵称"},
	"PUT /admin/users/disable":              {"UpdateUserDisable", "用户信息模块", "修改", "org.cuit.epoch.controller.UserInfoController.updateUserDisable", "修改用户禁用状态"},
}
