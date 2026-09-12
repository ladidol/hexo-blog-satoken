package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"blog-go/internal/identity"
	"blog-go/internal/platform"
)

func adminInput(body any, a platform.Actor) platform.Input {
	in := input(body)
	in.Actor = a
	return in
}

func TestManagementEditsFilteringAndHTTPCredentials(t *testing.T) {
	f := setup(t)
	_, admin, _ := login(t, f, "local-admin", "admin")
	menu := map[string]any{"name": "Created", "path": "/created", "component": "/Created.vue", "icon": "created", "orderNum": 7}
	if _, err := f.s.SaveOrUpdateMenu(ctx, adminInput(menu, admin)); err != nil {
		t.Fatal(err)
	}
	id := number(t, f, "SELECT id FROM tb_menu WHERE name='Created'")
	menu["id"] = id
	menu["name"] = "Edited"
	menu["isHidden"] = 1
	if _, err := f.s.SaveOrUpdateMenu(ctx, adminInput(menu, admin)); err != nil {
		t.Fatal(err)
	}
	query := adminInput(nil, admin)
	query.Query = url.Values{"keywords": {"Edited"}}
	v, err := f.s.ListMenus(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), `"name":"Edited"`) || !strings.Contains(string(raw), `"isHidden":1`) || strings.Contains(string(raw), `"name":"Created"`) {
		t.Fatalf("edited menus=%s", raw)
	}
	res := resourceID(t, f, "GET", "/admin/roles")
	role := map[string]any{"roleName": "Editor", "roleLabel": "editor", "resourceIdList": []int64{res}, "menuIdList": []int64{id}}
	if _, err := f.s.SaveOrUpdateRole(ctx, adminInput(role, admin)); err != nil {
		t.Fatal(err)
	}
	roleID := number(t, f, "SELECT id FROM tb_role WHERE role_label='editor'")
	role["id"] = roleID
	role["roleName"] = "Edited role"
	role["menuIdList"] = []int64{}
	if _, err := f.s.SaveOrUpdateRole(ctx, adminInput(role, admin)); err != nil {
		t.Fatal(err)
	}
	if number(t, f, "SELECT count(*) FROM tb_role_menu WHERE role_id=?", roleID) != 0 || number(t, f, "SELECT count(*) FROM tb_role_resource WHERE role_id=?", roleID) != 1 {
		t.Fatal("role edit did not preserve selected associations")
	}
	if _, err := f.s.DeleteRoles(ctx, adminInput([]int64{roleID, 2}, admin)); err == nil {
		t.Fatal("mixed role deletion partially allowed")
	}
	if number(t, f, "SELECT count(*) FROM tb_role WHERE id=?", roleID) != 1 {
		t.Fatal("partial role deletion")
	}
	query.Query = url.Values{"keywords": {"absent"}, "current": {"999"}, "size": {"10"}}
	for _, read := range []func(context.Context, platform.Input) (any, error){f.s.ListRoles, f.s.ListUsers} {
		v, err := read(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		p := object(t, v)
		if p["count"] != float64(0) || len(p["recordList"].([]any)) != 0 {
			t.Fatalf("empty page=%v", p)
		}
	}
	resource := map[string]any{"resourceName": "Editable", "url": "/admin/roles", "requestMethod": "GET"}
	if _, err := f.s.SaveOrUpdateResource(ctx, adminInput(resource, admin)); err != nil {
		t.Fatal(err)
	}
	resource["id"] = number(t, f, "SELECT id FROM tb_resource WHERE resource_name='Editable'")
	resource["url"] = "/admin/users"
	resource["resourceName"] = "Changed resource"
	if _, err := f.s.SaveOrUpdateResource(ctx, adminInput(resource, admin)); err != nil {
		t.Fatal(err)
	}
	if number(t, f, "SELECT count(*) FROM tb_resource WHERE resource_name='Changed resource' AND url='/admin/users'") != 1 {
		t.Fatal("resource edit not persisted")
	}
	engine, err := platform.NewRouter(f.s.Routes(), platform.Callbacks{Resolve: f.s.Resolve, Authorize: f.s.Authorize}, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"local-admin", "user@example.test"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"username": {user}, "password": {password}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		engine.ServeHTTP(w, r)
		var result platform.Result
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.Flag {
			t.Fatalf("HTTP form login=%s", w.Body)
		}
		cookies := w.Result().Cookies()
		w = httptest.NewRecorder()
		r = httptest.NewRequest("GET", "/admin/users", nil)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		engine.ServeHTTP(w, r)
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Flag != (user == "local-admin") {
			t.Fatalf("HTTP management user=%s result=%s", user, w.Body)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.s.ListMenus(canceled, adminInput(nil, admin)); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestManagementPreservesOriginalDateProjectionAndOmittedParents(t *testing.T) {
	f := setup(t)
	_, admin, _ := login(t, f, "local-admin", "admin")
	v, err := f.s.ListUsers(ctx, adminInput(nil, admin))
	if err != nil {
		t.Fatal(err)
	}
	page := object(t, v)
	for _, row := range page["recordList"].([]any) {
		user := row.(map[string]any)
		if user["lastLoginTime"] != nil {
			if _, ok := user["lastLoginTime"].(float64); !ok {
				t.Fatal("UserBackDTO.lastLoginTime must be numeric milliseconds")
			}
		}
	}
	execute(t, f.db, `INSERT INTO tb_menu(id,name,path,component,icon,order_num,parent_id,create_time) VALUES(10000,'Root','/root','Layout','x',1,NULL,'2026-09-12T16:00:00'),(10001,'Child','child','/Child.vue','x',1,10000,'2026-09-12T16:00:00');
		INSERT INTO tb_resource(id,resource_name,parent_id,url,request_method,create_time) VALUES(900,'Group',NULL,NULL,NULL,'2026-09-12T16:00:00'),(901,'Child',900,'/admin/users','GET','2026-09-12T16:00:00')`)
	if _, err := f.s.SaveOrUpdateMenu(ctx, adminInput(map[string]any{"id": 10001, "name": "Edited", "path": "child", "component": "/Child.vue", "icon": "x", "orderNum": 1}, admin)); err != nil {
		t.Fatal(err)
	}
	if number(t, f, "SELECT parent_id FROM tb_menu WHERE id=10001") != 10000 {
		t.Fatal("omitted menu parent detached child")
	}
	if _, err := f.s.SaveOrUpdateResource(ctx, adminInput(map[string]any{"id": 901, "resourceName": "Edited", "url": "/admin/users", "requestMethod": "GET"}, admin)); err != nil {
		t.Fatal(err)
	}
	if number(t, f, "SELECT parent_id FROM tb_resource WHERE id=901") != 900 {
		t.Fatal("omitted resource parent detached child")
	}
	for _, read := range []func(context.Context, platform.Input) (any, error){f.s.ListMenus, f.s.ListResources} {
		v, err := read(ctx, adminInput(nil, admin))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), "parentId") {
			t.Fatal("internal parent ID leaked beyond frozen DTO")
		}
		if !strings.Contains(string(raw), `"isDisable":null`) {
			t.Fatal("unowned optional flag must remain null")
		}
	}
}
func resourceID(t *testing.T, f *fixture, method, path string) int64 {
	t.Helper()
	var id int64
	if err := f.db.QueryRow("SELECT id FROM tb_resource WHERE request_method=? AND url=?", method, path).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func number(t *testing.T, f *fixture, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := f.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAuthorizationUsesLiveRolesAndRegisteredMethodTemplates(t *testing.T) {
	f := setup(t)
	_, admin, _ := login(t, f, "local-admin", "admin")
	_, user, _ := login(t, f, "user@example.test", "user")
	for _, c := range []struct {
		a            platform.Actor
		method, path string
		allowed      bool
	}{{admin, "GET", "/admin/users", true}, {user, "GET", "/admin/users", false}, {platform.Actor{}, "GET", "/admin/users", false}, {admin, "POST", "/admin/users", false}, {admin, "GET", "/admin/not-registered", false}} {
		err := f.s.Authorize(ctx, c.a, c.method, c.path)
		if (err == nil) != c.allowed {
			t.Fatalf("authorize(%s %s)=%v allowed=%v", c.method, c.path, err, c.allowed)
		}
	}
	id := resourceID(t, f, "GET", "/admin/users")
	execute(t, f.db, "INSERT INTO tb_role_resource(role_id,resource_id) VALUES(2,?)", id)
	if err := f.s.Authorize(ctx, user, "GET", "/admin/users"); err != nil {
		t.Fatal(err)
	}
	execute(t, f.db, "UPDATE tb_role SET is_disable=1 WHERE id=2")
	if err := f.s.Authorize(ctx, user, "GET", "/admin/users"); err == nil {
		t.Fatal("disabled role authorized")
	}
	execute(t, f.db, "UPDATE tb_role SET is_disable=0 WHERE id=2")
	execute(t, f.db, "DELETE FROM tb_role_resource WHERE resource_id=?", id)
	execute(t, f.db, "UPDATE tb_resource SET is_anonymous=1 WHERE id=?", id)
	if err := f.s.Authorize(ctx, admin, "GET", "/admin/users"); err == nil {
		t.Fatal("admin name or anonymous flag bypassed missing binding")
	}
	in := adminInput(map[string]any{"resourceName": "danger", "url": "/admin/.*", "requestMethod": "GET"}, admin)
	if _, err := f.s.SaveOrUpdateResource(ctx, in); err == nil {
		t.Fatal("arbitrary regex resource accepted")
	}
	in = adminInput(map[string]any{"resourceName": "online wildcard", "url": "/admin/users/*/online", "requestMethod": "DELETE"}, admin)
	if _, err := f.s.SaveOrUpdateResource(ctx, in); err != nil {
		t.Fatal(err)
	}
	wild := number(t, f, "SELECT id FROM tb_resource WHERE resource_name='online wildcard'")
	execute(t, f.db, "INSERT INTO tb_role_resource(role_id,resource_id) VALUES(2,?)", wild)
	if err := f.s.Authorize(ctx, user, "DELETE", "/admin/users/{userInfoId}/online"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Authorize(ctx, user, "DELETE", "/admin/users/{userInfoId}/anything"); err == nil {
		t.Fatal("wildcard admitted unregistered route")
	}
}

func TestMenuAndResourceTreesEnforceAncestorsCyclesAndReferences(t *testing.T) {
	f := setup(t)
	_, admin, _ := login(t, f, "local-admin", "admin")
	_, user, _ := login(t, f, "user@example.test", "user")
	execute(t, f.db, `INSERT INTO tb_menu(id,name,path,component,icon,order_num,parent_id,is_hidden,create_time) VALUES
		(100,'Parent','/parent','Layout','root',2,NULL,0,'2026-09-12T16:00:00'),
		(101,'Child','child','/Child.vue','child',2,100,1,'2026-09-12T16:00:00'),
		(102,'Sibling','sibling','/Sibling.vue','sibling',1,100,0,'2026-09-12T16:00:00');
		INSERT INTO tb_role_menu(role_id,menu_id) VALUES(2,101)`)
	access := resourceID(t, f, "GET", "/admin/user/menus")
	execute(t, f.db, "INSERT INTO tb_role_resource(role_id,resource_id) VALUES(2,?)", access)
	value, err := f.s.ListUserMenus(ctx, adminInput(nil, user))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(value)
	if !strings.Contains(string(data), `"component":"Layout"`) || !strings.Contains(string(data), `"hidden":true`) || strings.Contains(string(data), "Sibling") {
		t.Fatalf("restricted menu tree=%s", data)
	}
	deletion := adminInput(nil, admin)
	deletion.Params["menuId"] = "100"
	if _, err := f.s.DeleteMenu(ctx, deletion); err == nil {
		t.Fatal("deleted ancestor of referenced child")
	}
	if number(t, f, "SELECT count(*) FROM tb_menu WHERE id IN (100,101,102)") != 3 {
		t.Fatal("partial tree deletion")
	}
	update := adminInput(map[string]any{"id": 100, "name": "cycle", "path": "/parent", "component": "Layout", "icon": "x", "orderNum": 1, "parentId": 101}, admin)
	if _, err := f.s.SaveOrUpdateMenu(ctx, update); err == nil {
		t.Fatal("menu cycle accepted")
	}
	if number(t, f, "SELECT count(*) FROM tb_menu WHERE id=100 AND parent_id IS NULL") != 1 {
		t.Fatal("cycle failure mutated menu")
	}
	for _, method := range []func() (any, error){func() (any, error) { return f.s.ListMenus(ctx, adminInput(nil, admin)) }, func() (any, error) { return f.s.ListMenuOptions(ctx, adminInput(nil, admin)) }} {
		if v, err := method(); err != nil || v == nil {
			t.Fatalf("menu read=%v %v", v, err)
		}
	}
	execute(t, f.db, "DELETE FROM tb_role_menu WHERE role_id=2")
	if _, err := f.s.DeleteMenu(ctx, deletion); err != nil {
		t.Fatal(err)
	}
	if number(t, f, "SELECT count(*) FROM tb_menu WHERE id IN (100,101,102)") != 0 {
		t.Fatal("descendants survived deletion")
	}
	execute(t, f.db, `INSERT INTO tb_resource(id,resource_name,parent_id,url,request_method,create_time) VALUES
		(900,'group',NULL,NULL,NULL,'2026-09-12T16:00:00'),(901,'child',900,'/admin/roles','GET','2026-09-12T16:00:00');
		INSERT INTO tb_role_resource(role_id,resource_id) VALUES(2,901)`)
	deletion.Params = map[string]string{"resourceId": "900"}
	if _, err := f.s.DeleteResource(ctx, deletion); err == nil {
		t.Fatal("deleted resource subtree with referenced descendant")
	}
	if number(t, f, "SELECT count(*) FROM tb_resource WHERE id IN (900,901)") != 2 {
		t.Fatal("partial resource deletion")
	}
	if _, err := f.s.SaveOrUpdateResource(ctx, adminInput(map[string]any{"id": 900, "resourceName": "cycle", "parentId": 901}, admin)); err == nil {
		t.Fatal("resource cycle accepted")
	}
	if _, err := f.s.ListResources(ctx, adminInput(nil, admin)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ListResourceOption(ctx, adminInput(nil, admin)); err != nil {
		t.Fatal(err)
	}
	execute(t, f.db, "DELETE FROM tb_role_resource WHERE resource_id=901")
	if _, err := f.s.DeleteResource(ctx, deletion); err != nil {
		t.Fatal(err)
	}
}

func TestRoleAssignmentsAreAtomicAndImmediatelyAffectAccess(t *testing.T) {
	f := setup(t)
	_, admin, _ := login(t, f, "local-admin", "admin")
	_, user, _ := login(t, f, "user@example.test", "user")
	resource := resourceID(t, f, "GET", "/admin/roles")
	role := map[string]any{"roleName": "Reviewer", "roleLabel": "reviewer", "resourceIdList": []int64{resource}, "menuIdList": []int64{999999}}
	if _, err := f.s.SaveOrUpdateRole(ctx, adminInput(role, admin)); err == nil {
		t.Fatal("invalid relation succeeded")
	}
	if number(t, f, "SELECT count(*) FROM tb_role WHERE role_label='reviewer'") != 0 {
		t.Fatal("partial role remains")
	}
	role["menuIdList"] = []int64{1}
	if _, err := f.s.SaveOrUpdateRole(ctx, adminInput(role, admin)); err != nil {
		t.Fatal(err)
	}
	id := number(t, f, "SELECT id FROM tb_role WHERE role_label='reviewer'")
	assignment := map[string]any{"userInfoId": 11, "nickname": "Reviewer", "roleIdList": []int64{id}}
	if _, err := f.s.UpdateUserRole(ctx, adminInput(assignment, admin)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Authorize(ctx, user, "GET", "/admin/roles"); err != nil {
		t.Fatal(err)
	}
	assignment["roleIdList"] = []int64{1, 999999}
	assignment["nickname"] = "invalid"
	if _, err := f.s.UpdateUserRole(ctx, adminInput(assignment, admin)); err == nil {
		t.Fatal("invalid assignment succeeded")
	}
	if number(t, f, "SELECT count(*) FROM tb_user_role WHERE user_id=11 AND role_id=?", id) != 1 {
		t.Fatal("failed assignment lost old role")
	}
	if _, err := f.s.DeleteRoles(ctx, adminInput([]int64{id}, admin)); err == nil {
		t.Fatal("role with users deleted")
	}
	assignment["roleIdList"] = []int64{2}
	assignment["nickname"] = "Normal"
	if _, err := f.s.UpdateUserRole(ctx, adminInput(assignment, admin)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Authorize(ctx, user, "GET", "/admin/roles"); err == nil {
		t.Fatal("removed role retained access")
	}
	if _, err := f.s.DeleteRoles(ctx, adminInput([]int64{id}, admin)); err != nil {
		t.Fatal(err)
	}
	for _, method := range []func() (any, error){func() (any, error) { return f.s.ListUserRoles(ctx, adminInput(nil, admin)) }, func() (any, error) { return f.s.ListRoles(ctx, adminInput(nil, admin)) }, func() (any, error) { return f.s.ListUsers(ctx, adminInput(nil, admin)) }} {
		if _, err := method(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOnlineUsersDisableKickAndProtectedHTTPRoutes(t *testing.T) {
	f := setup(t)
	_, admin, _ := login(t, f, "local-admin", "admin")
	one, user, _ := login(t, f, "user@example.test", "one")
	two, _, _ := login(t, f, "user@example.test", "two")
	in := adminInput(nil, admin)
	v, err := f.s.ListOnlineUsers(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if object(t, v)["count"] != float64(2) {
		t.Fatal("online sessions not deduplicated by profile")
	}
	in.Query = url.Values{"current": {"999"}, "size": {"10"}}
	v, err = f.s.ListOnlineUsers(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	p := object(t, v)
	if len(p["recordList"].([]any)) != 0 || p["count"] != float64(2) {
		t.Fatalf("out-of-range online page=%v", p)
	}
	kick := adminInput(nil, admin)
	kick.Params["userInfoId"] = "11"
	if _, err := f.s.RemoveOnlineUser(ctx, kick); err != nil {
		t.Fatal(err)
	}
	if resolve(t, f.s, one).LoggedIn || resolve(t, f.s, two).LoggedIn {
		t.Fatal("kick did not revoke every user session")
	}
	one, _, _ = login(t, f, "user@example.test", "again")
	if _, err := f.s.UpdateUserDisable(ctx, adminInput(map[string]any{"id": 11, "isDisable": 1}, admin)); err != nil {
		t.Fatal(err)
	}
	if resolve(t, f.s, one).LoggedIn {
		t.Fatal("disable did not invalidate token")
	}
	if _, err := f.s.UpdateUserDisable(ctx, adminInput(map[string]any{"id": 11, "isDisable": 0}, user)); err == nil {
		t.Fatal("disabled or unprivileged user changed account")
	}
	routes := f.s.Routes()
	if len(routes) != len(identity.RouteSpecs) {
		t.Fatal("missing identity routes")
	}
	engine, err := platform.NewRouter(routes, platform.Callbacks{Resolve: f.s.Resolve, Authorize: f.s.Authorize}, f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/admin/users/disable", strings.NewReader(`{"id":1,"isDisable":1}`))
	r.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, r)
	var response platform.Result
	if json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Flag {
		t.Fatalf("anonymous management HTTP succeeded: %s", w.Body)
	}
	if number(t, f, "SELECT is_disable FROM tb_user_info WHERE id=1") != 0 {
		t.Fatal("unauthorized HTTP changed data")
	}
}
