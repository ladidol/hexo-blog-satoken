package files

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"blog-go/internal/platform"
	_ "golang.org/x/image/webp"
)

// Service owns file validation and conservative compensation of unreferenced files.
type Service struct {
	db     *sql.DB
	cfg    platform.Config
	now    func() time.Time
	client *http.Client
	store  Store
	mu     sync.Mutex
	reused map[string]bool
}

func New(db *sql.DB, cfg platform.Config, client *http.Client, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	s := &Service{db: db, cfg: cfg, now: now, client: client, reused: make(map[string]bool)}
	s.store = &LocalStore{Root: filepath.Join(cfg.DataDir, "files"), PublicURL: cfg.PublicURL}
	if cfg.Values["BLOG_UPLOAD_MODE"] == "oss" {
		s.store = newOSSStore(cfg, client)
	}
	return s
}

// StoredFile reports whether this call created the object and may request compensation.
type StoredFile struct {
	Key     string
	URL     string
	Created bool
}

// Store provides the same validated object contract for local and OSS storage.
type Store interface {
	Put(ctx context.Context, scope, name, contentType string, body io.Reader) (StoredFile, error)
	Remove(ctx context.Context, file StoredFile) error
}

func (s *Service) Put(ctx context.Context, scope, name, contentType string, body io.Reader) (StoredFile, error) {
	// Keep exports behind the application's Cookie/resource authorization even in OSS mode.
	store := s.store
	if scope == "exports" {
		store = &LocalStore{Root: filepath.Join(s.cfg.DataDir, "files"), PublicURL: s.cfg.PublicURL}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := store.Put(ctx, scope, name, contentType, body)
	if err == nil && !file.Created {
		s.reused[file.Key] = true
	}
	return file, err
}

func (s *Service) Discard(ctx context.Context, file StoredFile) error {
	if !file.Created {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reused[file.Key] {
		return nil
	} // Another upload may still be waiting to commit its reference.
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.db == nil {
		return errors.New("file cleanup retained object: database reference check unavailable")
	}
	if !validStoredKey(file.Key) {
		return invalidFile()
	}
	// Serialize with SQLite writers while checking all owners and removing the object.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("file cleanup reference check: %w", err)
	}
	defer tx.Rollback()
	refs := []struct {
		table   string
		columns []string
	}{
		{"tb_user_info", []string{"avatar"}}, {"tb_article", []string{"article_cover", "article_content"}},
		{"tb_photo", []string{"photo_src"}}, {"tb_photo_album", []string{"album_cover"}},
		{"tb_talk", []string{"images", "content"}}, {"tb_page", []string{"page_cover"}},
		{"tb_website_config", []string{"config"}}, {"tb_about", []string{"content"}},
		{"tb_chat_record", []string{"content", "avatar"}},
		{"tb_message", []string{"avatar", "message_content"}},
		{"tb_comment", []string{"comment_content"}}, {"tb_friend_link", []string{"link_avatar"}},
	}
	for _, ref := range refs {
		var conditions []string
		var args []any
		for _, column := range ref.columns {
			conditions = append(conditions, "instr(COALESCE("+column+",''),?)>0")
			args = append(args, file.Key)
		}
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+ref.table+" WHERE "+strings.Join(conditions, " OR ")+")", args...).Scan(&exists); err != nil {
			return fmt.Errorf("file cleanup reference check: %w", err)
		}
		if exists {
			return nil
		}
	}
	store := s.store
	if strings.HasPrefix(file.Key, "exports/") {
		store = &LocalStore{Root: filepath.Join(s.cfg.DataDir, "files"), PublicURL: s.cfg.PublicURL}
	}
	if err := store.Remove(ctx, file); err != nil {
		return fmt.Errorf("file cleanup retained orphan %s: %w", file.Key, err)
	}
	return nil
}

func (s *Service) SaveArticleImages(ctx context.Context, in platform.Input) (any, error) {
	return s.saveImage(ctx, in, "articles")
}
func (s *Service) SaveConfigImages(ctx context.Context, in platform.Input) (any, error) {
	return s.saveImage(ctx, in, "config")
}
func (s *Service) SaveAlbumCover(ctx context.Context, in platform.Input) (any, error) {
	return s.saveImage(ctx, in, "photos")
}
func (s *Service) SaveTalkImages(ctx context.Context, in platform.Input) (any, error) {
	return s.saveImage(ctx, in, "talks")
}
func (s *Service) saveImage(ctx context.Context, in platform.Input, scope string) (any, error) {
	if len(in.Files) != 1 || in.Files[0] == nil {
		return nil, invalidFile()
	}
	header := in.Files[0]
	if header.Size > maxFileBytes {
		return nil, &http.MaxBytesError{Limit: maxFileBytes}
	}
	body, err := header.Open()
	if err != nil {
		return nil, fmt.Errorf("open upload: %w", err)
	}
	defer body.Close()
	file, err := s.Put(ctx, scope, header.Filename, header.Header.Get("Content-Type"), body)
	if err != nil {
		return nil, err
	}
	return file.URL, nil
}

