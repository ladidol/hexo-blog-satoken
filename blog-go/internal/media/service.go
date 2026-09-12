package media

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"blog-go/internal/engagement"
	"blog-go/internal/platform"
)

// 用途：拥有说说与相册照片的状态、数据访问及下列显式依赖。
type Service struct {
	db         *sql.DB
	cfg        platform.Config
	now        func() time.Time
	engagement *engagement.Service
}

// 用途：构造说说与相册照片，由 app 唯一装配依赖。
func New(db *sql.DB, cfg platform.Config, engagement *engagement.Service, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if cfg.TimeZone == nil {
		cfg.TimeZone = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return &Service{db: db, cfg: cfg, engagement: engagement, now: now}
}

// 用途：承接原 POST /admin/photos/albums 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdatePhotoAlbum(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		ID                    *int64
		AlbumName, AlbumCover string
		AlbumDesc             *string
		Status                *int
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	request.AlbumName = strings.TrimSpace(request.AlbumName)
	if !textValid(request.AlbumName, 20, false) || request.AlbumDesc != nil && !textValid(*request.AlbumDesc, 50, true) || !mediaURL(request.AlbumCover) || request.ID != nil && *request.ID <= 0 || request.Status != nil && *request.Status != 1 && *request.Status != 2 {
		return nil, invalid("相册字段不正确")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id := int64(0)
	if request.ID != nil {
		id = *request.ID
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tb_photo_album WHERE album_name=? COLLATE NOCASE AND id<>?)", request.AlbumName, id).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, &platform.Error{Code: platform.CodeFail, Message: "相册名已存在"}
	}
	if request.ID == nil {
		status := 1
		if request.Status != nil {
			status = *request.Status
		}
		description := ""
		if request.AlbumDesc != nil {
			description = *request.AlbumDesc
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO tb_photo_album(album_name,album_desc,album_cover,status,create_time) VALUES(?,?,?,?,?)", request.AlbumName, description, request.AlbumCover, status, s.timestamp())
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE tb_photo_album SET album_name=?,album_desc=COALESCE(?,album_desc),album_cover=?,status=COALESCE(?,status),update_time=? WHERE id=?", request.AlbumName, request.AlbumDesc, request.AlbumCover, request.Status, s.timestamp(), id)
		if err == nil {
			err = requireAffected(result, "相册不存在")
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /admin/photos/albums 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListPhotoAlbumBacks(ctx context.Context, in platform.Input) (any, error) {
	where := " FROM tb_photo_album a WHERE a.is_delete=0 AND a.album_name LIKE ? ESCAPE '\\'"
	return s.page(ctx, in, where, albumColumns+",(SELECT count(*) FROM tb_photo p WHERE p.album_id=a.id AND p.is_delete=0),a.status", "a.id DESC", []any{searchPattern(in.Query.Get("keywords"))}, scanAlbumBack)
}

// 用途：承接原 GET /admin/photos/albums/info 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListPhotoAlbumBackInfos(ctx context.Context, in platform.Input) (any, error) {
	return s.readAlbums(ctx, false)
}

// 用途：承接原 GET /admin/photos/albums/{albumId}/info 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) GetPhotoAlbumBackById(ctx context.Context, in platform.Input) (any, error) {
	id, err := pathID(in, "albumId")
	if err != nil {
		return nil, err
	}
	value, err := scanAlbumBack(s.db.QueryRowContext(ctx, "SELECT "+albumColumns+",(SELECT count(*) FROM tb_photo p WHERE p.album_id=a.id AND p.is_delete=0),a.status FROM tb_photo_album a WHERE a.id=?", id))
	return value, missingRow(err, "相册不存在")
}

// 用途：承接原 DELETE /admin/photos/albums/{albumId} 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeletePhotoAlbumById(ctx context.Context, in platform.Input) (any, error) {
	id, err := pathID(in, "albumId")
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireIDs(ctx, tx, "tb_photo_album", []int64{id}); err != nil {
		return nil, err
	}
	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_photo WHERE album_id=?", id).Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, "DELETE FROM tb_photo_album WHERE id=?", id)
	} else {
		if _, err = tx.ExecContext(ctx, "UPDATE tb_photo_album SET is_delete=1,update_time=? WHERE id=?", s.timestamp(), id); err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE tb_photo SET is_delete=1,update_time=? WHERE album_id=?", s.timestamp(), id)
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /photos/albums 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListPhotoAlbums(ctx context.Context, in platform.Input) (any, error) {
	return s.readAlbums(ctx, true)
}

// 用途：承接原 GET /admin/photos 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListPhotos(ctx context.Context, in platform.Input) (any, error) {
	deleted, err := queryFlag(in.Query, "isDelete", 0, 0, 1)
	if err != nil {
		return nil, err
	}
	where := " FROM tb_photo p WHERE p.is_delete=?"
	args := []any{deleted}
	if _, ok := in.Query["albumId"]; ok {
		id, err := queryID(in.Query, "albumId")
		if err != nil {
			return nil, err
		}
		where += " AND p.album_id=?"
		args = append(args, id)
	}
	return s.page(ctx, in, where, "p.id,p.photo_name,p.photo_desc,p.photo_src", "p.id DESC,p.update_time DESC", args, scanPhoto)
}

// 用途：承接原 PUT /admin/photos 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdatePhoto(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		ID        int64
		PhotoName string
		PhotoDesc *string
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	request.PhotoName = strings.TrimSpace(request.PhotoName)
	if request.ID <= 0 || !textValid(request.PhotoName, 20, false) || request.PhotoDesc != nil && !textValid(*request.PhotoDesc, 50, true) {
		return nil, invalid("照片字段不正确")
	}
	result, err := s.db.ExecContext(ctx, "UPDATE tb_photo SET photo_name=?,photo_desc=COALESCE(?,photo_desc),update_time=? WHERE id=?", request.PhotoName, request.PhotoDesc, s.timestamp(), request.ID)
	if err != nil {
		return nil, err
	}
	return nil, requireAffected(result, "照片不存在")
}

