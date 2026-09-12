package social

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"blog-go/internal/platform"
)

// 用途：完成微博代码交换、应用归属/有效期和用户资料核验。
func (s *Service) VerifyWeibo(ctx context.Context, code string) (VerifiedIdentity, error) {
	const message = "微博登录失败"
	appKey, secret, redirect := s.cfg.Values["BLOG_WEIBO_APP_KEY"], s.cfg.Values["BLOG_WEIBO_APP_SECRET"], s.cfg.Values["BLOG_WEIBO_REDIRECT_URI"]
	if strings.TrimSpace(appKey) == "" || strings.TrimSpace(secret) == "" || strings.TrimSpace(redirect) == "" {
		return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, "微博登录未配置应用信息", nil)
	}
	if strings.TrimSpace(code) == "" {
		return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, message, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exchange, err := s.requestJSON(ctx, http.MethodPost, "https://api.weibo.com/oauth2/access_token", url.Values{
		"client_id": {appKey}, "client_secret": {secret}, "grant_type": {"authorization_code"},
		"code": {code}, "redirect_uri": {redirect},
	}, false)
	if err != nil {
		return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, message, err)
	}
	token, uid := textField(exchange, "access_token"), decimalField(exchange["uid"])
	if token == "" || uid == "" {
		return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, message, nil)
	}
	info, err := s.requestJSON(ctx, http.MethodPost, "https://api.weibo.com/oauth2/get_token_info", url.Values{
		"access_token": {token},
	}, false)
	if err != nil {
		return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, message, err)
	}
	if decimalField(info["appkey"]) != appKey || decimalField(info["uid"]) != uid || decimalField(info["expire_in"]) == "" {
		return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, message, nil)
	}
	profile, err := s.requestJSON(ctx, http.MethodGet, "https://api.weibo.com/2/users/show.json", url.Values{
		"access_token": {token}, "uid": {uid},
	}, false)
	if err != nil {
		return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, message, err)
	}
	foundID := false
	for _, field := range []string{"id", "idstr"} {
		if value, exists := profile[field]; exists {
			if decimalField(value) != uid {
				return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, message, nil)
			}
			foundID = true
		}
	}
	nickname, avatar := textField(profile, "screen_name"), avatarField(profile, "avatar_hd", "avatar_large", "profile_image_url")
	if !foundID || nickname == "" || avatar == "" {
		return VerifiedIdentity{}, loginFailure(platform.CodeWeibo, message, nil)
	}
	return VerifiedIdentity{LoginType: 3, ExternalID: uid, Nickname: nickname, Avatar: avatar}, nil
}

// Provider identifiers and lifetimes are positive decimal integers, encoded
// either as JSON numbers or digit strings. Never round them through float64.
func decimalField(raw json.RawMessage) string {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, "\"") {
		if json.Unmarshal(raw, &value) != nil {
			return ""
		}
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return ""
		}
	}
	return strings.TrimLeft(value, "0")
}

// P-118 HTTP POST https://api.weibo.com/oauth2/access_token
// 约束变化：client_id/client_secret/grant_type/code/redirect_uri form；要求access_token/uid非空，拒绝error字段及HTTP异常。 具体字段与状态解释见MODULE-011已确认执行边界。

// P-119 HTTP POST https://api.weibo.com/oauth2/get_token_info
// 请求变化：新增同一微博提供方的 POST 表单 access_token 校验调用。
// 响应变化：读取 appkey、uid、expire_in，数值或数字字符串无精度损失地核对应用/身份/有效期。
// 约束变化：新增同提供方必要核验，form access_token；校验appkey/uid/expire_in。只信验证后的应用和身份。 具体字段与状态解释见MODULE-011已确认执行边界。

// P-120 HTTP GET https://api.weibo.com/2/users/show.json
// 约束变化：access_token/uid；id/idstr必须匹配，screen_name/avatar_hd及明确头像回退；业务错误不能成功建用户。 具体字段与状态解释见MODULE-011已确认执行边界。
