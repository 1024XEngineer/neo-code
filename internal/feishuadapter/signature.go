package feishuadapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	headerLarkTimestamp = "X-Lark-Request-Timestamp"
	headerLarkNonce     = "X-Lark-Request-Nonce"
	headerLarkSignature = "X-Lark-Signature"
)

// verifyFeishuSignature 校验飞书回调签名与时间窗口，防止伪造请求与重放。
func verifyFeishuSignature(
	secret string,
	maxSkew time.Duration,
	header http.Header,
	body []byte,
	now time.Time,
	insecureSkip bool,
) bool {
	if insecureSkip {
		return true
	}
	if strings.TrimSpace(secret) == "" {
		return false
	}
	timestamp := strings.TrimSpace(header.Get(headerLarkTimestamp))
	nonce := strings.TrimSpace(header.Get(headerLarkNonce))
	signature := strings.TrimSpace(header.Get(headerLarkSignature))
	if timestamp == "" || signature == "" {
		return false
	}
	if maxSkew > 0 {
		if !withinSkew(timestamp, maxSkew, now) {
			return false
		}
	}
	payload := timestamp + nonce + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	sum := mac.Sum(nil)
	base64Sign := base64.StdEncoding.EncodeToString(sum)
	hexSign := hex.EncodeToString(sum)
	normalizedBase64Sig := normalizeBase64Signature(signature)
	normalizedHexSig := normalizeHexSignature(signature)
	return hmac.Equal([]byte(base64Sign), []byte(normalizedBase64Sig)) ||
		hmac.Equal([]byte(normalizeHexSignature(hexSign)), []byte(normalizedHexSig))
}

// withinSkew 判断请求时间戳是否在允许偏差窗口内。
func withinSkew(timestamp string, maxSkew time.Duration, now time.Time) bool {
	parsed, err := parseUnixSeconds(timestamp)
	if err != nil {
		return false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	diff := now.Sub(parsed)
	if diff < 0 {
		diff = -diff
	}
	return diff <= maxSkew
}

// parseUnixSeconds 解析 Unix 秒级时间戳。
func parseUnixSeconds(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0).UTC(), nil
}

// normalizeHexSignature 统一 hex 签名格式，兼容常见前缀并做大小写归一。
func normalizeHexSignature(signature string) string {
	trimmed := strings.TrimSpace(strings.ToLower(signature))
	trimmed = strings.TrimPrefix(trimmed, "v0=")
	trimmed = strings.TrimPrefix(trimmed, "sha256=")
	return trimmed
}

// normalizeBase64Signature 统一 base64 签名格式，仅裁剪空白与前缀，不改变大小写。
func normalizeBase64Signature(signature string) string {
	trimmed := strings.TrimSpace(signature)
	trimmed = strings.TrimPrefix(trimmed, "v0=")
	trimmed = strings.TrimPrefix(trimmed, "sha256=")
	return trimmed
}