// 用途：承接原 POST /admin/photos 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SavePhotos(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		AlbumID      int64
		PhotoURLList []string
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	if request.AlbumID <= 0 || len(request.PhotoURLList) == 0 || len(request.PhotoURLList) > maxBatch {
		return nil, invalid("照片列表不正确")
	}
	for _, address := range request.PhotoURLList {
		if !mediaURL(address) {
			return nil, invalid("照片地址不正确")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := activeAlbum(ctx, tx, request.AlbumID); err != nil {
		return nil, err
	}
	stamp := s.timestamp()
	for _, address := range request.PhotoURLList {
		result, err := tx.ExecContext(ctx, "INSERT INTO tb_photo(album_id,photo_name,photo_src,create_time) VALUES(?,'',?,?)", request.AlbumID, address, stamp)
		if err != nil {
			return nil, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE tb_photo SET photo_name=? WHERE id=?", strconv.FormatInt(id, 10), id); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

// 用途：承接原 PUT /admin/photos/album 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdatePhotosAlbum(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		AlbumID     int64
		PhotoIDList []int64
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	if request.AlbumID <= 0 {
		return nil, invalid("相册 ID 不正确")
	}
	ids, err := validateIDs(request.PhotoIDList)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := activeAlbum(ctx, tx, request.AlbumID); err != nil {
		return nil, err
	}
	if err := requireIDs(ctx, tx, "tb_photo", ids); err != nil {
		return nil, err
	}
	args := append([]any{request.AlbumID, s.timestamp()}, idArguments(ids)...)
	if _, err := tx.ExecContext(ctx, "UPDATE tb_photo SET album_id=?,update_time=? WHERE id IN ("+placeholders(ids)+")", args...); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 PUT /admin/photos/delete 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) UpdatePhotoDelete(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		IDList   []int64
		IsDelete *int
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	if request.IsDelete == nil || *request.IsDelete != 0 && *request.IsDelete != 1 {
		return nil, invalid("照片删除状态不正确")
	}
	ids, err := validateIDs(request.IDList)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireIDs(ctx, tx, "tb_photo", ids); err != nil {
		return nil, err
	}
	args := append([]any{*request.IsDelete, s.timestamp()}, idArguments(ids)...)
	if _, err := tx.ExecContext(ctx, "UPDATE tb_photo SET is_delete=?,update_time=? WHERE id IN ("+placeholders(ids)+")", args...); err != nil {
		return nil, err
	}
	if *request.IsDelete == 0 {
		args = append([]any{s.timestamp()}, idArguments(ids)...)
		if _, err := tx.ExecContext(ctx, "UPDATE tb_photo_album SET is_delete=0,update_time=? WHERE id IN (SELECT album_id FROM tb_photo WHERE id IN ("+placeholders(ids)+"))", args...); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

// 用途：承接原 DELETE /admin/photos 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeletePhotos(ctx context.Context, in platform.Input) (any, error) {
	ids, err := listInput(in.Body)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireIDs(ctx, tx, "tb_photo", ids); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM tb_photo WHERE id IN ("+placeholders(ids)+")", idArguments(ids)...); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /albums/{albumId}/photos 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListPhotosByAlbumId(ctx context.Context, in platform.Input) (any, error) {
	id, err := pathID(in, "albumId")
	if err != nil {
		return nil, err
	}
	current, size, err := pagination(in.Query)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var name, cover string
	if err := tx.QueryRowContext(ctx, "SELECT album_name,album_cover FROM tb_photo_album WHERE id=? AND status=1 AND is_delete=0", id).Scan(&name, &cover); err != nil {
		return nil, missingRow(err, "相册不存在")
	}
	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM tb_photo WHERE album_id=? AND is_delete=0", id).Scan(&count); err != nil {
		return nil, err
	}
	photos := []string{}
	if count > 0 && current-1 <= (count-1)/size {
		rows, err := tx.QueryContext(ctx, "SELECT photo_src FROM tb_photo WHERE album_id=? AND is_delete=0 ORDER BY id DESC LIMIT ? OFFSET ?", id, size, (current-1)*size)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var source string
			if err := rows.Scan(&source); err != nil {
				rows.Close()
				return nil, err
			}
			photos = append(photos, source)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return platform.Object{"photoAlbumCover": cover, "photoAlbumName": name, "photoList": photos}, tx.Commit()
}

// 用途：承接原 GET /home/talks 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListHomeTalks(ctx context.Context, in platform.Input) (any, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT content FROM tb_talk WHERE status=1 ORDER BY is_top DESC,create_time DESC,id DESC LIMIT 10")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	talks := []string{}
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		runes := []rune(content)
		if len(runes) > 200 {
			content = string(runes[:200])
		}
		content = homeEntities.ReplaceAllString(content, "")
		content = homeScripts.ReplaceAllString(content, "")
		content = homeStyles.ReplaceAllString(content, "")
		talks = append(talks, content)
	}
	return talks, rows.Err()
}

// 用途：承接原 GET /talks 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListTalks(ctx context.Context, in platform.Input) (any, error) {
	return s.page(ctx, in, " FROM tb_talk t JOIN tb_user_info u ON u.id=t.user_id WHERE t.status=1", publicTalkColumns, "t.is_top DESC,t.create_time DESC,t.id DESC", nil, scanTalk)
}

// 用途：承接原 GET /talks/{talkId} 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) GetTalkById(ctx context.Context, in platform.Input) (any, error) {
	id, err := pathID(in, "talkId")
	if err != nil {
		return nil, err
	}
	value, err := scanTalk(s.db.QueryRowContext(ctx, "SELECT "+publicTalkColumns+" FROM tb_talk t JOIN tb_user_info u ON u.id=t.user_id WHERE t.id=? AND t.status=1", id))
	return value, missingRow(err, "说说不存在")
}

