package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"blog-go/internal/platform"
)

// 用途：承接原 GET /admin/menus 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListMenus(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListMenus"); err != nil {
		return nil, err
	}
	return s.readTree(ctx, in, "menu", "all")
}

// 用途：承接原 POST /admin/menus 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdateMenu(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "SaveOrUpdateMenu"); err != nil {
		return nil, err
	}
	var p struct {
		ID                          int64
		Name, Path, Component, Icon string
		OrderNum                    *int
		ParentID                    *int64
		IsHidden                    int
	}
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	if p.ID < 0 || p.OrderNum == nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Path) == "" || strings.TrimSpace(p.Component) == "" || strings.TrimSpace(p.Icon) == "" || !flag(p.IsHidden) {
		return nil, invalid("菜单参数不正确")
	}
	if p.ParentID != nil && *p.ParentID == 0 {
		p.ParentID = nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	nodes, err := readNodes(ctx, tx, "menu")
	if err != nil {
		return nil, err
	}
	if p.ID != 0 && p.ParentID == nil && !hasField(in, "parentId") && nodes[p.ID].parent != 0 {
		parent := nodes[p.ID].parent
		p.ParentID = &parent
	}
	if err := checkParent(nodes, p.ID, p.ParentID); err != nil {
		return nil, err
	}
	if p.ID == 0 {
		_, err = tx.ExecContext(ctx, "INSERT INTO tb_menu(name,path,component,icon,order_num,parent_id,is_hidden,create_time) VALUES(?,?,?,?,?,?,?,?)", p.Name, p.Path, p.Component, p.Icon, *p.OrderNum, p.ParentID, p.IsHidden, s.stamp())
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE tb_menu SET name=?,path=?,component=?,icon=?,order_num=?,parent_id=?,is_hidden=?,update_time=? WHERE id=?", p.Name, p.Path, p.Component, p.Icon, *p.OrderNum, p.ParentID, p.IsHidden, s.stamp(), p.ID)
		err = managementChanged(result, err)
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 DELETE /admin/menus/{menuId} 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteMenu(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "DeleteMenu"); err != nil {
		return nil, err
	}
	id, err := pathID(in, "menuId")
	if err != nil {
		return nil, err
	}
	return nil, s.deleteTree(ctx, "menu", id)
}

// 用途：承接原 GET /admin/role/menus 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListMenuOptions(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListMenuOptions"); err != nil {
		return nil, err
	}
	return s.readTree(ctx, in, "menu", "option")
}

// 用途：承接原 GET /admin/user/menus 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListUserMenus(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListUserMenus"); err != nil {
		return nil, err
	}
	return s.readTree(ctx, in, "menu", "user")
}

// 用途：承接原 GET /admin/resources 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListResources(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListResources"); err != nil {
		return nil, err
	}
	return s.readTree(ctx, in, "resource", "all")
}

// 用途：承接原 DELETE /admin/resources/{resourceId} 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteResource(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "DeleteResource"); err != nil {
		return nil, err
	}
	id, err := pathID(in, "resourceId")
	if err != nil {
		return nil, err
	}
	return nil, s.deleteTree(ctx, "resource", id)
}

