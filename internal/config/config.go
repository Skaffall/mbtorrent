// Package config хранит настройки mbtorrent и пути к его данным.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appName = "mbtorrent"

// Config — пользовательские настройки. Всё, что меняет движок
// (папка, порт, лимиты), требует пересоздания torrent-клиента.
type Config struct {
	DownloadDir string `json:"download_dir"`
	ListenPort  int    `json:"listen_port"`
	WebPort     int    `json:"web_port"`
	DownLimitKB int64  `json:"down_limit_kb"` // 0 = без ограничений
	UpLimitKB   int64  `json:"up_limit_kb"`   // 0 = без ограничений
	Seed        bool   `json:"seed"`
	StartPaused bool   `json:"start_paused"`
}

// Default возвращает настройки по умолчанию.
func Default() Config {
	home, _ := os.UserHomeDir()
	downloads := filepath.Join(home, "Downloads", appName)
	if runtime.GOOS == "windows" {
		// На Windows папка «Загрузки» называется Downloads независимо от языка.
		downloads = filepath.Join(home, "Downloads", appName)
	}
	return Config{
		DownloadDir: downloads,
		ListenPort:  0, // 0 = выбрать свободный автоматически
		WebPort:     8642,
		DownLimitKB: 0,
		UpLimitKB:   0,
		Seed:        true,
		StartPaused: false,
	}
}

// Dir — каталог с конфигом, состоянием и resume-данными.
// Windows: %AppData%\mbtorrent, Linux: ~/.config/mbtorrent.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить каталог настроек: %w", err)
	}
	dir := filepath.Join(base, appName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Load читает config.json, создавая его при первом запуске.
func Load() (Config, error) {
	dir, err := Dir()
	if err != nil {
		return Config{}, err
	}
	path := filepath.Join(dir, "config.json")

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		if err := cfg.Save(); err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("config.json повреждён: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		// Не блокируем запуск из-за кривого конфига — чиним и продолжаем.
		fixed := Default()
		fixed.DownloadDir = cfg.DownloadDir
		if fixed.DownloadDir == "" {
			fixed.DownloadDir = Default().DownloadDir
		}
		_ = fixed.Save()
		return fixed, nil
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	d := Default()
	if c.DownloadDir == "" {
		c.DownloadDir = d.DownloadDir
	}
	if c.WebPort == 0 {
		c.WebPort = d.WebPort
	}
}

// Validate проверяет настройки перед применением.
func (c Config) Validate() error {
	if c.DownloadDir == "" {
		return errors.New("не выбрана папка загрузок")
	}
	if c.ListenPort != 0 && (c.ListenPort < 1024 || c.ListenPort > 65535) {
		return errors.New("порт BitTorrent должен быть 0 (авто) или 1024–65535")
	}
	if c.WebPort < 1024 || c.WebPort > 65535 {
		return errors.New("порт панели должен быть в диапазоне 1024–65535")
	}
	if c.ListenPort == c.WebPort {
		return errors.New("порты BitTorrent и панели не должны совпадать")
	}
	if c.DownLimitKB < 0 || c.UpLimitKB < 0 {
		return errors.New("лимиты скорости не могут быть отрицательными")
	}
	return nil
}

// Save атомарно записывает конфиг.
func (c Config) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, "config.json"), raw)
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