// 用途：承接原 POST /talks/{talkId}/like 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveTalkLike(ctx context.Context, in platform.Input) (any, error) {
	id, err := pathID(in, "talkId")
	if err != nil {
		return nil, err
	}
	if !in.Actor.LoggedIn || in.Actor.UserInfoID <= 0 {
		return nil, &platform.Error{Code: platform.CodeFail, Message: "请先登录"}
	}
	if s.engagement == nil {
		return nil, errors.New("engagement service is required")
	}
	_, _, err = s.engagement.Toggle(ctx, in.Actor.UserInfoID, 3, id)
	var business *platform.Error
	if errors.As(err, &business) && business.Code == platform.CodeMissing {
		// The public wire reserves 52002 for account lookup; invisible media is
		// an ordinary business failure. Keep the shared service error as a cause.
		return nil, errors.Join(&platform.Error{Code: platform.CodeFail, Message: business.Message}, err)
	}
	return nil, err
}

// 用途：承接原 POST /admin/talks 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) SaveOrUpdateTalk(ctx context.Context, in platform.Input) (any, error) {
	var request struct {
		ID            *int64
		Content       string
		Images        *string
		IsTop, Status *int
	}
	if err := objectInput(in.Body, &request); err != nil {
		return nil, err
	}
	if request.ID != nil && *request.ID <= 0 || !textValid(request.Content, 2000, false) || request.IsTop == nil || *request.IsTop != 0 && *request.IsTop != 1 || request.Status == nil || *request.Status != 1 && *request.Status != 2 {
		return nil, invalid("说说字段不正确")
	}
	if request.Images != nil {
		if _, err := imageList(*request.Images); err != nil {
			return nil, err
		}
	}
	if !in.Actor.LoggedIn || in.Actor.UserInfoID <= 0 {
		return nil, &platform.Error{Code: platform.CodeFail, Message: "请先登录"}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var disabled int
	if err := tx.QueryRowContext(ctx, "SELECT is_disable FROM tb_user_info WHERE id=?", in.Actor.UserInfoID).Scan(&disabled); err != nil {
		return nil, missingRow(err, "用户不存在")
	}
	if disabled != 0 {
		return nil, &platform.Error{Code: platform.CodeFail, Message: "用户已禁用"}
	}
	if request.ID == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO tb_talk(user_id,content,images,is_top,status,create_time) VALUES(?,?,?,?,?,?)", in.Actor.UserInfoID, request.Content, request.Images, *request.IsTop, *request.Status, s.timestamp())
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, "UPDATE tb_talk SET user_id=?,content=?,images=?,is_top=?,status=?,update_time=? WHERE id=?", in.Actor.UserInfoID, request.Content, request.Images, *request.IsTop, *request.Status, s.timestamp(), *request.ID)
		if err == nil {
			err = requireAffected(result, "说说不存在")
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 DELETE /admin/talks 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) DeleteTalks(ctx context.Context, in platform.Input) (any, error) {
	ids, err := listInput(in.Body)
	if err != nil {
		return nil, err
	}
	if s.engagement == nil {
		return nil, errors.New("engagement service is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireIDs(ctx, tx, "tb_talk", ids); err != nil {
		return nil, err
	}
	// Clean comment likes in bounded groups before deleting their owning comments.
	lastID := int64(0)
	for {
		args := append(idArguments(ids), lastID)
		rows, err := tx.QueryContext(ctx, "SELECT id FROM tb_comment WHERE type=3 AND topic_id IN ("+placeholders(ids)+") AND id>? ORDER BY id LIMIT 500", args...)
		if err != nil {
			return nil, err
		}
		commentIDs := []int64{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			commentIDs = append(commentIDs, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(commentIDs) == 0 {
			break
		}
		if err := s.engagement.DeleteTargetsTx(ctx, tx, 2, commentIDs); err != nil {
			return nil, err
		}
		lastID = commentIDs[len(commentIDs)-1]
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM tb_comment WHERE type=3 AND topic_id IN ("+placeholders(ids)+")", idArguments(ids)...); err != nil {
		return nil, err
	}
	if err := s.engagement.DeleteTargetsTx(ctx, tx, 3, ids); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM tb_talk WHERE id IN ("+placeholders(ids)+")", idArguments(ids)...); err != nil {
		return nil, err
	}
	return nil, tx.Commit()
}

// 用途：承接原 GET /admin/talks 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ListBackTalks(ctx context.Context, in platform.Input) (any, error) {
	where := " FROM tb_talk t JOIN tb_user_info u ON u.id=t.user_id WHERE 1=1"
	args := []any{}
	if _, ok := in.Query["status"]; ok {
		status, err := queryFlag(in.Query, "status", 1, 1, 2)
		if err != nil {
			return nil, err
		}
		where += " AND t.status=?"
		args = append(args, status)
	}
	return s.page(ctx, in, where, backTalkColumns, "t.is_top DESC,t.create_time DESC,t.id DESC", args, scanTalkBack)
}

// 用途：承接原 GET /admin/talks/{talkId} 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) GetBackTalkById(ctx context.Context, in platform.Input) (any, error) {
	id, err := pathID(in, "talkId")
	if err != nil {
		return nil, err
	}
	value, err := scanTalkBack(s.db.QueryRowContext(ctx, "SELECT "+backTalkColumns+" FROM tb_talk t JOIN tb_user_info u ON u.id=t.user_id WHERE t.id=?", id))
	return value, missingRow(err, "说说不存在")
}