// 用途：承接原 POST /admin/resources 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdateResource(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "SaveOrUpdateResource"); err != nil {
		return nil, err
	}
	var p struct {
		ID                 int64
		ResourceName       string
		URL, RequestMethod *string
		ParentID           *int64
		IsAnonymous        int
	}
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	if p.ID < 0 || strings.TrimSpace(p.ResourceName) == "" || !flag(p.IsAnonymous) {
		return nil, invalid("资源参数不正确")
	}
	if p.URL != nil && *p.URL == "" {
		p.URL = nil
	}
	if p.RequestMethod != nil && *p.RequestMethod == "" {
		p.RequestMethod = nil
	}
	if (p.URL == nil) != (p.RequestMethod == nil) {
		return nil, invalid("资源方法与路径必须同时填写")
	}
	if p.URL != nil && !validResourceRule(*p.RequestMethod, *p.URL) {
		return nil, invalid("资源必须匹配已注册管理路由")
	}
	if p.ParentID != nil && *p.ParentID == 0 {
		p.ParentID = nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	nodes, err := readNodes(ctx, tx, "resource")
	if err != nil {
		return nil, err
	}
	if p.ID != 0 && p.ParentID == nil && !hasField(in, "parentId") && nodes[p.ID].parent != 0 {
		parent := nodes[p.ID].parent
		p.ParentID = &parent
	}
	if err := checkParent(nodes, p.ID, p.ParentID); err != nil {
		return nil, err
	}
	if p.ID == 0 {
		_, err = tx.ExecContext(ctx, "INSERT INTO tb_resource(resource_name,url,request_method,parent_id,is_anonymous,create_time) VALUES(?,?,?,?,?,?)", p.ResourceName, p.URL, p.RequestMethod, p.ParentID, p.IsAnonymous, s.stamp())
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE tb_resource SET resource_name=?,url=?,request_method=?,parent_id=?,is_anonymous=?,update_time=? WHERE id=?", p.ResourceName, p.URL, p.RequestMethod, p.ParentID, p.IsAnonymous, s.stamp(), p.ID)
		err = managementChanged(result, err)
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /admin/role/resources 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListResourceOption(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListResourceOption"); err != nil {
		return nil, err
	}
	return s.readTree(ctx, in, "resource", "option")
}

// 用途：承接原 GET /admin/users/role 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListUserRoles(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListUserRoles"); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,role_name FROM tb_role ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []platform.Object{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result = append(result, platform.Object{"id": id, "roleName": name})
	}
	return result, rows.Err()
}

