package nodeca

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
)

// removeDoubleDots replaces the last occurrence of a double dot ("..") with
// a colon. The config map holding the certificates uses double dots to
// represent a colon separating the registry/service name and port. For
// example "registry.company.com:5000" becomes the "registry.company..5000"
// index on the config map.
func removeDoubleDots(fname string) string {
	if idx := strings.LastIndex(fname, ".."); idx >= 0 {
		fname = fmt.Sprintf("%s:%s", fname[0:idx], fname[idx+2:])
	}
	return fname
}

// copyFile copies a file from srcpath to dstpath. This function copies using
// an intermediary file ($dst.tmp) so we can do a move (rename) after the copy.
// With this we avoid failures on processes that may be reading the destination
// file while the write happens.
func copyFile(srcpath, dstpath string) error {
	srcfp, err := os.Open(srcpath)
	if err != nil {
		return fmt.Errorf("opening input file: %w", err)
	}
	defer srcfp.Close()

	srcstat, err := srcfp.Stat()
	if err != nil {
		return fmt.Errorf("stating input file: %w", err)
	}

	tmppath := fmt.Sprintf("%s.tmp", dstpath)
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	tmpfp, err := os.OpenFile(tmppath, flags, srcstat.Mode())
	if err != nil {
		return fmt.Errorf("opening temp file: %w", err)
	}

	defer func() {
		_ = tmpfp.Close()
		err := os.Remove(tmppath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			klog.Errorf("removing temporary file: %v", err)
		}
	}()

	if _, err := io.Copy(tmpfp, srcfp); err != nil {
		return fmt.Errorf("copying input to temp: %w", err)
	}

	if err := tmpfp.Sync(); err != nil {
		return fmt.Errorf("syncing temp file: %w", err)
	}

	if err := os.Rename(tmppath, dstpath); err != nil {
		return fmt.Errorf("moving temp to destination: %w", err)
	}

	return nil
}

// SyncCerts mirrors src directory to dst by copying new/updated files and
// removing files from dstdir that no longer exist in srcdir. This func
// returns the number of copied files (new or updated certs), skipped and
// trimmed certs.
func SyncCerts(srcdir, dstdir string) (int, int, int, error) {
	copied, skipped, err := copyFrom(srcdir, dstdir)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("copying dir content: %w", err)
	}
	trimmed, err := trim(srcdir, dstdir)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("trimming dir content: %w", err)
	}
	return copied, skipped, trimmed, nil
}

// trim removes directories from dstdir that don't exist, as files, in srcdir.
// Source filenames are transformed via removeDoubleDots for comparison.
// Errors removing individual directories are logged, not returned. Returns
// the number of directories removed from dstdir.
func trim(srcdir, dstdir string) (int, error) {
	entries, err := os.ReadDir(srcdir)
	if err != nil {
		return 0, fmt.Errorf("reading src: %w", err)
	}

	keep := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		dirname := removeDoubleDots(entry.Name())
		keep[dirname] = true
	}

	entries, err = os.ReadDir(dstdir)
	if err != nil {
		return 0, fmt.Errorf("reading current: %w", err)
	}

	trimmed := 0
	for _, entry := range entries {
		if !entry.IsDir() || keep[entry.Name()] {
			continue
		}
		dstpath := filepath.Join(dstdir, entry.Name())
		if err := os.RemoveAll(dstpath); err != nil {
			klog.Errorf("removing %s: %v", dstpath, err)
			continue
		}
		trimmed++
	}

	return trimmed, nil
}

// copyFrom copies all files from srcdir directory to dstdir, creating a
// subdirectory per file with transformed name and copying it as ca.crt.
// For example, srcdir/registry..5000 file is copied to
// dst/registry:5000/ca.crt. Errors copying individual files are logged
// but not returned. If dstdir does not exist this function creates it.
// Returns the number of copied and skipped files.
func copyFrom(srcdir, dstdir string) (int, int, error) {
	if dststat, err := os.Stat(dstdir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return 0, 0, fmt.Errorf("reading destination: %w", err)
		}
		if err := os.MkdirAll(dstdir, 0o755); err != nil {
			return 0, 0, fmt.Errorf("creating destination: %w", err)
		}
	} else if !dststat.IsDir() {
		return 0, 0, fmt.Errorf("%s is not a directory", dstdir)
	}

	entries, err := os.ReadDir(srcdir)
	if err != nil {
		return 0, 0, fmt.Errorf("listing service ca: %w", err)
	}

	copied, skipped := 0, 0
	for _, entry := range entries {
		srcpath := filepath.Join(srcdir, entry.Name())
		srcstat, err := os.Stat(srcpath)
		if err != nil {
			klog.Errorf("stat %s: %v", srcpath, err)
			continue
		}

		if srcstat.IsDir() {
			continue
		}

		dirname := removeDoubleDots(entry.Name())
		subdir := filepath.Join(dstdir, dirname)
		dstpath := filepath.Join(subdir, "ca.crt")

		if err := os.MkdirAll(subdir, 0o755); err != nil {
			klog.Errorf("creating directory %s: %v", subdir, err)
			continue
		}

		dststat, err := os.Stat(dstpath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			klog.Errorf("stat %s: %v", dstpath, err)
			continue
		} else if err == nil {
			if srcstat.ModTime().Before(dststat.ModTime()) {
				skipped++
				continue
			}
		}

		if err := copyFile(srcpath, dstpath); err != nil {
			klog.Errorf("copying %s: %v", srcpath, err)
			continue
		}

		copied++
		klog.Infof("%s copied to %s", srcpath, dstpath)
	}
	return copied, skipped, nil
}