// 用途：将本模块 schema.sql 交给唯一迁移器，业务逻辑不自动建表。
//
//go:embed schema.sql
var Schema string

// Public protocol projections use platform.Object; no database row is serialized directly.
// Integer/Long: JSON number; Date: Unix milliseconds; LocalDateTime: yyyy-MM-ddTHH:mm:ss.
// Optional fields retain null; lists/sets are arrays. UserDetailDTO never includes password.
// TalkDTO: id:Integer, nickname:String, avatar:String, content:String, images:String, imgList:String[], isTop:Integer, likeCount:Integer, commentCount:Integer, createTime:LocalDateTime
// TalkBackDTO: id:Integer, nickname:String, avatar:String, content:String, images:String, imgList:String[], isTop:Integer, status:Integer, createTime:LocalDateTime
// PhotoAlbumDTO: id:Integer, albumName:String, albumDesc:String, albumCover:String
// PhotoAlbumBackDTO: id:Integer, albumName:String, albumDesc:String, albumCover:String, photoCount:Integer, status:Integer
// PhotoDTO: photoAlbumCover:String, photoAlbumName:String, photoList:String[]
// PhotoBackDTO: id:Integer, photoName:String, photoDesc:String, photoSrc:String
// TalkVO: id:Integer, content:String, images:String, isTop:Integer, status:Integer
// PhotoAlbumVO: id:Integer, albumName:String, albumDesc:String, albumCover:String, status:Integer
// PhotoVO: albumId:Integer, photoUrlList:String[], photoIdList:Integer[]
// PhotoInfoVO: id:Integer, photoName:String, photoDesc:String