// 用途：承接原 GET /admin/roles 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListRoles(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListRoles"); err != nil {
		return nil, err
	}
	offset, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	keyword := "%" + escapeLike(in.Query.Get("keywords")) + "%"
	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_role WHERE role_name LIKE ? ESCAPE '\\'", keyword).Scan(&count); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,role_name,role_label,is_disable,create_time FROM tb_role WHERE role_name LIKE ? ESCAPE '\\' ORDER BY id LIMIT ? OFFSET ?", keyword, size, offset)
	if err != nil {
		return nil, err
	}
	records := []platform.Object{}
	ids := []int64{}
	for rows.Next() {
		var id int64
		var name, label, stamp string
		var disabled int
		if err := rows.Scan(&id, &name, &label, &disabled, &stamp); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		records = append(records, platform.Object{"id": id, "roleName": name, "roleLabel": label, "isDisable": disabled, "createTime": stamp, "resourceIdList": []int64{}, "menuIdList": []int64{}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, link := range []struct{ table, column, key string }{{"tb_role_resource", "resource_id", "resourceIdList"}, {"tb_role_menu", "menu_id", "menuIdList"}} {
		groups, err := readLinks(ctx, tx, link.table, "role_id", link.column, ids)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if group := groups[record["id"].(int64)]; group != nil {
				record[link.key] = group
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return platform.Page{RecordList: records, Count: count}, nil
}

// 用途：承接原 POST /admin/role 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdateRole(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "SaveOrUpdateRole"); err != nil {
		return nil, err
	}
	var p struct {
		ID                         int64
		RoleName, RoleLabel        string
		ResourceIDList, MenuIDList []int64
	}
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	if p.ID < 0 || strings.TrimSpace(p.RoleName) == "" || strings.TrimSpace(p.RoleLabel) == "" {
		return nil, invalid("角色名称和标签不能为空")
	}
	if err := validIDs(p.ResourceIDList); err != nil {
		return nil, err
	}
	if err := validIDs(p.MenuIDList); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tb_role WHERE (role_name=? OR role_label=?) AND id<>?)", p.RoleName, p.RoleLabel, p.ID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, denied("角色名称或标签已存在")
	}
	if err := requireIDs(ctx, tx, "tb_resource", p.ResourceIDList); err != nil {
		return nil, err
	}
	if err := requireIDs(ctx, tx, "tb_menu", p.MenuIDList); err != nil {
		return nil, err
	}
	id := p.ID
	if id == 0 {
		result, e := tx.ExecContext(ctx, "INSERT INTO tb_role(role_name,role_label,create_time) VALUES(?,?,?)", p.RoleName, p.RoleLabel, s.stamp())
		if e != nil {
			return nil, e
		}
		id, err = result.LastInsertId()
	} else {
		result, e := tx.ExecContext(ctx, "UPDATE tb_role SET role_name=?,role_label=?,update_time=? WHERE id=?", p.RoleName, p.RoleLabel, s.stamp(), id)
		err = managementChanged(result, e)
	}
	if err != nil {
		return nil, err
	}
	if p.ResourceIDList != nil {
		if err := replaceLinks(ctx, tx, "tb_role_resource", "role_id", "resource_id", id, p.ResourceIDList); err != nil {
			return nil, err
		}
	}
	if p.MenuIDList != nil {
		if err := replaceLinks(ctx, tx, "tb_role_menu", "role_id", "menu_id", id, p.MenuIDList); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

// 用途：承接原 DELETE /admin/roles 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteRoles(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "DeleteRoles"); err != nil {
		return nil, err
	}
	ids, err := bodyIDs(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireIDs(ctx, tx, "tb_role", ids); err != nil {
		return nil, err
	}
	links, err := readLinks(ctx, tx, "tb_user_role", "role_id", "user_id", ids)
	if err != nil {
		return nil, err
	}
	if len(links) > 0 {
		return nil, denied("角色仍有关联用户")
	}
	if err := deleteIDs(ctx, tx, "tb_role", ids); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /admin/users 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListUsers(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListUsers"); err != nil {
		return nil, err
	}
	return s.listUsers(ctx, in)
}

// 用途：承接原 PUT /admin/users/role 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateUserRole(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "UpdateUserRole"); err != nil {
		return nil, err
	}
	var p struct {
		UserInfoID int64
		Nickname   string
		RoleIDList []int64
	}
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	if p.UserInfoID <= 0 || strings.TrimSpace(p.Nickname) == "" || p.RoleIDList == nil {
		return nil, invalid("用户资料和角色参数不正确")
	}
	if err := validIDs(p.RoleIDList); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireIDs(ctx, tx, "tb_role", p.RoleIDList); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE tb_user_info SET nickname=?,update_time=? WHERE id=?", p.Nickname, s.stamp(), p.UserInfoID)
	if err := changed(result, err); err != nil {
		return nil, err
	}
	if err := replaceLinks(ctx, tx, "tb_user_role", "user_id", "role_id", p.UserInfoID, p.RoleIDList); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 PUT /admin/users/disable 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdateUserDisable(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "UpdateUserDisable"); err != nil {
		return nil, err
	}
	var p struct {
		ID        int64
		IsDisable *int
	}
	if err := decode(in, &p); err != nil {
		return nil, err
	}
	if p.ID <= 0 || p.IsDisable == nil || !flag(*p.IsDisable) {
		return nil, invalid("禁用参数不正确")
	}
	s.sessions.mu.Lock()
	defer s.sessions.mu.Unlock()
	result, err := s.db.ExecContext(ctx, "UPDATE tb_user_info SET is_disable=?,update_time=? WHERE id=?", *p.IsDisable, s.stamp(), p.ID)
	if err := changed(result, err); err != nil {
		return nil, err
	}
	if *p.IsDisable == 1 {
		s.revokeUserLocked(p.ID)
	}
	return nil, nil
}

