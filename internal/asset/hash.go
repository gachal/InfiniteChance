package asset

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// hashReader streams r through SHA-256 and answers the hex digest — 48 号票
// 的「哈希认证」口径:内容指纹只用于判同去重,不是下载完整性校验。
func hashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashBytes answers the hex SHA-256 of p (转存链字节全量在手时的同款指纹)。
func hashBytes(p []byte) string {
	s := sha256.Sum256(p)
	return hex.EncodeToString(s[:])
}
