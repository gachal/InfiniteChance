package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileSystem keeps objects as files under Root: the key becomes the
// relative path, so the canvases/{id}/{task}/… archival layout is visible
// on the volume. Writes land via a temp file and rename, so a reader never
// sees a half-written object even across process deaths.
type FileSystem struct {
	Root string
}

// NewFileSystem returns a store rooted at dir, creating it if missing.
func NewFileSystem(dir string) (*FileSystem, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("objectstore: create root: %w", err)
	}
	return &FileSystem{Root: dir}, nil
}

// validKey refuses keys that would escape the root or break the archival
// layout: absolute paths, .. segments, empty or backslash separators.
func validKey(key string) bool {
	if key == "" || strings.ContainsRune(key, '\\') {
		return false
	}
	if filepath.IsAbs(key) {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func (s *FileSystem) path(key string) (string, error) {
	if !validKey(key) {
		return "", fmt.Errorf("objectstore: invalid key %q", key)
	}
	return filepath.Join(s.Root, filepath.FromSlash(key)), nil
}

func (s *FileSystem) Put(ctx context.Context, key string, r io.Reader, size int64, _ string) error {
	dst, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("objectstore: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".put-*")
	if err != nil {
		return fmt.Errorf("objectstore: temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	// size+1 字节上限:写入超过声明的 size 视为调用方契约破裂,当场报错
	// 而不是把截断或超量的对象留下来。
	n, err := io.Copy(tmp, io.LimitReader(r, size+1))
	if err != nil {
		tmp.Close()
		return fmt.Errorf("objectstore: write: %w", err)
	}
	if n != size {
		tmp.Close()
		return fmt.Errorf("objectstore: wrote %d bytes, declared %d", n, size)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("objectstore: close: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("objectstore: rename: %w", err)
	}
	return nil
}

func (s *FileSystem) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("objectstore: object %q: %w", key, fs.ErrNotExist)
	}
	if err != nil {
		return nil, fmt.Errorf("objectstore: open: %w", err)
	}
	return f, nil
}

func (s *FileSystem) Delete(ctx context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("objectstore: delete: %w", err)
	}
	// 逐级清掉空父目录,卷上不留归档空壳;非空目录的 Remove 失败即停,
	// 属正常情况。到 Root 为止。
	dir := filepath.Dir(p)
	for dir != s.Root {
		if err := os.Remove(dir); err != nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	return nil
}
