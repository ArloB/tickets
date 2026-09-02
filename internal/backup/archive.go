package backup

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func WriteZip(w io.Writer, dir string) error {
	zw := zip.NewWriter(w)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		dest, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		_, err = io.Copy(dest, src)
		return err
	})
	if err != nil {
		_ = zw.Close()
		return fmt.Errorf("backup: write zip: %w", err)
	}
	return zw.Close()
}

func ExtractZip(zipPath, destDir string) error {
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("backup: extract: destination %s already exists", destDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("backup: extract: stat destination: %w", err)
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("backup: open archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("backup: extract: create destination: %w", err)
	}

	for _, f := range r.File {
		target := filepath.Join(destDir, filepath.FromSlash(f.Name))
		if !isWithinDir(destDir, target) {
			return fmt.Errorf("backup: extract: archive entry %q escapes the destination directory", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("backup: extract: create %s: %w", f.Name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("backup: extract: create %s: %w", f.Name, err)
		}
		if err := extractFile(f, target); err != nil {
			return fmt.Errorf("backup: extract: %s: %w", f.Name, err)
		}
	}
	return nil
}

func extractFile(f *zip.File, target string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	dest, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = dest.Close() }()

	_, err = io.Copy(dest, src)
	return err
}

func isWithinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
