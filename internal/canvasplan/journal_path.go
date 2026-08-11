package canvasplan

import (
	"fmt"
	"os"
	"path/filepath"
)

type secureJournalDirectory struct {
	path string
	info os.FileInfo
}

func ensureSecureJournalDirectory(path string) (*secureJournalDirectory, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve CanvasPlan journal directory: %w", err)
	}
	absolute = filepath.Clean(absolute)
	components := parentComponents(absolute)
	for index, component := range components {
		info, lstatErr := os.Lstat(component)
		if os.IsNotExist(lstatErr) {
			if err := os.Mkdir(component, 0o700); err != nil && !os.IsExist(err) {
				return nil, fmt.Errorf("create CanvasPlan journal directory %q: %w", component, err)
			}
			info, lstatErr = os.Lstat(component)
		}
		if lstatErr != nil {
			return nil, fmt.Errorf("inspect CanvasPlan journal directory %q: %w", component, lstatErr)
		}
		if fileInfoIsLinkLike(info) {
			isFinal := index == len(components)-1
			if isFinal || !trustedSystemAncestorSymlink(component, info) {
				return nil, fmt.Errorf("CanvasPlan journal directory must not contain symbolic links: %s", component)
			}
			continue
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("CanvasPlan journal parent is not a directory: %s", component)
		}
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, fmt.Errorf("inspect CanvasPlan journal directory: %w", err)
	}
	if fileInfoIsLinkLike(info) || !info.IsDir() {
		return nil, fmt.Errorf("CanvasPlan journal parent must be a real directory: %s", absolute)
	}
	return &secureJournalDirectory{path: absolute, info: info}, nil
}

func parentComponents(path string) []string {
	components := make([]string, 0, 8)
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		components = append(components, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for left, right := 0, len(components)-1; left < right; left, right = left+1, right-1 {
		components[left], components[right] = components[right], components[left]
	}
	return components
}

func (directory *secureJournalDirectory) validateStable() error {
	if directory == nil || directory.info == nil {
		return fmt.Errorf("CanvasPlan journal directory identity is missing")
	}
	current, err := os.Lstat(directory.path)
	if err != nil {
		return fmt.Errorf("reinspect CanvasPlan journal directory: %w", err)
	}
	if fileInfoIsLinkLike(current) || !current.IsDir() || !os.SameFile(directory.info, current) {
		return fmt.Errorf("CanvasPlan journal directory changed during operation")
	}
	return nil
}

func openRegularFileNoFollow(path string, flags int, mode os.FileMode) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil && !(os.IsNotExist(err) && flags&os.O_CREATE != 0) {
		return nil, err
	}
	if err == nil {
		if fileInfoIsLinkLike(before) {
			return nil, fmt.Errorf("refusing symbolic link: %s", path)
		}
		if !before.Mode().IsRegular() {
			return nil, fmt.Errorf("refusing non-regular file: %s", path)
		}
	}
	file, err := openFileNoFollow(path, flags, mode)
	if err != nil {
		return nil, err
	}
	if err := validateOpenedRegularFile(path, file, before); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateOpenedRegularFile(path string, file *os.File, before os.FileInfo) error {
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened file: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect opened file path: %w", err)
	}
	if !opened.Mode().IsRegular() || fileInfoIsLinkLike(current) || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		return fmt.Errorf("opened CanvasPlan path is not the expected regular file: %s", path)
	}
	if before != nil && !os.SameFile(before, current) {
		return fmt.Errorf("CanvasPlan file changed while it was being opened: %s", path)
	}
	return nil
}

func lstatRegularOrMissing(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if fileInfoIsLinkLike(info) {
		return nil, fmt.Errorf("refusing symbolic link: %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular file: %s", path)
	}
	return info, nil
}
