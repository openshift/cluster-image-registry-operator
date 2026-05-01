package nodeca

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func Test_removeDoubleDots(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "converts IP address with port",
			input:    "1.1.1.1..5000",
			expected: "1.1.1.1:5000",
		},
		{
			name:     "converts domain registry with port",
			input:    "my.registry.com..3000",
			expected: "my.registry.com:3000",
		},
		{
			name:     "converts localhost with port",
			input:    "localhost..8080",
			expected: "localhost:8080",
		},
		{
			name:     "converts registry with subdomain",
			input:    "quay.io..443.crt",
			expected: "quay.io:443.crt",
		},
		{
			name:     "returns unchanged when no port separator",
			input:    "registry.example.com.crt",
			expected: "registry.example.com.crt",
		},
		{
			name:     "handles registry with multiple dots and port",
			input:    "reg.sub.example.com..5000",
			expected: "reg.sub.example.com:5000",
		},
		{
			name:     "returns empty string unchanged",
			input:    "",
			expected: "",
		},
		{
			name:     "handles already formatted registry address",
			input:    "registry.com:5000",
			expected: "registry.com:5000",
		},
		{
			name:     "replace only the last pattern",
			input:    "this..is..not..valid..5000.crt",
			expected: "this..is..not..valid:5000.crt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if result := removeDoubleDots(tt.input); result != tt.expected {
				t.Errorf("removeDoubleDot(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func Test_copyFile(t *testing.T) {
	t.Run("success cases", func(t *testing.T) {
		tests := []struct {
			name     string
			setup    func(t *testing.T, tmpDir string) (src, dst string)
			validate func(t *testing.T, tmpDir, src, dst string)
		}{
			{
				name: "copies file successfully",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "source.txt")
					dst := filepath.Join(tmpDir, "dest.txt")
					if err := os.WriteFile(src, []byte("test content\n"), 0o644); err != nil {
						t.Fatalf("failed to create source file: %v", err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					got, err := os.ReadFile(dst)
					if err != nil {
						t.Fatalf("failed to read destination: %v", err)
					}
					if string(got) != "test content\n" {
						t.Errorf("content mismatch: got %q, want %q", got, "test content\n")
					}
				},
			},
			{
				name: "preserves file permissions",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "source.txt")
					dst := filepath.Join(tmpDir, "dest.txt")
					if err := os.WriteFile(src, []byte("content"), 0o755); err != nil {
						t.Fatalf("failed to create source file: %v", err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					srcInfo, _ := os.Stat(src)
					dstInfo, _ := os.Stat(dst)
					if srcInfo.Mode() != dstInfo.Mode() {
						t.Errorf("permission mismatch: src=%v, dst=%v", srcInfo.Mode(), dstInfo.Mode())
					}
				},
			},
			{
				name: "overwrites existing destination",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "source.txt")
					dst := filepath.Join(tmpDir, "dest.txt")
					if err := os.WriteFile(dst, []byte("old content\n"), 0o644); err != nil {
						t.Fatalf("failed to create destination file: %v", err)
					}
					if err := os.WriteFile(src, []byte("new content\n"), 0o644); err != nil {
						t.Fatalf("failed to create source file: %v", err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					got, err := os.ReadFile(dst)
					if err != nil {
						t.Fatalf("failed to read destination: %v", err)
					}
					if string(got) != "new content\n" {
						t.Errorf("content not overwritten: got %q, want %q", got, "new content\n")
					}
				},
			},
			{
				name: "cleans up temp file after success",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "source.txt")
					dst := filepath.Join(tmpDir, "dest.txt")
					if err := os.WriteFile(src, []byte("content"), 0o644); err != nil {
						t.Fatalf("failed to create source file: %v", err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					tempFile := dst + ".tmp"
					if _, err := os.Stat(tempFile); err == nil {
						t.Error("temp file should not exist after successful copy")
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				src, dst := tt.setup(t, tmpDir)

				if err := copyFile(src, dst); err != nil {
					t.Fatalf("copyFile() failed: %v", err)
				}
				tt.validate(t, tmpDir, src, dst)
			})
		}
	})

	t.Run("error cases", func(t *testing.T) {
		tests := []struct {
			name     string
			setup    func(t *testing.T, tmpDir string) (src, dst string)
			validate func(t *testing.T, tmpDir, src, dst string)
		}{
			{
				name: "source does not exist",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "nonexistent.txt")
					dst := filepath.Join(tmpDir, "dest.txt")
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					tempFile := dst + ".tmp"
					if _, err := os.Stat(tempFile); err == nil {
						t.Error("temp file should be cleaned up after error")
					}
				},
			},
			{
				name: "source is a directory",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "srcdir")
					dst := filepath.Join(tmpDir, "dest.txt")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatalf("failed to create source directory: %v", err)
					}
					return src, dst
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				src, dst := tt.setup(t, tmpDir)

				err := copyFile(src, dst)
				if err == nil {
					t.Fatal("expected error but got nil")
				}

				if tt.validate != nil {
					tt.validate(t, tmpDir, src, dst)
				}
			})
		}
	})
}

