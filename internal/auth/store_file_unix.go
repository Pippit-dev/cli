//go:build !windows

package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type fileCredentialStore struct {
	path string
}

func NewFileCredentialStore(path string) CredentialStore {
	return &fileCredentialStore{path: filepath.Clean(path)}
}

func newPlatformFallbackStore() CredentialStore {
	path := defaultCredentialPath()
	if path == "" {
		return nil
	}
	return NewFileCredentialStore(path)
}

func (s *fileCredentialStore) Load(ctx context.Context) (*Credential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dirFD, name, err := s.openPrivateDirectory(false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	defer unix.Close(dirFD)

	fd, err := unix.Openat(dirFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("安全打开本机登录凭证失败: %w", ErrSecureStore)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	if err := verifyPrivateRegularFile(fd); err != nil {
		return nil, err
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxCredentialBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取本机登录凭证失败: %w", ErrSecureStore)
	}
	return decodeCredential(payload)
}

func (s *fileCredentialStore) Save(ctx context.Context, credential *Credential) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := encodeCredential(credential)
	if err != nil {
		return err
	}
	dirFD, name, err := s.openPrivateDirectory(true)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)

	tempName, err := randomTempName()
	if err != nil {
		return errors.New("创建本机登录凭证临时文件失败")
	}
	fd, err := unix.Openat(dirFD, tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("创建本机登录凭证临时文件失败: %w", ErrSecureStore)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = unix.Unlinkat(dirFD, tempName, 0)
		}
	}()
	file := os.NewFile(uintptr(fd), tempName)
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入本机登录凭证失败: %w", ErrSecureStore)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("同步本机登录凭证失败: %w", ErrSecureStore)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭本机登录凭证失败: %w", ErrSecureStore)
	}
	if err := unix.Renameat(dirFD, tempName, dirFD, name); err != nil {
		return fmt.Errorf("原子保存本机登录凭证失败: %w", ErrSecureStore)
	}
	cleanup = false
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("同步本机凭证目录失败: %w", ErrSecureStore)
	}
	return nil
}

func (s *fileCredentialStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dirFD, name, err := s.openPrivateDirectory(false)
	if errors.Is(err, os.ErrNotExist) {
		return ErrCredentialNotFound
	}
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	if err := unix.Unlinkat(dirFD, name, 0); errors.Is(err, unix.ENOENT) {
		return ErrCredentialNotFound
	} else if err != nil {
		return fmt.Errorf("删除本机登录凭证失败: %w", ErrSecureStore)
	}
	if err := unix.Fsync(dirFD); err != nil {
		return fmt.Errorf("同步本机凭证目录失败: %w", ErrSecureStore)
	}
	return nil
}

func (s *fileCredentialStore) openPrivateDirectory(create bool) (int, string, error) {
	if s == nil || strings.TrimSpace(s.path) == "" || filepath.Base(s.path) == "." {
		return -1, "", ErrSecureStore
	}
	dir := filepath.Dir(s.path)
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return -1, "", fmt.Errorf("创建本机凭证目录失败: %w", ErrSecureStore)
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return -1, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return -1, "", fmt.Errorf("本机凭证目录必须是权限 0700 的真实目录: %w", ErrSecureStore)
	}
	dirFD, err := unix.Open(dir, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, "", fmt.Errorf("安全打开本机凭证目录失败: %w", ErrSecureStore)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(dirFD, &stat); err != nil || stat.Uid != uint32(os.Geteuid()) {
		unix.Close(dirFD)
		return -1, "", fmt.Errorf("本机凭证目录所有者无效: %w", ErrSecureStore)
	}
	return dirFD, filepath.Base(s.path), nil
}

func verifyPrivateRegularFile(fd int) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("检查本机登录凭证失败: %w", ErrSecureStore)
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o077 != 0 {
		return fmt.Errorf("本机登录凭证必须是当前用户拥有的权限 0600 文件: %w", ErrSecureStore)
	}
	return nil
}
