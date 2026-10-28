// mbtorrent — лёгкий BitTorrent-клиент для Windows.
// Один exe, без Qt и установщика: движок и интерфейс внутри.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mbtorrent/internal/api"
	"mbtorrent/internal/buildinfo"
	"mbtorrent/internal/config"
	"mbtorrent/internal/engine"
	"mbtorrent/internal/platform"
)

func main() {
	var (
		noBrowser = flag.Bool("no-browser", false, "не открывать браузер при запуске")
		portFlag  = flag.Int("port", 0, "порт панели (переопределяет настройки)")
		debug     = flag.Bool("debug", false, "подробный лог в консоль")
		showVer   = flag.Bool("version", false, "показать версию и выйти")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("mbtorrent", buildinfo.Version)
		return
	}

	logger, logCloser := setupLogging(*debug)
	if logCloser != nil {
		defer logCloser.Close()
	}
	logger.Info("запуск", "version", buildinfo.Version)

	cfg, err := config.Load()
	if err != nil {
		fatal(logger, "Не удалось прочитать настройки", err)
	}
	if *portFlag != 0 {
		cfg.WebPort = *portFlag
	}

	// Второй запуск не поднимает второй движок: просто открывает панель.
	if ping(cfg.WebPort) {
		if !*noBrowser {
			_ = platform.OpenURL(urlFor(cfg.WebPort))
		}
		return
	}

	mgr, err := engine.New(cfg, logger)
	if err != nil {
		fatal(logger, "Не удалось запустить mbtorrent", err)
	}
	defer mgr.Close()

	quit := make(chan struct{}, 1)
	server := api.New(mgr, logger, quit)
	httpSrv := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, port, err := listen(cfg.WebPort)
	if err != nil {
		fatal(logger, "Не удалось занять порт панели", err)
	}
	if port != cfg.WebPort {
		logger.Warn("порт занят, выбран соседний", "want", cfg.WebPort, "got", port)
	}

	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP-сервер остановился", "err", err)
			platform.ErrorBox("mbtorrent", "Панель управления остановилась:\n"+err.Error())
			select {
			case quit <- struct{}{}:
			default:
			}
		}
	}()

	url := urlFor(port)
	logger.Info("панель запущена", "url", url)
	if !*noBrowser {
		if err := platform.OpenURL(url); err != nil {
			logger.Warn("не удалось открыть браузер", "err", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	select {
	case <-sig:
		logger.Info("получен сигнал завершения")
	case <-quit:
		logger.Info("выход через интерфейс")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	logger.Info("остановлено")
}

// listen занимает порт панели; если он занят — пробует следующие.
func listen(want int) (net.Listener, int, error) {
	var lastErr error
	for port := want; port < want+10; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return ln, port, nil
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

// ping проверяет, не работает ли уже mbtorrent на этом порту.
func ping(port int) bool {
	client := &http.Client{Timeout: 900 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var v struct {
		App string `json:"app"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&v); err != nil {
		return false
	}
	return v.App == "mbtorrent"
}

func urlFor(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/", port)
}

// setupLogging пишет лог рядом с настройками: в GUI-сборке консоли нет.
func setupLogging(debug bool) (*slog.Logger, *os.File) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}

	dir, err := config.Dir()
	if err != nil {
		dir = "."
	}
	path := filepath.Join(dir, "mbtorrent.log")

	// Простая ротация: держим один предыдущий файл.
	if info, err := os.Stat(path); err == nil && info.Size() > 5<<20 {
		_ = os.Remove(path + ".old")
		_ = os.Rename(path, path+".old")
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, opts)), nil
	}

	var w io.Writer = f
	if debug {
		w = io.MultiWriter(f, os.Stderr)
	}
	return slog.New(slog.NewTextHandler(w, opts)), f
}

func fatal(logger *slog.Logger, title string, err error) {
	logger.Error(title, "err", err)
	platform.ErrorBox("mbtorrent", title+":\n"+err.Error())
	os.Exit(1)
}
