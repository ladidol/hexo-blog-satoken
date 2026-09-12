package identity

import (
	"context"
	"strconv"
	"strings"

	"blog-go/internal/analytics"
	"blog-go/internal/article"
	"blog-go/internal/catalog"
	"blog-go/internal/community"
	"blog-go/internal/files"
	"blog-go/internal/media"
	"blog-go/internal/platform"
	"blog-go/internal/site"
)

// 用途：根据实时角色与资源关联校验管理操作，缺授权拒绝。
func (s *Service) Authorize(ctx context.Context, actor platform.Actor, method, pattern string) error {
	if !actor.LoggedIn || actor.AuthID <= 0 || actor.UserInfoID <= 0 || !registeredAdmin(method, pattern) {
		return denied("没有操作权限")
	}
	// A single query observes account, roles and bindings at the same SQLite
	// snapshot. Names and is_anonymous never constitute management authority.
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT resource.url FROM tb_resource resource
		JOIN tb_role_resource rr ON rr.resource_id=resource.id JOIN tb_role r ON r.id=rr.role_id
		JOIN tb_user_role ur ON ur.role_id=r.id JOIN tb_user_info i ON i.id=ur.user_id
		JOIN tb_user_auth a ON a.user_info_id=i.id
		WHERE a.id=? AND i.id=? AND i.is_disable=0 AND r.is_disable=0 AND resource.request_method=?`, actor.AuthID, actor.UserInfoID, method)
	if err != nil {
		return err
	}
	defer rows.Close()
	allowed := false
	for rows.Next() {
		var rule string
		if err := rows.Scan(&rule); err != nil {
			return err
		}
		if validResourceRule(method, rule) && resourceMatches(rule, pattern) {
			allowed = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !allowed {
		return denied("没有操作权限")
	}
	return nil
}

func adminGroups() [][]platform.RouteSpec {
	return [][]platform.RouteSpec{RouteSpecs, article.RouteSpecs, catalog.RouteSpecs, community.RouteSpecs, media.RouteSpecs, site.RouteSpecs, analytics.RouteSpecs, files.RouteSpecs}
}
func registeredAdmin(method, path string) bool {
	for _, group := range adminGroups() {
		for _, spec := range group {
			if spec.Policy == "admin" && spec.Method == method && spec.Path == path {
				return true
			}
		}
	}
	return false
}
func validResourceRule(method, rule string) bool {
	if rule == "" || strings.ContainsAny(rule, "[]()^$+|\\%") {
		return false
	}
	segments := strings.Split(rule, "/")
	for i, segment := range segments {
		if segment == "*" {
			segments[i] = "{wild" + strconv.Itoa(i) + "}"
		} else if segment == "**" && i == len(segments)-1 {
			segments[i] = "{tail...}"
		} else if strings.Contains(segment, "*") {
			return false
		}
	}
	if _, err := platform.GinPath(strings.Join(segments, "/")); err != nil {
		return false
	}
	for _, group := range adminGroups() {
		for _, spec := range group {
			if spec.Policy == "admin" && spec.Method == method && resourceMatches(rule, spec.Path) {
				return true
			}
		}
	}
	return false
}
func resourceMatches(rule, pattern string) bool {
	r, p := strings.Split(rule, "/"), strings.Split(pattern, "/")
	for i, segment := range r {
		if segment == "**" && i == len(r)-1 {
			return true
		}
		if i >= len(p) {
			return false
		}
		if segment != "*" && segment != p[i] {
			return false
		}
	}
	return len(r) == len(p)
}
func (s *Service) guard(ctx context.Context, in platform.Input, operation string) error {
	a, err := s.requireActor(ctx, in)
	if err != nil {
		return err
	}
	for _, spec := range RouteSpecs {
		if spec.Operation == operation {
			return s.Authorize(ctx, a, spec.Method, spec.Path)
		}
	}
	return denied("没有操作权限")
}
