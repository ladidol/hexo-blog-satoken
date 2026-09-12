package identity

import (
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// 用途：为新本地账号产生有盐口令验证串。
func HashPassword(password string) (string, error) {
	if !utf8.ValidString(password) || len(password) < 8 || len(password) > 72 {
		return "", invalid("密码长度必须为 8 至 72 字节")
	}
	encoded, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	return string(encoded), err
}

// 用途：仅验证本次新账号的 bcrypt 口令，不将外部令牌当密码。
func VerifyPassword(encoded, password string) error {
	if !utf8.ValidString(password) || len(password) > 72 {
		return invalid("密码不正确")
	}
	return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password))
}
