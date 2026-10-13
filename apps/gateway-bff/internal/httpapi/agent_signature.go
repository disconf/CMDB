package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func verifyAgentSignature(r *http.Request, body []byte) bool {
	key := strings.TrimSpace(os.Getenv("AGENT_SIGNING_KEY"))
	if key == "" {
		return true
	}
	timestamp := strings.TrimSpace(r.Header.Get("X-Agent-Timestamp"))
	signature := strings.TrimSpace(r.Header.Get("X-Agent-Signature"))
	if timestamp == "" || signature == "" {
		return false
	}
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > time.Minute {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("\n"))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(expected))
}