const maxFileBytes int64 = 100 << 20
const maxImagePixels = 16 << 20

func invalidFile() error {
	return &platform.Error{Code: platform.CodeInvalid, Message: "文件名称、类型或内容不正确"}
}
func validName(name string) bool {
	return name != "" && len(name) <= 255 && utf8.ValidString(name) && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00\r\n:")
}
func scopeKind(scope string) string {
	switch scope {
	case "avatar", "avatars", "articles", "photos", "config", "talks":
		return "image"
	case "voice", "audio":
		return "audio"
	case "exports":
		return "text"
	}
	return ""
}
func validStoredKey(key string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 2 || scopeKind(parts[0]) == "" {
		return false
	}
	ext := path.Ext(parts[1])
	hash := strings.TrimSuffix(parts[1], ext)
	if parts[0] == "exports" {
		if id, rest, found := strings.Cut(hash, "-"); found {
			if !validArticleExportID(id) {
				return false
			}
			hash = rest
		}
	}
	if len(hash) != 64 || ext == "" {
		return false
	}
	for _, c := range hash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return fileMIME(ext) != ""
}
func fileMIME(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".webm":
		return "audio/webm"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	}
	return ""
}

type contextReader struct {
	ctx  context.Context
	body io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.body.Read(p)
}

type preparedFile struct {
	key, contentType string
	data             []byte
}

