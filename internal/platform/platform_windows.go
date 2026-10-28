//go:build windows

// Package platform — тонкий слой над Win32: открыть браузер, показать файл
// в проводнике, узнать свободное место, вывести ошибку без консоли.
package platform

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW      = user32.NewProc("MessageBoxW")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procGetDiskFreeSpace = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// OpenURL открывает ссылку в браузере по умолчанию.
func OpenURL(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// RevealInExplorer открывает проводник с выделенным файлом.
func RevealInExplorer(path string) error {
	return exec.Command("explorer", "/select,"+path).Start()
}

// OpenFolder открывает папку в проводнике.
func OpenFolder(path string) error {
	return exec.Command("explorer", path).Start()
}

// FreeSpace возвращает свободные байты на диске, где лежит path.
func FreeSpace(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	r1, _, e1 := procGetDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return 0, e1
	}
	return int64(free), nil
}

// ErrorBox показывает нативное окно с ошибкой (полезно в GUI-сборке,
// где нет консоли).
func ErrorBox(title, message string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(message)
	const mbIconError = 0x00000010
	procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(m)),
		uintptr(unsafe.Pointer(t)),
		mbIconError)
}

// HideConsole — заглушка: консоль скрывается флагом сборки -H=windowsgui.
func HideConsole() {}
