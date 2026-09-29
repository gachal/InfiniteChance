// Package tc3 implements the Tencent Cloud API 3.0 request signature
// (TC3-HMAC-SHA256) and one POST helper. It is the shared seam for every
// Tencent-native integration — the relay's VOD AIGC adaptor (20 号票) and
// the channel connectivity probe both sign through here, so the signing
// rules live exactly once.
package tc3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxResponseBytes caps one signed call's response body; Tencent API bodies
// stay far below it and a runaway endpoint should fail, not exhaust memory.
const maxResponseBytes = 32 << 20

// Sign builds the TC3-HMAC-SHA256 Authorization header set for one API call.
// host is the bare endpoint host (e.g. vod.tencentcloudapi.com), body the
// already-marshaled JSON payload — the signature covers exactly these bytes,
// so callers must not re-marshal between signing and sending. region may be
// empty (some products are regionless); ts is the request timestamp, which
// must also ride the X-TC-Timestamp header (Call wires that itself).
func Sign(secretID, secretKey, region, service, host, action, version string, ts time.Time, body []byte) map[string]string {
	timestamp := strconv.FormatInt(ts.Unix(), 10)
	date := ts.UTC().Format("2006-01-02")

	headers := map[string]string{
		"content-type":   "application/json; charset=utf-8",
		"host":           host,
		"x-tc-action":    action,
		"x-tc-timestamp": timestamp,
		"x-tc-version":   version,
	}
	if region != "" {
		headers["x-tc-region"] = region
	}

	signed := make([]string, 0, len(headers))
	for k := range headers {
		signed = append(signed, k)
	}
	sort.Strings(signed)

	var canonical strings.Builder
	for _, k := range signed {
		canonical.WriteString(k)
		canonical.WriteString(":")
		v := headers[k]
		// TC3 规范:canonical headers 里 x-tc-action 的值必须小写,
		// 实际发送的 HTTP 头保持原样。
		if k == "x-tc-action" {
			v = strings.ToLower(v)
		}
		canonical.WriteString(v)
		canonical.WriteString("\n")
	}
	bodyHash := sha256.Sum256(body)
	canonicalRequest := strings.Join([]string{
		"POST", "/", "", canonical.String(),
		strings.Join(signed, ";"),
		hex.EncodeToString(bodyHash[:]),
	}, "\n")

	scope := date + "/" + service + "/tc3_request"
	stringToSign := strings.Join([]string{
		"TC3-HMAC-SHA256", timestamp, scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := hmacSHA256([]byte("TC3"+secretKey), []byte(date))
	signingKey = hmacSHA256(signingKey, []byte(service))
	signingKey = hmacSHA256(signingKey, []byte("tc3_request"))
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	headers["Authorization"] = fmt.Sprintf(
		"TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		secretID, scope, strings.Join(signed, ";"), signature)
	return headers
}

// Call signs and POSTs one Tencent Cloud API request. endpoint accepts a
// bare host or a full URL; the scheme defaults to https — an explicit
// http:// endpoint is honored so tests can point the seam at a local stub
// (real Tencent endpoints are always https). The returned body is the raw
// JSON envelope {"Response":{…}} — business errors live inside it under
// Response.Error and are the caller's to interpret.
func Call(ctx context.Context, client *http.Client, endpoint, action, version, service, secretID, secretKey, region string, payload []byte) (int, []byte, error) {
	scheme := "https://"
	trimmed := strings.TrimSpace(endpoint)
	if strings.HasPrefix(trimmed, "http://") {
		scheme = "http://"
	}
	host := strings.TrimPrefix(strings.TrimPrefix(trimmed, "https://"), "http://")
	host = strings.TrimRight(host, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, scheme+host+"/", bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	headers := Sign(secretID, secretKey, region, service, host, action, version, time.Now(), payload)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func hmacSHA256(key []byte, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
