package files

import (
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"blog-go/internal/platform"
)

// 用途：向浏览器提供 GET/HEAD、Range、图片与导出下载响应。
func (s *Service) ServeFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/files/")
	parts := strings.Split(key, "/")
	if !strings.HasPrefix(r.URL.Path, "/files/") || len(parts) != 2 || !validName(parts[1]) || strings.ContainsAny(key, "\\\x00") || path.Clean(key) != key {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if !validStoredKey(key) && !(parts[0] == "defaults" && (parts[1] == "avatar.svg" || parts[1] == "cover.svg")) {
		http.NotFound(w, r)
		return
	}
	if parts[0] == "exports" {
		// Resource authorization is performed by platform.AdaptRoute before entering Raw.
		actor, ok := r.Context().Value(platform.Actor{}).(platform.Actor)
		if !ok || !actor.LoggedIn || actor.UserInfoID <= 0 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
	}
	store := &LocalStore{Root: filepath.Join(s.cfg.DataDir, "files"), PublicURL: s.cfg.PublicURL}
	directory, err := store.openScope(parts[0], false)
	if err != nil {
		fileReadError(w, r, err)
		return
	}
	defer directory.Close()
	file, err := openRegular(int(directory.Fd()), parts[1])
	if err != nil {
		fileReadError(w, r, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fileReadError(w, r, err)
		return
	}
	typ := fileMIME(path.Ext(parts[1]))
	if parts[0] == "defaults" {
		typ = "image/svg+xml"
	}
	w.Header().Set("Content-Type", typ)
	if parts[0] == "exports" {
		name := r.URL.Query().Get("filename")
		if !validName(name) || strings.ToLower(path.Ext(name)) != path.Ext(parts[1]) {
			name = parts[1]
		}
		w.Header().Set("Content-Disposition", attachment(name))
	}
	http.ServeContent(w, r, parts[1], info.ModTime(), file)
}
func fileReadError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, "forbidden", http.StatusForbidden)
}

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	handlers := []platform.Endpoint{s.SaveArticleImages, s.SaveConfigImages, s.SaveAlbumCover, s.SaveTalkImages}
	routes := make([]platform.Route, len(RouteSpecs))
	for index, spec := range RouteSpecs {
		routes[index] = platform.Route{Spec: spec, Handle: handlers[index]}
	}
	return routes
}

// 用途：作为共享文件存储与 HTTP 读取路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "POST", Path: "/admin/articles/images", Operation: "SaveArticleImages", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/config/images", Operation: "SaveConfigImages", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/photos/albums/cover", Operation: "SaveAlbumCover", Policy: "admin", Audit: false},
	{Method: "POST", Path: "/admin/talks/images", Operation: "SaveTalkImages", Policy: "admin", Audit: false},
}

// P-106 HTTP POST /admin/articles/images

// P-107 HTTP POST /admin/config/images

// P-108 HTTP POST /admin/photos/albums/cover

// P-109 HTTP POST /admin/talks/images

// P-110 HTTP GET /files/{path...}
// 响应变化：新增受限文件读取出口；GET内容或HEAD元数据、标准MIME/Range/下载头，具体头与路径限制见MODULE-009。
// 约束变化：新增受限目录读取，图片/音频/下载 MIME、Range、nosniff和附件头；不存在404、越界403；图片解码、语音播放与正文下载验证。 具体字段与状态解释见MODULE-009已确认执行边界。

// P-111 HTTP HEAD /files/{path...}
// 响应变化：新增受限文件读取出口；GET内容或HEAD元数据、标准MIME/Range/下载头，具体头与路径限制见MODULE-009。
// 约束变化：与GET相同元数据和权限范围，无body，Range/Content-Length按标准；非资源目录不列举。 具体字段与状态解释见MODULE-009已确认执行边界。
