package community

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// 用途：清理评论、留言与聊天中的危险 HTML，保留受支持的表情和排版。
func SanitizeHTML(content string) (string, error) {
	if !utf8.ValidString(content) || len(content) > 2<<20 || strings.ContainsRune(content, 0) {
		return "", invalid("内容格式不正确")
	}
	nodes, err := html.ParseFragment(strings.NewReader(content), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", invalid("内容格式不正确")
	}
	clean := &html.Node{Type: html.DocumentNode}
	meaningful, visited := false, 0
	var copySafe func(*html.Node, *html.Node, int) error
	copySafe = func(source, parent *html.Node, depth int) error {
		visited++
		if depth > 256 || visited > 100000 {
			return invalid("内容结构过于复杂")
		}
		switch source.Type {
		case html.TextNode:
			parent.AppendChild(&html.Node{Type: html.TextNode, Data: source.Data})
			if strings.TrimSpace(source.Data) != "" {
				meaningful = true
			}
			return nil
		case html.ElementNode:
			if source.Namespace != "" {
				return nil
			}
			switch source.Data {
			case "script", "style", "iframe", "object", "embed", "svg", "math", "template", "noscript", "textarea", "title", "xmp", "head", "link", "meta", "base":
				return nil
			case "p", "br", "em", "strong", "b", "i", "u", "s", "code", "pre", "blockquote", "ul", "ol", "li", "span", "a", "img":
				node := &html.Node{Type: html.ElementNode, Data: source.Data, DataAtom: source.DataAtom}
				attributes := make(map[string]string)
				for _, attr := range source.Attr {
					if attr.Namespace == "" {
						attributes[attr.Key] = attr.Val
					}
				}
				if source.Data == "a" && safeURL(attributes["href"], false) {
					node.Attr = []html.Attribute{{Key: "href", Val: attributes["href"]}, {Key: "rel", Val: "noopener noreferrer nofollow"}}
				}
				if source.Data == "img" {
					if !safeURL(attributes["src"], true) {
						return nil
					}
					node.Attr = []html.Attribute{{Key: "src", Val: attributes["src"]}}
					if alt, exists := attributes["alt"]; exists {
						node.Attr = append(node.Attr, html.Attribute{Key: "alt", Val: alt})
					}
					for _, key := range []string{"width", "height"} {
						if n, err := strconv.Atoi(attributes[key]); err == nil && n > 0 && n <= 4096 {
							node.Attr = append(node.Attr, html.Attribute{Key: key, Val: strconv.Itoa(n)})
						}
					}
					meaningful = true
				}
				parent.AppendChild(node)
				parent = node
			}
		default:
			return nil
		}
		for child := source.FirstChild; child != nil; child = child.NextSibling {
			if err := copySafe(child, parent, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, node := range nodes {
		if err := copySafe(node, clean, 0); err != nil {
			return "", err
		}
	}
	if !meaningful {
		return "", invalid("内容不能为空")
	}
	var result strings.Builder
	if err := html.Render(&result, clean); err != nil {
		return "", err
	}
	return strings.TrimSpace(result.String()), nil
}

func safeURL(value string, relative bool) bool {
	if value == "" || strings.Contains(value, "\\") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.Opaque != "" {
		return false
	}
	if relative && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") && u.Host == "" {
		return true
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != ""
}
