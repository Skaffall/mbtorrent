//go:build !windows

// Package platform — версия для Linux/macOS. Нужна только для разработки:
// целевая платформа mbtorrent — Windows.
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// OpenURL открывает ссылку в браузере по умолчанию.
func OpenURL(url string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}

// RevealInExplorer открывает файловый менеджер рядом с файлом.
func RevealInExplorer(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return OpenFolder(dirOf(path))
}

// OpenFolder открывает папку в файловом менеджере.
func OpenFolder(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

// FreeSpace возвращает свободные байты на диске, где лежит path.
func FreeSpace(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// ErrorBox печатает ошибку в stderr (в Linux консоль всегда есть).
func ErrorBox(title, message string) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", title, message)
}

// HideConsole — заглушка.
func HideConsole() {}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