// 用途：承接原 GET /admin/users/online 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListOnlineUsers(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "ListOnlineUsers"); err != nil {
		return nil, err
	}
	offset, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	s.sessions.mu.Lock()
	s.pruneSessionsLocked()
	latest := make(map[int64]Session)
	devices := make(map[int64][2]string)
	for token, entry := range s.sessions.byToken {
		if old, ok := latest[entry.UserInfoID]; !ok || entry.ExpiresAt.After(old.ExpiresAt) {
			latest[entry.UserInfoID] = entry
			devices[entry.UserInfoID] = s.sessions.devices[token]
		}
	}
	s.sessions.mu.Unlock()
	records, err := s.onlineProfiles(ctx, in.Query.Get("keywords"), latest, devices)
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		left, right := records[i]["lastLoginTime"].(string), records[j]["lastLoginTime"].(string)
		if left == right {
			return records[i]["userInfoId"].(int64) < records[j]["userInfoId"].(int64)
		}
		return left > right
	})
	count := int64(len(records))
	if offset >= count {
		return platform.Page{RecordList: []platform.Object{}, Count: count}, nil
	}
	end := offset + size
	if end > count {
		end = count
	}
	return platform.Page{RecordList: records[offset:end], Count: count}, nil
}

// 用途：承接原 DELETE /admin/users/{userInfoId}/online 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) RemoveOnlineUser(ctx context.Context, in platform.Input) (any, error) {
	if err := s.guard(ctx, in, "RemoveOnlineUser"); err != nil {
		return nil, err
	}
	id, err := pathID(in, "userInfoId")
	if err != nil {
		return nil, err
	}
	s.sessions.mu.Lock()
	s.revokeUserLocked(id)
	s.sessions.mu.Unlock()
	return nil, nil
}

type identityReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type treeNode struct {
	id, parent int64
	order      int
	data       platform.Object
}

func flag(v int) bool { return v == 0 || v == 1 }
func managementChanged(result sql.Result, err error) error {
	err = changed(result, err)
	var business *platform.Error
	if errors.As(err, &business) && business.Code == platform.CodeMissing {
		return errors.Join(denied(business.Message), err)
	}
	return err
}
func hasField(in platform.Input, key string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(in.Body, &fields) != nil {
		return in.Form.Has(key)
	}
	for field := range fields {
		if strings.EqualFold(field, key) {
			return true
		}
	}
	return false
}
func pathID(in platform.Input, key string) (int64, error) {
	id, err := strconv.ParseInt(in.Params[key], 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid("目标 ID 无效")
	}
	return id, nil
}
func validIDs(ids []int64) error {
	for _, id := range ids {
		if id <= 0 {
			return invalid("目标 ID 无效")
		}
	}
	return nil
}
func bodyIDs(in platform.Input) ([]int64, error) {
	var ids []int64
	if json.Unmarshal(in.Body, &ids) != nil || ids == nil {
		return nil, invalid("需要 ID 数组")
	}
	return ids, validIDs(ids)
}
func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}
func pagination(q url.Values) (int64, int64, error) {
	current, size := int64(1), int64(10)
	var err error
	if q.Has("current") {
		current, err = strconv.ParseInt(q.Get("current"), 10, 64)
		if err != nil {
			return 0, 0, invalid("分页参数无效")
		}
	}
	if q.Has("size") {
		size, err = strconv.ParseInt(q.Get("size"), 10, 64)
		if err != nil {
			return 0, 0, invalid("分页参数无效")
		}
	}
	if current < 1 || size < 1 || size > 100 || current-1 > math.MaxInt64/size {
		return 0, 0, invalid("分页参数无效")
	}
	return (current - 1) * size, size, nil
}
func idArgs(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}
func requireIDs(ctx context.Context, tx *sql.Tx, table string, ids []int64) error {
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		unique := make(map[int64]bool)
		for _, id := range ids[start:end] {
			unique[id] = true
		}
		marks, args := idArgs(ids[start:end])
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id IN ("+marks+")", args...).Scan(&n); err != nil {
			return err
		}
		if n != len(unique) {
			return denied("关联目标不存在")
		}
	}
	return nil
}
func replaceLinks(ctx context.Context, tx *sql.Tx, table, left, right string, id int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+left+"=?", id); err != nil {
		return err
	}
	seen := make(map[int64]bool)
	for _, other := range ids {
		if seen[other] {
			continue
		}
		seen[other] = true
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+"("+left+","+right+") VALUES(?,?)", id, other); err != nil {
			return err
		}
	}
	return nil
}
func readLinks(ctx context.Context, r identityReader, table, left, right string, ids []int64) (map[int64][]int64, error) {
	groups := make(map[int64][]int64)
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		marks, args := idArgs(ids[start:end])
		rows, err := r.QueryContext(ctx, "SELECT "+left+","+right+" FROM "+table+" WHERE "+left+" IN ("+marks+") ORDER BY "+left+","+right, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var a, b int64
			if err := rows.Scan(&a, &b); err != nil {
				rows.Close()
				return nil, err
			}
			groups[a] = append(groups[a], b)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return groups, nil
}
func deleteIDs(ctx context.Context, tx *sql.Tx, table string, ids []int64) error {
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		marks, args := idArgs(ids[start:end])
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id IN ("+marks+")", args...); err != nil {
			return err
		}
	}
	return nil
}

