package article

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"blog-go/internal/files"
	"blog-go/internal/platform"
	"gopkg.in/yaml.v3"
)

// 用途：承接原 POST /admin/articles/export 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ExportArticles(ctx context.Context, in platform.Input) (result any, err error) {
	ids, err := bodyIDs(in.Body)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	created := make([]files.StoredFile, 0, len(ids))
	defer func() {
		// Discard performs a database reference check, so release the snapshot
		// first. Compensation must also work after request cancellation.
		_ = tx.Rollback()
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			for _, file := range created {
				err = errors.Join(err, s.files.Discard(cleanup, file))
			}
		}
	}()
	if err := ensureArticles(ctx, tx, ids); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(ids))
	// Read each title/body atomically, retaining only one body at a time. Never
	// hold a database transaction while entering the storage mutex: Discard
	// acquires those resources in the opposite order. A concurrent deletion
	// after the initial selection check fails and compensates this export.
	for _, id := range ids {
		var title, content string
		if err := s.db.QueryRowContext(ctx, "SELECT article_title,article_content FROM tb_article WHERE id=?", id).Scan(&title, &content); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, failure("文章不存在")
			}
			return nil, err
		}
		// The storage adapter derives the hash from these exact bytes and adds
		// the article ID. Filename metadata never supplies a trusted digest.
		name := fmt.Sprintf("article-%d-%s.md", id, exportTitle(title))
		file, err := s.files.Put(ctx, "exports", name, "text/markdown", strings.NewReader(content))
		if err != nil {
			return nil, err
		}
		created = append(created, file)
		urls = append(urls, file.URL)
	}
	return urls, nil
}

// 用途：承接原 POST /admin/articles/import 的实际业务结果；由对应 Logic TASK 实现已确认的状态与失败语义。
func (s *Service) ImportArticles(ctx context.Context, in platform.Input) (any, error) {
	if len(in.Files) == 0 {
		return nil, invalid("请选择导入文件")
	}
	if !in.Actor.LoggedIn || in.Actor.UserInfoID <= 0 {
		return nil, failure("请先登录")
	}
	typ := in.Form.Get("type")
	if typ == "" {
		typ = in.Query.Get("type")
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ != "" && typ != "hexo" {
		return nil, invalid("不支持的文章导入类型")
	}
	const maxBytes int64 = 100 << 20
	remaining := maxBytes
	documents := make([]articleInput, 0, len(in.Files))
	for _, header := range in.Files {
		if header == nil {
			return nil, invalid("导入文件不正确")
		}
		name := header.Filename
		ext := strings.ToLower(path.Ext(name))
		if name == "" || strings.ContainsAny(name, "/\\\x00\r\n") || (ext != ".md" && ext != ".markdown" && ext != ".txt") {
			return nil, invalid("仅支持 UTF-8 Markdown 或文本文件")
		}
		if header.Size > remaining {
			return nil, &http.MaxBytesError{Limit: maxBytes}
		}
		file, err := header.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(importReader{ctx, file}, remaining+1))
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		if int64(len(data)) > remaining {
			return nil, &http.MaxBytesError{Limit: maxBytes}
		}
		remaining -= int64(len(data))
		if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return nil, invalid("导入正文必须是 UTF-8 文本")
		}
		status, articleType := 3, 1
		document := articleInput{ArticleTitle: strings.TrimSuffix(name, path.Ext(name)), ArticleContent: string(data), Status: &status, Type: &articleType}
		if typ == "hexo" {
			if err := s.parseHexo(&document); err != nil {
				return nil, err
			}
		}
		if err := validateArticle(document); err != nil {
			return nil, err
		}
		if err := s.defaultCover(ctx, &document); err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	// Complete parsing and validation before acquiring the single write
	// transaction; any later catalog/article failure rolls the whole batch back.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, document := range documents {
		if err := s.saveTx(ctx, tx, in.Actor.UserInfoID, document); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

type importReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r importReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func exportTitle(title string) string {
	title = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("/\\:", r) {
			return '_'
		}
		return r
	}, title)
	title = strings.TrimSpace(title)
	if title == "" {
		return "article"
	}
	return title
}

func (s *Service) parseHexo(document *articleInput) error {
	text := strings.TrimPrefix(document.ArticleContent, "\ufeff")
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return invalid("Hexo 文件缺少 front matter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return invalid("Hexo front matter 未闭合")
	}
	var fields map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "")), &fields); err != nil {
		return invalid("Hexo YAML 不正确")
	}
	document.ArticleContent = strings.Join(lines[end+1:], "")
	for _, key := range []string{"title", "date", "categories", "tags"} {
		node, exists := fields[key]
		if !exists {
			continue
		}
		values, err := yamlText(node)
		if err != nil {
			return invalid("Hexo " + key + " 格式不正确")
		}
		switch key {
		case "title":
			if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
				return invalid("Hexo 标题不正确")
			}
			document.ArticleTitle = values[0]
		case "date":
			if len(values) != 1 {
				return invalid("Hexo 日期不正确")
			}
			var date time.Time
			for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
				date, err = time.ParseInLocation(layout, values[0], s.cfg.TimeZone)
				if err == nil {
					break
				}
			}
			if err != nil {
				return invalid("Hexo 日期不正确")
			}
			document.created = &date
		case "categories":
			if len(values) > 0 {
				category := values[len(values)-1]
				document.CategoryName = &category
			}
		case "tags":
			document.TagNameList = values
		}
	}
	_, hasTitle := fields["title"]
	if hasTitle && strings.TrimSpace(value(document.CategoryName)) != "" && len(document.TagNameList) > 0 {
		status := 1
		document.Status = &status
	}
	return nil
}
func yamlText(node yaml.Node) ([]string, error) {
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = *node.Alias
	}
	if node.Kind == yaml.ScalarNode && (node.Tag == "!!str" || node.Tag == "!!timestamp") {
		return []string{node.Value}, nil
	}
	if node.Kind == yaml.SequenceNode {
		values := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			if child.Kind != yaml.ScalarNode || (child.Tag != "!!str" && child.Tag != "!!timestamp") {
				return nil, errors.New("expected text")
			}
			values = append(values, child.Value)
		}
		return values, nil
	}
	return nil, errors.New("expected text or text list")
}