func prepareFile(ctx context.Context, scope, name, contentType string, body io.Reader) (preparedFile, error) {
	var out preparedFile
	if err := ctx.Err(); err != nil {
		return out, err
	}
	kind := scopeKind(scope)
	if kind == "" || !validName(name) || body == nil {
		return out, invalidFile()
	}
	ext := strings.ToLower(path.Ext(name))
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	expected, _, _ := mime.ParseMediaType(fileMIME(ext))
	declared := ""
	if contentType != "" {
		var err error
		declared, _, err = mime.ParseMediaType(contentType)
		if err != nil {
			return out, invalidFile()
		}
	}
	switch declared {
	case "image/jpg":
		declared = "image/jpeg"
	case "audio/x-wav", "audio/wave", "audio/vnd.wave":
		declared = "audio/wav"
	case "application/ogg":
		declared = "audio/ogg"
	case "video/webm":
		declared = "audio/webm"
	}
	if expected == "" {
		return out, invalidFile()
	}
	if declared != "" && declared != "application/octet-stream" && declared != expected {
		// A browser may preserve either the original or the encoder's raster
		// MIME. Both declarations must remain within the supported image set.
		supportedRaster := declared == "image/jpeg" || declared == "image/png" || declared == "image/gif" || declared == "image/webp"
		if kind != "image" || !strings.HasPrefix(expected, "image/") || !supportedRaster {
			return out, invalidFile()
		}
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, body}, maxFileBytes+1))
	if err != nil {
		return out, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(data)) > maxFileBytes {
		return out, &http.MaxBytesError{Limit: maxFileBytes}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if len(data) == 0 && kind != "text" {
		return out, invalidFile()
	}
	switch kind {
	case "image":
		if !strings.HasPrefix(expected, "image/") {
			return out, invalidFile()
		}
		info, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || info.Width <= 0 || info.Height <= 0 || info.Width > 16384 || info.Height > 16384 || int64(info.Width)*int64(info.Height) > maxImagePixels {
			return out, invalidFile()
		}
		// The existing frontend's image-conversion pipeline can label JPEG bytes
		// with the original PNG name/MIME. Only normalize after actual decoding;
		// the accepted raster formats and pixel limits remain the same.
		switch format {
		case "jpeg":
			ext = ".jpg"
		case "png", "gif", "webp":
			ext = "." + format
		default:
			return out, invalidFile()
		}
		if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return out, invalidFile()
		}
	case "audio":
		detected := http.DetectContentType(data)
		valid := false
		switch ext {
		case ".wav":
			valid = validWAV(data)
		case ".mp3":
			valid = validMP3(data)
		case ".ogg":
			valid = detected == "application/ogg" && len(data) >= 28 && (bytes.Contains(data, []byte("OpusHead")) || bytes.Contains(data, []byte("vorbis")))
		case ".webm":
			valid = detected == "video/webm" && len(data) >= 16
		}
		if !valid {
			return out, invalidFile()
		}
	case "text":
		if ext != ".md" && ext != ".txt" || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return out, invalidFile()
		}
	}
	digest := sha256.Sum256(data)
	key := fmt.Sprintf("%s/%x%s", scope, digest, ext)
	if scope == "exports" && strings.HasPrefix(name, "article-") {
		id, _, found := strings.Cut(strings.TrimPrefix(name, "article-"), "-")
		if !found || !validArticleExportID(id) {
			return out, invalidFile()
		}
		key = fmt.Sprintf("exports/%s-%x%s", id, digest, ext)
	}
	return preparedFile{key: key, contentType: fileMIME(ext), data: data}, nil
}
func validArticleExportID(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}
func validWAV(data []byte) bool {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return false
	}
	format, audio := false, false
	for pos := 12; pos+8 <= len(data); {
		size := uint64(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		start := pos + 8
		if uint64(start)+size > uint64(len(data)) {
			return false
		}
		switch string(data[pos : pos+4]) {
		case "fmt ":
			if size < 16 {
				return false
			}
			code := binary.LittleEndian.Uint16(data[start:])
			channels := binary.LittleEndian.Uint16(data[start+2:])
			rate := binary.LittleEndian.Uint32(data[start+4:])
			bits := binary.LittleEndian.Uint16(data[start+14:])
			format = (code == 1 || code == 3) && channels > 0 && channels <= 8 && rate > 0 && rate <= 384000 && bits > 0 && bits <= 64
		case "data":
			audio = size > 0
		}
		pos = start + int(size) + int(size%2)
	}
	return format && audio
}
func localFileURL(publicURL, key, name string) string {
	result := strings.TrimRight(publicURL, "/") + "/files/" + key
	if strings.HasPrefix(key, "exports/") {
		if strings.HasPrefix(name, "article-") {
			id, title, found := strings.Cut(strings.TrimPrefix(name, "article-"), "-")
			if found && validArticleExportID(id) {
				name = title
			}
		}
		result += "?" + url.Values{"filename": []string{name}}.Encode()
	}
	return result
}
func attachment(name string) string {
	// PathEscape produces UTF-8 percent encoding; additionally escape RFC5987 delimiters.
	escaped := url.PathEscape(name)
	escaped = strings.NewReplacer("'", "%27", "(", "%28", ")", "%29", "*", "%2A").Replace(escaped)
	return "attachment; filename=\"download" + strings.ToLower(path.Ext(name)) + "\"; filename*=UTF-8''" + escaped
}

// Validate complete Layer III frames with or without an optional ID3v2 metadata prefix.
func validMP3(data []byte) bool {
	pos := 0
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		if data[3] > 4 || data[3] < 2 {
			return false
		}
		for _, b := range data[6:10] {
			if b&0x80 != 0 {
				return false
			}
		}
		size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
		pos = 10 + size
		if data[3] == 4 && data[5]&0x10 != 0 {
			pos += 10
		}
	}
	frames := 0
	for pos+4 <= len(data) {
		if frames > 0 && len(data)-pos == 128 && string(data[pos:pos+3]) == "TAG" {
			return true
		}
		h := data[pos : pos+4]
		if h[0] != 0xff || h[1]&0xe0 != 0xe0 || h[1]>>1&3 != 1 {
			return false
		}
		version := h[1] >> 3 & 3
		rateIndex := h[2] >> 2 & 3
		bitrateIndex := h[2] >> 4
		if version == 1 || rateIndex == 3 || bitrateIndex == 0 || bitrateIndex == 15 {
			return false
		}
		rates := [3]int{44100, 48000, 32000}
		rate := rates[rateIndex]
		bitrates := [14]int{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
		multiplier := 144
		if version != 3 {
			bitrates = [14]int{8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
			rate /= 2
			multiplier = 72
		}
		if version == 0 {
			rate /= 2
		}
		size := multiplier*bitrates[bitrateIndex-1]*1000/rate + int(h[2]>>1&1)
		if size < 4 || pos+size > len(data) {
			return false
		}
		pos += size
		frames++
	}
	return frames > 0 && pos == len(data)
}