func readNodes(ctx context.Context, r identityReader, kind string) (map[int64]treeNode, error) {
	query := "SELECT id,name,path,component,icon,order_num,parent_id,is_hidden,create_time FROM tb_menu"
	if kind == "resource" {
		query = "SELECT id,resource_name,url,request_method,parent_id,is_anonymous,create_time FROM tb_resource"
	}
	rows, err := r.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := make(map[int64]treeNode)
	for rows.Next() {
		var id int64
		var parent sql.NullInt64
		var name, stamp string
		var order, hidden int
		var data platform.Object
		if kind == "menu" {
			var path, component, icon string
			if err := rows.Scan(&id, &name, &path, &component, &icon, &order, &parent, &hidden, &stamp); err != nil {
				return nil, err
			}
			data = platform.Object{"id": id, "name": name, "path": path, "component": component, "icon": icon, "orderNum": order, "isHidden": hidden, "isDisable": nil, "createTime": stamp}
		} else {
			var path, method sql.NullString
			if err := rows.Scan(&id, &name, &path, &method, &parent, &hidden, &stamp); err != nil {
				return nil, err
			}
			data = platform.Object{"id": id, "resourceName": name, "url": nullable(path), "requestMethod": nullable(method), "isAnonymous": hidden, "isDisable": nil, "createTime": stamp}
		}
		nodes[id] = treeNode{id: id, parent: parent.Int64, order: order, data: data}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := validForest(nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}
func validForest(nodes map[int64]treeNode) error {
	done := make(map[int64]bool)
	for id := range nodes {
		path := make(map[int64]bool)
		for id != 0 && !done[id] {
			if path[id] {
				return invalid("菜单或资源关系存在环")
			}
			node, ok := nodes[id]
			if !ok {
				return denied("父节点不存在")
			}
			path[id] = true
			id = node.parent
		}
		for visited := range path {
			done[visited] = true
		}
	}
	return nil
}
func checkParent(nodes map[int64]treeNode, id int64, parent *int64) error {
	if id != 0 {
		if _, ok := nodes[id]; !ok {
			return denied("节点不存在")
		}
	} else {
		id = -1
	}
	p := int64(0)
	if parent != nil {
		p = *parent
		if p <= 0 {
			return invalid("父节点 ID 无效")
		}
		if _, ok := nodes[p]; !ok {
			return denied("父节点不存在")
		}
	}
	n := nodes[id]
	n.id = id
	n.parent = p
	nodes[id] = n
	return validForest(nodes)
}
func (s *Service) readTree(ctx context.Context, in platform.Input, kind, mode string) (any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	nodes, err := readNodes(ctx, tx, kind)
	if err != nil {
		return nil, err
	}
	keep := make(map[int64]bool)
	if mode == "user" {
		rows, err := tx.QueryContext(ctx, `SELECT DISTINCT rm.menu_id FROM tb_role_menu rm JOIN tb_role r ON r.id=rm.role_id JOIN tb_user_role ur ON ur.role_id=r.id WHERE ur.user_id=? AND r.is_disable=0`, in.Actor.UserInfoID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			if _, ok := nodes[id]; ok {
				keep[id] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	} else {
		key := "name"
		if kind == "resource" {
			key = "resourceName"
		}
		for id, node := range nodes {
			if mode == "option" && kind == "resource" && node.data["isAnonymous"].(int) != 0 {
				continue
			}
			if strings.Contains(strings.ToLower(node.data[key].(string)), strings.ToLower(in.Query.Get("keywords"))) {
				keep[id] = true
			}
		}
	}
	for id := range keep {
		for parent := nodes[id].parent; parent != 0; parent = nodes[parent].parent {
			if keep[parent] {
				break
			}
			keep[parent] = true
		}
	}
	children := make(map[int64][]treeNode)
	for id, node := range nodes {
		if keep[id] {
			children[node.parent] = append(children[node.parent], node)
		}
	}
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool {
			a, b := children[parent][i], children[parent][j]
			if a.order == b.order {
				return a.id < b.id
			}
			return a.order < b.order
		})
	}
	var build func(int64) []platform.Object
	build = func(parent int64) []platform.Object {
		out := []platform.Object{}
		for _, node := range children[parent] {
			nested := build(node.id)
			var item platform.Object
			if mode == "option" {
				key := "name"
				if kind == "resource" {
					key = "resourceName"
				}
				item = platform.Object{"id": node.id, "label": node.data[key], "children": nested}
			} else if mode == "user" {
				item = platform.Object{"name": node.data["name"], "path": node.data["path"], "component": node.data["component"], "icon": node.data["icon"], "hidden": node.data["isHidden"].(int) == 1, "children": nested}
				if parent == 0 && len(nested) == 0 && node.data["component"] != "Layout" {
					child := platform.Object{"name": item["name"], "path": "", "component": item["component"], "icon": item["icon"], "hidden": item["hidden"], "children": []platform.Object{}}
					item["component"] = "Layout"
					item["children"] = []platform.Object{child}
				}
			} else {
				item = node.data
				item["children"] = nested
			}
			out = append(out, item)
		}
		return out
	}
	result := build(0)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) onlineProfiles(ctx context.Context, keyword string, latest map[int64]Session, devices map[int64][2]string) ([]platform.Object, error) {
	ids := make([]int64, 0, len(latest))
	for _, entry := range latest {
		ids = append(ids, entry.AuthID)
	}
	records := []platform.Object{}
	keyword = strings.ToLower(keyword)
	if len(ids) == 0 {
		return records, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// At most one database query per 500 live accounts; profile data is read
	// from the database, while browser metadata belongs to the live session.
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		marks, args := idArgs(ids[start:end])
		rows, err := tx.QueryContext(ctx, `SELECT a.id,i.id,i.nickname,i.avatar,a.ip_address,a.ip_source
			FROM tb_user_auth a JOIN tb_user_info i ON i.id=a.user_info_id WHERE i.is_disable=0 AND a.id IN (`+marks+")", args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var aid, uid int64
			var nickname, avatar string
			var ip, source sql.NullString
			if err := rows.Scan(&aid, &uid, &nickname, &avatar, &ip, &source); err != nil {
				rows.Close()
				return nil, err
			}
			entry, ok := latest[uid]
			if !ok || entry.AuthID != aid || !strings.Contains(strings.ToLower(nickname), keyword) {
				continue
			}
			records = append(records, platform.Object{"userInfoId": uid, "nickname": nickname, "avatar": avatar, "ipAddress": nullable(ip), "ipSource": nullable(source), "browser": devices[uid][0], "os": devices[uid][1], "lastLoginTime": entry.ExpiresAt.Add(-10 * time.Hour).In(s.cfg.TimeZone).Format("2006-01-02T15:04:05")})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}
func (s *Service) deleteTree(ctx context.Context, kind string, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	nodes, err := readNodes(ctx, tx, kind)
	if err != nil {
		return err
	}
	if _, ok := nodes[id]; !ok {
		return denied("节点不存在")
	}
	children := make(map[int64][]int64)
	for key, node := range nodes {
		children[node.parent] = append(children[node.parent], key)
	}
	ids := []int64{id}
	for i := 0; i < len(ids); i++ {
		ids = append(ids, children[ids[i]]...)
	}
	links, err := readLinks(ctx, tx, "tb_role_"+kind, kind+"_id", "role_id", ids)
	if err != nil {
		return err
	}
	if len(links) > 0 {
		return denied("节点或子节点仍被角色引用")
	}
	if err := deleteIDs(ctx, tx, "tb_"+kind, ids); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) listUsers(ctx context.Context, in platform.Input) (any, error) {
	offset, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	where := "i.nickname LIKE ? ESCAPE '\\'"
	args := []any{"%" + escapeLike(in.Query.Get("keywords")) + "%"}
	if v := in.Query.Get("loginType"); v != "" {
		typ, err := strconv.Atoi(v)
		if err != nil || typ < 1 || typ > 3 {
			return nil, invalid("登录类型无效")
		}
		where += " AND a.login_type=?"
		args = append(args, typ)
	}
	online := make(map[int64]bool)
	s.sessions.mu.Lock()
	s.pruneSessionsLocked()
	for _, entry := range s.sessions.byToken {
		online[entry.UserInfoID] = true
	}
	s.sessions.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_user_auth a JOIN tb_user_info i ON i.id=a.user_info_id WHERE "+where, args...).Scan(&count); err != nil {
		return nil, err
	}
	params := append(append([]any{}, args...), size, offset)
	rows, err := tx.QueryContext(ctx, `SELECT a.id,i.id,i.avatar,i.nickname,a.login_type,a.ip_address,a.ip_source,a.create_time,a.last_login_time,i.is_disable FROM tb_user_auth a JOIN tb_user_info i ON i.id=a.user_info_id WHERE `+where+" ORDER BY a.id LIMIT ? OFFSET ?", params...)
	if err != nil {
		return nil, err
	}
	records := []platform.Object{}
	uids := []int64{}
	for rows.Next() {
		var id, uid int64
		var avatar, nickname, created string
		var typ, disabled int
		var ip, source, last sql.NullString
		if err := rows.Scan(&id, &uid, &avatar, &nickname, &typ, &ip, &source, &created, &last, &disabled); err != nil {
			rows.Close()
			return nil, err
		}
		at, err := time.ParseInLocation("2006-01-02T15:04:05", created, s.cfg.TimeZone)
		if err != nil {
			rows.Close()
			return nil, err
		}
		status := 0
		if online[uid] && disabled == 0 {
			status = 1
		}
		var lastLogin any
		if last.Valid {
			parsed, err := time.ParseInLocation("2006-01-02T15:04:05", last.String, s.cfg.TimeZone)
			if err != nil {
				rows.Close()
				return nil, err
			}
			lastLogin = parsed.UnixMilli()
		}
		records = append(records, platform.Object{"id": id, "userInfoId": uid, "avatar": avatar, "nickname": nickname, "loginType": typ, "ipAddress": nullable(ip), "ipSource": nullable(source), "createTime": at.UnixMilli(), "lastLoginTime": lastLogin, "isDisable": disabled, "status": status, "roleList": []platform.Object{}})
		uids = append(uids, uid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(uids) > 0 {
		marks, values := idArgs(uids)
		rows, err := tx.QueryContext(ctx, "SELECT ur.user_id,r.id,r.role_name FROM tb_user_role ur JOIN tb_role r ON r.id=ur.role_id WHERE ur.user_id IN ("+marks+") ORDER BY r.id", values...)
		if err != nil {
			return nil, err
		}
		groups := make(map[int64][]platform.Object)
		for rows.Next() {
			var uid, id int64
			var name string
			if err := rows.Scan(&uid, &id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			groups[uid] = append(groups[uid], platform.Object{"id": id, "roleName": name})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if roles := groups[record["userInfoId"].(int64)]; roles != nil {
				record["roleList"] = roles
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return platform.Page{RecordList: records, Count: count}, nil
}
