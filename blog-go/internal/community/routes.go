package community

import "blog-go/internal/platform"

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	handlers := map[string]platform.Endpoint{
		"ListComments": s.ListComments, "SaveComment": s.SaveComment,
		"ListRepliesByCommentId": s.ListRepliesByCommentId, "SaveCommentLike": s.SaveCommentLike,
		"UpdateCommentsReview": s.UpdateCommentsReview, "DeleteComments": s.DeleteComments,
		"ListCommentBackDTO": s.ListCommentBackDTO, "SaveMessage": s.SaveMessage,
		"ListMessages": s.ListMessages, "ListMessageBackDTO": s.ListMessageBackDTO,
		"UpdateMessagesReview": s.UpdateMessagesReview, "DeleteMessages": s.DeleteMessages,
	}
	routes := make([]platform.Route, 0, len(RouteSpecs))
	for _, spec := range RouteSpecs {
		routes = append(routes, platform.Route{Spec: spec, Handle: handlers[spec.Operation]})
	}
	return routes
}

// 用途：作为评论、回复与留言审核路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "GET", Path: "/comments", Operation: "ListComments", Policy: "public", Audit: false},
	{Method: "POST", Path: "/comments", Operation: "SaveComment", Policy: "user", Audit: false},
	{Method: "GET", Path: "/comments/{commentId}/replies", Operation: "ListRepliesByCommentId", Policy: "public", Audit: false},
	{Method: "POST", Path: "/comments/{commentId}/like", Operation: "SaveCommentLike", Policy: "user", Audit: false},
	{Method: "PUT", Path: "/admin/comments/review", Operation: "UpdateCommentsReview", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/comments", Operation: "DeleteComments", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/admin/comments", Operation: "ListCommentBackDTO", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/messages", Operation: "SaveMessage", Policy: "public", Audit: false},
	{Method: "GET", Path: "/messages", Operation: "ListMessages", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/messages", Operation: "ListMessageBackDTO", Policy: "admin", Audit: false},
	{Method: "PUT", Path: "/admin/messages/review", Operation: "UpdateMessagesReview", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/messages", Operation: "DeleteMessages", Policy: "admin", Audit: true},
}

// P-059 HTTP GET /comments

// P-060 HTTP POST /comments
// 响应变化：data新增commentId与notification.status/deliveryId，通知失败保持已保存评论的flag=true。

// P-061 HTTP GET /comments/{commentId}/replies

// P-062 HTTP POST /comments/{commentId}/like

// P-063 HTTP PUT /admin/comments/review

// P-064 HTTP DELETE /admin/comments

// P-065 HTTP GET /admin/comments

// P-066 HTTP POST /messages

// P-067 HTTP GET /messages

// P-068 HTTP GET /admin/messages

// P-069 HTTP PUT /admin/messages/review

// P-070 HTTP DELETE /admin/messages