func Test_copyFrom(t *testing.T) {
	t.Run("success cases", func(t *testing.T) {
		tests := []struct {
			name     string
			setup    func(t *testing.T, tmpDir string) (src, dst string)
			validate func(t *testing.T, tmpDir, src, dst string)
		}{
			{
				name: "creates directory and ca.crt file",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "registry..5000"), []byte("cert"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					dstdir := filepath.Join(dst, "registry:5000")
					if _, err := os.Stat(dstdir); err != nil {
						t.Error("registry:5000 directory should exist")
					}
					got, err := os.ReadFile(filepath.Join(dstdir, "ca.crt"))
					if err != nil {
						t.Fatalf("ca.crt should exist: %v", err)
					}
					if string(got) != "cert" {
						t.Errorf("content mismatch: got %q, want %q", got, "cert")
					}
				},
			},
			{
				name: "handles multiple files",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "a..80"), []byte("cert1"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "b..90"), []byte("cert2"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					got1, err := os.ReadFile(filepath.Join(dst, "a:80", "ca.crt"))
					if err != nil {
						t.Errorf("a:80/ca.crt should exist: %v", err)
					} else if string(got1) != "cert1" {
						t.Errorf("a:80/ca.crt content mismatch")
					}
					got2, err := os.ReadFile(filepath.Join(dst, "b:90", "ca.crt"))
					if err != nil {
						t.Errorf("b:90/ca.crt should exist: %v", err)
					} else if string(got2) != "cert2" {
						t.Errorf("b:90/ca.crt content mismatch")
					}
				},
			},
			{
				name: "updates existing ca.crt when source newer",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dst, "registry:5000"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dst, "registry:5000", "ca.crt"), []byte("old"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "registry..5000"), []byte("new"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					got, _ := os.ReadFile(filepath.Join(dst, "registry:5000", "ca.crt"))
					if string(got) != "new" {
						t.Errorf("ca.crt should be updated to new content")
					}
				},
			},
			{
				name: "creates destination directory if missing",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "file..80"), []byte("data"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(dst); err != nil {
						t.Error("dst directory should be created")
					}
					if _, err := os.Stat(filepath.Join(dst, "file:80", "ca.crt")); err != nil {
						t.Error("file:80/ca.crt should exist")
					}
				},
			},
			{
				name: "skips directories in source",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(src, "subdir"), 0o755); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					entries, _ := os.ReadDir(dst)
					if len(entries) != 0 {
						t.Error("source directories should be skipped")
					}
				},
			},
			{
				name: "skips symlinks to directories but processes symlinks to files",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}

					// Create actual cert file in a subdirectory (simulating ConfigMap structure)
					dataDir := filepath.Join(tmpDir, "data")
					if err := os.Mkdir(dataDir, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dataDir, "registry..5000"), []byte("cert1"), 0o644); err != nil {
						t.Fatal(err)
					}

					// Create symlink to file (like ConfigMap entries)
					if err := os.Symlink(filepath.Join(dataDir, "registry..5000"), filepath.Join(src, "registry..5000")); err != nil {
						t.Fatal(err)
					}

					// Create symlink to directory (like ConfigMap ..data)
					if err := os.Symlink(dataDir, filepath.Join(src, "..data")); err != nil {
						t.Fatal(err)
					}

					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					entries, _ := os.ReadDir(dst)
					if len(entries) != 1 {
						t.Errorf("expected 1 entry (from symlink to file), got %d", len(entries))
					}
					// Symlink to file should be processed
					if _, err := os.Stat(filepath.Join(dst, "registry:5000", "ca.crt")); err != nil {
						t.Error("registry:5000/ca.crt should exist from symlink to file")
					}
					// Symlink to directory should be skipped
					if _, err := os.Stat(filepath.Join(dst, ":data")); err == nil {
						t.Error("symlink to directory should be skipped, :data should not exist")
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				src, dst := tt.setup(t, tmpDir)

				_, _, err := copyFrom(src, dst)
				if err != nil {
					t.Fatalf("CopyFrom() failed: %v", err)
				}

				tt.validate(t, tmpDir, src, dst)
			})
		}
	})

	t.Run("error cases", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(t *testing.T, tmpDir string) (src, dst string)
		}{
			{
				name: "source does not exist",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					return filepath.Join(tmpDir, "nonexistent"), dst
				},
			},
			{
				name: "destination is a file",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(dst, []byte("file"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				src, dst := tt.setup(t, tmpDir)

				_, _, err := copyFrom(src, dst)
				if err == nil {
					t.Fatal("expected error but got nil")
				}
			})
		}
	})
}

