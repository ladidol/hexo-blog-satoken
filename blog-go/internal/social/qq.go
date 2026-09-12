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

// 用途：核对 QQ 应用/OpenID 与有效资料。
func (s *Service) VerifyQQ(ctx context.Context, openID, accessToken string) (VerifiedIdentity, error) {
	const message = "QQ登录失败"
	appID := s.cfg.Values["BLOG_QQ_APP_ID"]
	if strings.TrimSpace(appID) == "" {
		return VerifiedIdentity{}, loginFailure(platform.CodeQQ, "QQ登录未配置应用ID", nil)
	}
	if strings.TrimSpace(openID) == "" || strings.TrimSpace(accessToken) == "" {
		return VerifiedIdentity{}, loginFailure(platform.CodeQQ, message, nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	identity, err := s.requestJSON(ctx, http.MethodGet, "https://graph.qq.com/oauth2.0/me", url.Values{
		"access_token": {accessToken}, "fmt": {"json"},
	}, true)
	if err != nil {
		return VerifiedIdentity{}, loginFailure(platform.CodeQQ, message, err)
	}
	if textField(identity, "client_id") != appID || textField(identity, "openid") != openID {
		return VerifiedIdentity{}, loginFailure(platform.CodeQQ, message, nil)
	}
	profile, err := s.requestJSON(ctx, http.MethodGet, "https://graph.qq.com/user/get_user_info", url.Values{
		"access_token": {accessToken}, "oauth_consumer_key": {appID}, "openid": {openID},
	}, false)
	if err != nil {
		return VerifiedIdentity{}, loginFailure(platform.CodeQQ, message, err)
	}
	var ret *int
	if json.Unmarshal(profile["ret"], &ret) != nil || ret == nil || *ret != 0 {
		return VerifiedIdentity{}, loginFailure(platform.CodeQQ, message, nil)
	}
	nickname := textField(profile, "nickname")
	avatar := avatarField(profile, "figureurl_qq_2", "figureurl_qq_1")
	if nickname == "" || avatar == "" {
		return VerifiedIdentity{}, loginFailure(platform.CodeQQ, message, nil)
	}
	return VerifiedIdentity{LoginType: 2, ExternalID: openID, Nickname: nickname, Avatar: avatar}, nil
}

// P-116 HTTP GET https://graph.qq.com/oauth2.0/me
// 约束变化：access_token、fmt=json；校验client_id/openid，处理JSON/历史callback和error；凭据不落日志。 具体字段与状态解释见MODULE-011已确认执行边界。

// P-117 HTTP GET https://graph.qq.com/user/get_user_info
// 约束变化：access_token/oauth_consumer_key/openid；ret必须0，读取nickname与figureurl_qq_2/1；非0、空字段和畸形失败。 具体字段与状态解释见MODULE-011已确认执行边界。
