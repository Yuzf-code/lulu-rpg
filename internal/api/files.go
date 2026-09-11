package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// saveUpload 把图片数据写入 <DataDir>/files/avatars/ 并返回 URL 路径。
// 生成的文件名带随机前缀，避免并发冲突。
func saveUpload(dataDir, mime string, data []byte) (string, error) {
	ext := ".png"
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	name := "up_" + hex.EncodeToString(b) + ext
	dir := filepath.Join(dataDir, "files", "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	return "/files/avatars/" + name, nil
}

// avatarURLLabel 返回给日志/前端调试用的短标识。
func avatarURLLabel(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return fmt.Sprintf("…%s", p[i:])
	}
	return p
}