const maxBatch = 500
const albumColumns = "a.id,a.album_name,a.album_desc,a.album_cover"
const talkColumns = "t.id,u.nickname,u.avatar,t.content,t.images,t.is_top,t.create_time"
const publicTalkColumns = talkColumns + ",(SELECT count(*) FROM tb_like l WHERE l.target_type=3 AND l.target_id=t.id),(SELECT count(*) FROM tb_comment c WHERE c.type=3 AND c.topic_id=t.id AND c.is_review=1 AND c.is_delete=0 AND c.parent_id IS NULL)"
const backTalkColumns = talkColumns + ",t.status"

var homeEntities = regexp.MustCompile(`&.{2,6}?;`)
var homeScripts = regexp.MustCompile(`(?is)<\s*script[^>]*>.*?<\s*/\s*script\s*>`)
var homeStyles = regexp.MustCompile(`(?is)<\s*style[^>]*>.*?<\s*/\s*style\s*>`)

type scanner interface{ Scan(...any) error }

func (s *Service) timestamp() string { return s.now().In(s.cfg.TimeZone).Format("2006-01-02T15:04:05") }
func invalid(message string) error {
	return &platform.Error{Code: platform.CodeInvalid, Message: message}
}
func missingRow(err error, message string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return &platform.Error{Code: platform.CodeFail, Message: message}
	}
	return err
}
func objectInput(body json.RawMessage, destination any) error {
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "{") || json.Unmarshal(body, destination) != nil {
		return invalid("请求字段不正确")
	}
	return nil
}
func textValid(value string, limit int, empty bool) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && !strings.ContainsRune(value, 0) && (empty || strings.TrimSpace(value) != "")
}
func mediaURL(value string) bool {
	if !textValid(value, 255, false) || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Hostname() != "" && parsed.User == nil
}
func imageList(raw string) ([]string, error) {
	if !textValid(raw, 2500, true) || !strings.HasPrefix(strings.TrimSpace(raw), "[") {
		return nil, invalid("说说图片格式不正确")
	}
	var images []string
	if err := json.Unmarshal([]byte(raw), &images); err != nil {
		return nil, invalid("说说图片格式不正确")
	}
	for _, address := range images {
		if !mediaURL(address) {
			return nil, invalid("说说图片地址不正确")
		}
	}
	if images == nil {
		images = []string{}
	}
	return images, nil
}
func pathID(in platform.Input, name string) (int64, error) {
	value, err := strconv.ParseInt(in.Params[name], 10, 64)
	if err != nil || value <= 0 {
		return 0, invalid("资源 ID 不正确")
	}
	return value, nil
}
func queryID(query url.Values, name string) (int64, error) {
	values := query[name]
	if len(values) != 1 {
		return 0, invalid("资源 ID 不正确")
	}
	id, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || id <= 0 {
		return 0, invalid("资源 ID 不正确")
	}
	return id, nil
}
func queryFlag(query url.Values, name string, fallback, min, max int) (int, error) {
	values, ok := query[name]
	if !ok {
		return fallback, nil
	}
	if len(values) != 1 {
		return 0, invalid("状态参数不正确")
	}
	value, err := strconv.Atoi(values[0])
	if err != nil || value < min || value > max {
		return 0, invalid("状态参数不正确")
	}
	return value, nil
}
func pagination(query url.Values) (int64, int64, error) {
	values := [2]int64{1, 10}
	for i, key := range []string{"current", "size"} {
		if raw, ok := query[key]; ok {
			if len(raw) != 1 {
				return 0, 0, invalid("分页参数不正确")
			}
			value, err := strconv.ParseInt(raw[0], 10, 64)
			if err != nil || value < 1 || key == "size" && value > 100 {
				return 0, 0, invalid("分页参数不正确")
			}
			values[i] = value
		}
	}
	return values[0], values[1], nil
}
func validateIDs(values []int64) ([]int64, error) {
	if len(values) == 0 || len(values) > maxBatch {
		return nil, invalid("资源列表不正确")
	}
	seen := make(map[int64]bool, len(values))
	ids := make([]int64, 0, len(values))
	for _, id := range values {
		if id <= 0 {
			return nil, invalid("资源 ID 不正确")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}
func listInput(body json.RawMessage) ([]int64, error) {
	var ids []int64
	if json.Unmarshal(body, &ids) != nil {
		return nil, invalid("资源列表不正确")
	}
	return validateIDs(ids)
}
func placeholders(ids []int64) string { return strings.TrimRight(strings.Repeat("?,", len(ids)), ",") }
func idArguments(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
func requireIDs(ctx context.Context, tx *sql.Tx, table string, ids []int64) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id IN ("+placeholders(ids)+")", idArguments(ids)...).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return &platform.Error{Code: platform.CodeFail, Message: "资源不存在"}
	}
	return nil
}
func requireAffected(result sql.Result, message string) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return &platform.Error{Code: platform.CodeFail, Message: message}
	}
	return nil
}
func activeAlbum(ctx context.Context, tx *sql.Tx, id int64) error {
	var found int
	err := tx.QueryRowContext(ctx, "SELECT 1 FROM tb_photo_album WHERE id=? AND is_delete=0", id).Scan(&found)
	return missingRow(err, "相册不存在或已删除")
}
func scanAlbum(row scanner) (platform.Object, error) {
	var id int64
	var name, desc, cover string
	if err := row.Scan(&id, &name, &desc, &cover); err != nil {
		return nil, err
	}
	return platform.Object{"id": id, "albumName": name, "albumDesc": desc, "albumCover": cover}, nil
}
func scanAlbumBack(row scanner) (platform.Object, error) {
	var id, count, status int64
	var name, desc, cover string
	if err := row.Scan(&id, &name, &desc, &cover, &count, &status); err != nil {
		return nil, err
	}
	return platform.Object{"id": id, "albumName": name, "albumDesc": desc, "albumCover": cover, "photoCount": count, "status": status}, nil
}
func scanPhoto(row scanner) (platform.Object, error) {
	var id int64
	var name, source string
	var desc sql.NullString
	if err := row.Scan(&id, &name, &desc, &source); err != nil {
		return nil, err
	}
	var optional any
	if desc.Valid {
		optional = desc.String
	}
	return platform.Object{"id": id, "photoName": name, "photoDesc": optional, "photoSrc": source}, nil
}
func scanTalk(row scanner) (platform.Object, error)     { return scanTalkProjection(row, false) }
func scanTalkBack(row scanner) (platform.Object, error) { return scanTalkProjection(row, true) }
func scanTalkProjection(row scanner, back bool) (platform.Object, error) {
	var id, top, status, likes, comments int64
	var nickname, avatar, content, created string
	var images sql.NullString
	destination := []any{&id, &nickname, &avatar, &content, &images, &top, &created}
	if back {
		destination = append(destination, &status)
	} else {
		destination = append(destination, &likes, &comments)
	}
	if err := row.Scan(destination...); err != nil {
		return nil, err
	}
	list := []string{}
	var raw any
	if images.Valid {
		var err error
		list, err = imageList(images.String)
		if err != nil {
			return nil, fmt.Errorf("invalid stored talk images: %w", err)
		}
		raw = images.String
	}
	result := platform.Object{"id": id, "nickname": nickname, "avatar": avatar, "content": content, "images": raw, "imgList": list, "isTop": top, "createTime": created}
	if back {
		result["status"] = status
	} else {
		result["likeCount"] = likes
		result["commentCount"] = comments
	}
	return result, nil
}
func (s *Service) readAlbums(ctx context.Context, public bool) ([]platform.Object, error) {
	query := "SELECT " + albumColumns + " FROM tb_photo_album a WHERE a.is_delete=0"
	if public {
		query += " AND a.status=1"
	}
	query += " ORDER BY a.id DESC"
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []platform.Object{}
	for rows.Next() {
		album, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, album)
	}
	return result, rows.Err()
}

// Keep totals and their page in the same SQLite snapshot; never multiply huge page inputs.
func (s *Service) page(ctx context.Context, in platform.Input, from, columns, order string, args []any, scan func(scanner) (platform.Object, error)) (platform.Page, error) {
	page := platform.Page{RecordList: []platform.Object{}}
	current, size, err := pagination(in.Query)
	if err != nil {
		return page, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT count(*)"+from, args...).Scan(&page.Count); err != nil {
		return page, err
	}
	if page.Count > 0 && current-1 <= (page.Count-1)/size {
		parameters := append(append([]any{}, args...), size, (current-1)*size)
		rows, err := tx.QueryContext(ctx, "SELECT "+columns+from+" ORDER BY "+order+" LIMIT ? OFFSET ?", parameters...)
		if err != nil {
			return page, err
		}
		for rows.Next() {
			item, err := scan(rows)
			if err != nil {
				rows.Close()
				return page, err
			}
			page.RecordList = append(page.RecordList, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, err
		}
	}
	return page, tx.Commit()
}

func searchPattern(keyword string) string {
	return "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(keyword) + "%"
}
