package files

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type LocalStore struct {
	Root      string
	PublicURL string
}

func (s *LocalStore) Put(ctx context.Context, scope, name, contentType string, body io.Reader) (StoredFile, error) {
	input, err := prepareFile(ctx, scope, name, contentType, body)
	if err != nil {
		return StoredFile{}, err
	}
	directory, err := s.openScope(scope, true)
	if err != nil {
		return StoredFile{}, fmt.Errorf("open upload directory: %w", err)
	}
	defer directory.Close()
	fd := int(directory.Fd())
	leaf := path.Base(input.key)
	result := StoredFile{Key: input.key, URL: localFileURL(s.PublicURL, input.key, name)}
	if existing, err := openRegular(fd, leaf); err == nil {
		defer existing.Close()
		if err := verifyExisting(ctx, existing, input); err != nil {
			return StoredFile{}, err
		}
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return StoredFile{}, fmt.Errorf("inspect existing file: %w", err)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return StoredFile{}, err
	}
	temporary := fmt.Sprintf(".upload-%x", random)
	tempFD, err := unix.Openat(fd, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return StoredFile{}, fmt.Errorf("create upload temporary file: %w", err)
	}
	temp := os.NewFile(uintptr(tempFD), temporary)
	defer temp.Close()
	defer unix.Unlinkat(fd, temporary, 0)
	if _, err := temp.Write(input.data); err != nil {
		return StoredFile{}, fmt.Errorf("write upload: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return StoredFile{}, fmt.Errorf("sync upload: %w", err)
	}
	if err := temp.Close(); err != nil {
		return StoredFile{}, fmt.Errorf("close upload: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return StoredFile{}, err
	}
	// linkat commits without replacing a concurrent winner; readers see only complete files.
	if err := unix.Linkat(fd, temporary, fd, leaf, 0); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return StoredFile{}, fmt.Errorf("commit upload: %w", err)
		}
		existing, err := openRegular(fd, leaf)
		if err != nil {
			return StoredFile{}, fmt.Errorf("inspect concurrent upload: %w", err)
		}
		defer existing.Close()
		if err := verifyExisting(ctx, existing, input); err != nil {
			return StoredFile{}, err
		}
		return result, nil
	}
	if err := unix.Unlinkat(fd, temporary, 0); err != nil {
		return StoredFile{}, fmt.Errorf("upload committed as %s but temporary cleanup failed: %w", input.key, err)
	}
	if err := directory.Sync(); err != nil {
		return StoredFile{}, fmt.Errorf("upload committed as %s but directory sync failed: %w", input.key, err)
	}
	result.Created = true
	return result, nil
}
func (s *LocalStore) Remove(ctx context.Context, file StoredFile) error {
	if !file.Created {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validStoredKey(file.Key) {
		return invalidFile()
	}
	scope, leaf, _ := strings.Cut(file.Key, "/")
	directory, err := s.openScope(scope, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer directory.Close()
	existing, err := openRegular(int(directory.Fd()), leaf)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	existing.Close()
	if err := unix.Unlinkat(int(directory.Fd()), leaf, 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return directory.Sync()
}
func (s *LocalStore) openScope(scope string, create bool) (*os.File, error) {
	if scopeKind(scope) == "" && scope != "defaults" {
		return nil, os.ErrPermission
	}
	if s.Root == "" {
		return nil, os.ErrInvalid
	}
	if create {
		// The configured parent is trusted; the public root itself and its children are never followed.
		if err := os.MkdirAll(filepath.Dir(s.Root), 0700); err != nil {
			return nil, err
		}
		if err := os.Mkdir(s.Root, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	rootFD, err := unix.Open(s.Root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)
	if create {
		if err := unix.Mkdirat(rootFD, scope, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	scopeFD, err := unix.Openat(rootFD, scope, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(scopeFD), scope), nil
}
func openRegular(directory int, leaf string) (*os.File, error) {
	fd, err := unix.Openat(directory, leaf, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), leaf)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, os.ErrPermission
	}
	return file, nil
}

func verifyExisting(ctx context.Context, file *os.File, input preparedFile) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() != int64(len(input.data)) {
		return errors.New("existing content-addressed file is corrupt")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(contextReader{ctx, file}, maxFileBytes+1)); err != nil {
		return fmt.Errorf("verify existing file: %w", err)
	}
	expected := sha256.Sum256(input.data)
	if !bytes.Equal(hash.Sum(nil), expected[:]) {
		return errors.New("existing content-addressed file is corrupt")
	}
	return nil
}
