package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
)

func verifySignature(secret string, body []byte, header http.Header) bool {
	if secret == "" {
		return false
	}
	value := header.Get("Agora-Signature-V2")
	mac := hmac.New(sha256.New, []byte(secret))
	if len(header.Values("Agora-Signature-V2")) == 0 {
		value = header.Get("Agora-Signature")
		mac = hmac.New(sha1.New, []byte(secret))
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || value == "" {
		return false
	}
	mac.Write(body)
	return hmac.Equal(decoded, mac.Sum(nil))
}
