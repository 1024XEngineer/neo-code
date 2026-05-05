package feishuadapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestVerifyFeishuSignatureAcceptsHexCaseInsensitive(t *testing.T) {
	secret := "secret"
	body := []byte(`{"hello":"world"}`)
	ts := time.Now().UTC().Unix()
	nonce := "nonce"
	payload := formatPayload(ts, nonce, body)
	sum := signPayload(secret, payload)
	upperHex := "SHA256=" + stringsToUpper(hex.EncodeToString(sum))

	header := make(http.Header)
	header.Set(headerLarkTimestamp, formatUnix(ts))
	header.Set(headerLarkNonce, nonce)
	header.Set(headerLarkSignature, upperHex)

	if !verifyFeishuSignature(secret, 5*time.Minute, header, body, time.Unix(ts, 0).UTC(), false) {
		t.Fatal("expected uppercase hex signature to be accepted")
	}
}

func TestVerifyFeishuSignatureRejectsBase64CaseModified(t *testing.T) {
	secret := "secret"
	body := []byte(`{"hello":"world"}`)
	ts := time.Now().UTC().Unix()
	nonce := "nonce"
	payload := formatPayload(ts, nonce, body)
	sum := signPayload(secret, payload)
	base64Sig := base64.StdEncoding.EncodeToString(sum)
	tampered := stringsToUpper(base64Sig)

	header := make(http.Header)
	header.Set(headerLarkTimestamp, formatUnix(ts))
	header.Set(headerLarkNonce, nonce)
	header.Set(headerLarkSignature, tampered)

	if verifyFeishuSignature(secret, 5*time.Minute, header, body, time.Unix(ts, 0).UTC(), false) {
		t.Fatal("expected case-modified base64 signature to be rejected")
	}
}

func TestVerifyFeishuSignatureAcceptsExactBase64(t *testing.T) {
	secret := "secret"
	body := []byte(`{"hello":"world"}`)
	ts := time.Now().UTC().Unix()
	nonce := "nonce"
	payload := formatPayload(ts, nonce, body)
	sum := signPayload(secret, payload)
	base64Sig := base64.StdEncoding.EncodeToString(sum)

	header := make(http.Header)
	header.Set(headerLarkTimestamp, formatUnix(ts))
	header.Set(headerLarkNonce, nonce)
	header.Set(headerLarkSignature, base64Sig)

	if !verifyFeishuSignature(secret, 5*time.Minute, header, body, time.Unix(ts, 0).UTC(), false) {
		t.Fatal("expected exact base64 signature to be accepted")
	}
}

func signPayload(secret string, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func formatPayload(ts int64, nonce string, body []byte) string {
	return formatUnix(ts) + nonce + string(body)
}

func formatUnix(ts int64) string {
	return strconv.FormatInt(ts, 10)
}

func stringsToUpper(value string) string {
	out := make([]byte, len(value))
	for index := range value {
		current := value[index]
		if current >= 'a' && current <= 'z' {
			current -= 32
		}
		out[index] = current
	}
	return string(out)
}