func Test_trim(t *testing.T) {
	t.Run("success cases", func(t *testing.T) {
		tests := []struct {
			name     string
			setup    func(t *testing.T, tmpDir string) (src, dst string)
			validate func(t *testing.T, tmpDir, src, dst string)
		}{
			{
				name: "removes directories not in source",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "registry..5000"), []byte("cert"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dst, "registry:5000"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dst, "old:8080"), 0o755); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(filepath.Join(dst, "registry:5000")); err != nil {
						t.Error("registry:5000 should be kept")
					}
					if _, err := os.Stat(filepath.Join(dst, "old:8080")); err == nil {
						t.Error("old:8080 should be removed")
					}
				},
			},
			{
				name: "keeps directories with matching source files",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "a..80"), []byte("1"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "b..90"), []byte("2"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dst, "a:80"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dst, "b:90"), 0o755); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(filepath.Join(dst, "a:80")); err != nil {
						t.Error("a:80 should exist")
					}
					if _, err := os.Stat(filepath.Join(dst, "b:90")); err != nil {
						t.Error("b:90 should exist")
					}
				},
			},
			{
				name: "ignores files in destination",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dst, "somefile.txt"), []byte("data"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(filepath.Join(dst, "somefile.txt")); err != nil {
						t.Error("files should be ignored, not removed")
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				src, dst := tt.setup(t, tmpDir)

				_, err := trim(src, dst)
				if err != nil {
					t.Fatalf("Trim() failed: %v", err)
				}

				tt.validate(t, tmpDir, src, dst)
			})
		}
	})

	t.Run("error cases", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(t *testing.T, tmpDir string) (src, dst string)
		}{
			{
				name: "source does not exist",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					return filepath.Join(tmpDir, "nonexistent"), dst
				},
			},
			{
				name: "destination does not exist",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					return src, filepath.Join(tmpDir, "nonexistent")
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				src, dst := tt.setup(t, tmpDir)

				_, err := trim(src, dst)
				if err == nil {
					t.Fatal("expected error but got nil")
				}
			})
		}
	})
}

