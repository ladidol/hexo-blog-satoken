package media

import "blog-go/internal/platform"

// 用途：将下列原路径逐项绑定本模块真实业务方法。
func (s *Service) Routes() []platform.Route {
	handlers := []platform.Endpoint{
		s.SaveOrUpdatePhotoAlbum,
		s.ListPhotoAlbumBacks,
		s.ListPhotoAlbumBackInfos,
		s.GetPhotoAlbumBackById,
		s.DeletePhotoAlbumById,
		s.ListPhotoAlbums,
		s.ListPhotos,
		s.UpdatePhoto,
		s.SavePhotos,
		s.UpdatePhotosAlbum,
		s.UpdatePhotoDelete,
		s.DeletePhotos,
		s.ListPhotosByAlbumId,
		s.ListHomeTalks,
		s.ListTalks,
		s.GetTalkById,
		s.SaveTalkLike,
		s.SaveOrUpdateTalk,
		s.DeleteTalks,
		s.ListBackTalks,
		s.GetBackTalkById,
	}
	routes := make([]platform.Route, len(RouteSpecs))
	for index, spec := range RouteSpecs {
		routes[index] = platform.Route{Spec: spec, Handle: handlers[index]}
	}
	return routes
}

// 用途：作为说说与相册照片路由、权限和行为追溯的唯一注册清单。
var RouteSpecs = []platform.RouteSpec{
	{Method: "POST", Path: "/admin/photos/albums", Operation: "SaveOrUpdatePhotoAlbum", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/admin/photos/albums", Operation: "ListPhotoAlbumBacks", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/photos/albums/info", Operation: "ListPhotoAlbumBackInfos", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/photos/albums/{albumId}/info", Operation: "GetPhotoAlbumBackById", Policy: "admin", Audit: false},
	{Method: "DELETE", Path: "/admin/photos/albums/{albumId}", Operation: "DeletePhotoAlbumById", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/photos/albums", Operation: "ListPhotoAlbums", Policy: "public", Audit: false},
	{Method: "GET", Path: "/admin/photos", Operation: "ListPhotos", Policy: "admin", Audit: false},
	{Method: "PUT", Path: "/admin/photos", Operation: "UpdatePhoto", Policy: "admin", Audit: true},
	{Method: "POST", Path: "/admin/photos", Operation: "SavePhotos", Policy: "admin", Audit: true},
	{Method: "PUT", Path: "/admin/photos/album", Operation: "UpdatePhotosAlbum", Policy: "admin", Audit: true},
	{Method: "PUT", Path: "/admin/photos/delete", Operation: "UpdatePhotoDelete", Policy: "admin", Audit: true},
	{Method: "DELETE", Path: "/admin/photos", Operation: "DeletePhotos", Policy: "admin", Audit: true},
	{Method: "GET", Path: "/albums/{albumId}/photos", Operation: "ListPhotosByAlbumId", Policy: "public", Audit: false},
	{Method: "GET", Path: "/home/talks", Operation: "ListHomeTalks", Policy: "public", Audit: false},
	{Method: "GET", Path: "/talks", Operation: "ListTalks", Policy: "public", Audit: false},
	{Method: "GET", Path: "/talks/{talkId}", Operation: "GetTalkById", Policy: "public", Audit: false},
	{Method: "POST", Path: "/talks/{talkId}/like", Operation: "SaveTalkLike", Policy: "user", Audit: false},
	{Method: "POST", Path: "/admin/talks", Operation: "SaveOrUpdateTalk", Policy: "admin", Audit: false},
	{Method: "DELETE", Path: "/admin/talks", Operation: "DeleteTalks", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/talks", Operation: "ListBackTalks", Policy: "admin", Audit: false},
	{Method: "GET", Path: "/admin/talks/{talkId}", Operation: "GetBackTalkById", Policy: "admin", Audit: false},
}

// P-071 HTTP POST /admin/photos/albums

// P-072 HTTP GET /admin/photos/albums

// P-073 HTTP GET /admin/photos/albums/info

// P-074 HTTP GET /admin/photos/albums/{albumId}/info

// P-075 HTTP DELETE /admin/photos/albums/{albumId}

// P-076 HTTP GET /photos/albums

// P-077 HTTP GET /admin/photos

// P-078 HTTP PUT /admin/photos

// P-079 HTTP POST /admin/photos

// P-080 HTTP PUT /admin/photos/album

// P-081 HTTP PUT /admin/photos/delete

// P-082 HTTP DELETE /admin/photos

// P-083 HTTP GET /albums/{albumId}/photos

// P-084 HTTP GET /home/talks

// P-085 HTTP GET /talks

// P-086 HTTP GET /talks/{talkId}

// P-087 HTTP POST /talks/{talkId}/like

// P-088 HTTP POST /admin/talks

// P-089 HTTP DELETE /admin/talks

// P-090 HTTP GET /admin/talks

// P-091 HTTP GET /admin/talks/{talkId}
