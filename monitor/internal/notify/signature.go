package notify

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
)

// Sign the exact transmitted bytes, never a reserialized JSON object.
func signatures(secret string, body []byte) (string, string) {
	v1 := hmac.New(sha1.New, []byte(secret))
	v1.Write(body)
	v2 := hmac.New(sha256.New, []byte(secret))
	v2.Write(body)
	return hex.EncodeToString(v1.Sum(nil)), hex.EncodeToString(v2.Sum(nil))
}