func TestSyncCerts(t *testing.T) {
	t.Run("success cases", func(t *testing.T) {
		tests := []struct {
			name            string
			expectedCopied  int
			expectedSkipped int
			expectedTrimmed int
			setup           func(t *testing.T, tmpDir string) (src, dst string)
			validate        func(t *testing.T, tmpDir, src, dst string)
		}{
			{
				name:            "destination directory is created",
				expectedCopied:  1,
				expectedSkipped: 0,
				expectedTrimmed: 0,
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "registry..5000"), []byte("cert1"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(filepath.Join(dst, "registry:5000")); err != nil {
						t.Error("registry:5000 directory should exist")
					}
					got, err := os.ReadFile(filepath.Join(dst, "registry:5000", "ca.crt"))
					if err != nil {
						t.Fatalf("ca.crt should exist: %v", err)
					}
					if string(got) != "cert1" {
						t.Errorf("content mismatch: got %q, want %q", got, "cert1")
					}
				},
			},
			{
				name:            "creates directory structure from files",
				expectedCopied:  1,
				expectedSkipped: 0,
				expectedTrimmed: 0,
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "registry..5000"), []byte("cert1"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(filepath.Join(dst, "registry:5000")); err != nil {
						t.Error("registry:5000 directory should exist")
					}
					got, err := os.ReadFile(filepath.Join(dst, "registry:5000", "ca.crt"))
					if err != nil {
						t.Fatalf("ca.crt should exist: %v", err)
					}
					if string(got) != "cert1" {
						t.Errorf("content mismatch: got %q, want %q", got, "cert1")
					}
				},
			},
			{
				name:            "removes orphaned directories",
				expectedCopied:  0,
				expectedSkipped: 0,
				expectedTrimmed: 1,
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dst, "old:8080"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dst, "old:8080", "ca.crt"), []byte("old"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(filepath.Join(dst, "old:8080")); err == nil {
						t.Error("old:8080 should be removed")
					}
				},
			},
			{
				name:            "complete sync operation",
				expectedCopied:  2,
				expectedSkipped: 0,
				expectedTrimmed: 1,
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "keep..80"), []byte("keep"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "new..90"), []byte("new"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dst, "keep:80"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dst, "keep:80", "ca.crt"), []byte("keep"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(filepath.Join(dst, "remove:70"), 0o755); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(filepath.Join(dst, "keep:80", "ca.crt")); err != nil {
						t.Error("keep:80/ca.crt should exist")
					}
					if _, err := os.Stat(filepath.Join(dst, "new:90", "ca.crt")); err != nil {
						t.Error("new:90/ca.crt should be created")
					}
					if _, err := os.Stat(filepath.Join(dst, "remove:70")); err == nil {
						t.Error("remove:70 should be removed")
					}
				},
			},
			{
				name:            "skips files when destination is newer",
				expectedCopied:  1,
				expectedSkipped: 1,
				expectedTrimmed: 0,
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}

					// Create source files
					if err := os.WriteFile(filepath.Join(src, "old..80"), []byte("old"), 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(src, "new..90"), []byte("new"), 0o644); err != nil {
						t.Fatal(err)
					}

					// Create destination cert with newer timestamp
					if err := os.Mkdir(filepath.Join(dst, "old:80"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dst, "old:80", "ca.crt"), []byte("old"), 0o644); err != nil {
						t.Fatal(err)
					}

					// Set destination to be 1 hour in the future
					future := time.Now().Add(time.Hour)
					if err := os.Chtimes(filepath.Join(dst, "old:80", "ca.crt"), future, future); err != nil {
						t.Fatal(err)
					}

					return src, dst
				},
				validate: func(t *testing.T, tmpDir, src, dst string) {
					if _, err := os.Stat(filepath.Join(dst, "old:80", "ca.crt")); err != nil {
						t.Error("old:80/ca.crt should exist (skipped)")
					}
					if _, err := os.Stat(filepath.Join(dst, "new:90", "ca.crt")); err != nil {
						t.Error("new:90/ca.crt should be created")
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				src, dst := tt.setup(t, tmpDir)

				copied, skipped, trimmed, err := SyncCerts(src, dst)
				if err != nil {
					t.Fatalf("Sync() failed: %v", err)
				}

				if copied != tt.expectedCopied {
					t.Errorf("expected %d files copied, got %d", tt.expectedCopied, copied)
				}
				if skipped != tt.expectedSkipped {
					t.Errorf("expected %d files skipped, got %d", tt.expectedSkipped, skipped)
				}
				if trimmed != tt.expectedTrimmed {
					t.Errorf("expected %d directories trimmed, got %d", tt.expectedTrimmed, trimmed)
				}

				tt.validate(t, tmpDir, src, dst)
			})
		}
	})

	t.Run("error cases", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(t *testing.T, tmpDir string) (src, dst string)
		}{
			{
				name: "source does not exist",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(dst, 0o755); err != nil {
						t.Fatal(err)
					}
					return filepath.Join(tmpDir, "nonexistent"), dst
				},
			},
			{
				name: "destination is a file",
				setup: func(t *testing.T, tmpDir string) (string, string) {
					src := filepath.Join(tmpDir, "src")
					dst := filepath.Join(tmpDir, "dst")
					if err := os.Mkdir(src, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(dst, []byte("file"), 0o644); err != nil {
						t.Fatal(err)
					}
					return src, dst
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir := t.TempDir()
				src, dst := tt.setup(t, tmpDir)
				if _, _, _, err := SyncCerts(src, dst); err == nil {
					t.Fatal("expected error but got nil")
				}
			})
		}
	})
}
