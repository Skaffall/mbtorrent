// Package api — локальный HTTP-сервер: JSON API и встроенный интерфейс.
package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mbtorrent/internal/buildinfo"
	"mbtorrent/internal/config"
	"mbtorrent/internal/engine"
	"mbtorrent/internal/platform"
	"mbtorrent/web"
)

// Server связывает движок и HTTP.
type Server struct {
	mgr  *engine.Manager
	log  *slog.Logger
	quit chan<- struct{}
}

// New создаёт сервер. quit получает сигнал при выходе через интерфейс.
func New(mgr *engine.Manager, log *slog.Logger, quit chan<- struct{}) *Server {
	return &Server{mgr: mgr, log: log, quit: quit}
}

// Handler собирает маршруты и защитные проверки.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"app":     "mbtorrent",
			"version": buildinfo.Version,
		})
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.mgr.Snapshot())
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.mgr.Config())
	})
	mux.HandleFunc("PUT /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/torrents", s.handleAdd)
	mux.HandleFunc("GET /api/torrents/{hash}/files", s.handleFiles)
	mux.HandleFunc("POST /api/torrents/{hash}/{action}", s.handleTorrentAction)
	mux.HandleFunc("POST /api/quit", s.handleQuit)

	static := web.Handler()
	mux.Handle("GET /", static)

	return s.guard(s.recoverer(mux))
}

// guard не даёт управлять клиентом чужим сайтам: сервер слушает только
// loopback, Host обязан быть локальным, а изменения требуют своего заголовка
// (браузер не отправит его в cross-origin запросе без CORS-префлайта).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")

		if !isLocalHost(r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-MBTorrent") != "1" {
				writeErr(w, http.StatusForbidden, "missing X-MBTorrent header")
				return
			}
			if ct := r.Header.Get("Content-Type"); ct != "" &&
				!strings.HasPrefix(ct, "application/json") {
				writeErr(w, http.StatusUnsupportedMediaType, "expected application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLocalHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("паника в обработчике", "path", r.URL.Path, "panic", v)
				writeErr(w, http.StatusInternalServerError, "внутренняя ошибка")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type addRequest struct {
	Magnet  string `json:"magnet"`
	URL     string `json:"url"`
	Torrent string `json:"torrent"` // base64 от .torrent
	Paused  bool   `json:"paused"`
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var (
		hash string
		err  error
	)
	switch {
	case req.Torrent != "":
		data, decErr := base64.StdEncoding.DecodeString(req.Torrent)
		if decErr != nil {
			writeErr(w, http.StatusBadRequest, "битый .torrent")
			return
		}
		hash, err = s.mgr.AddTorrentBytes(data, req.Paused)
	case req.Magnet != "":
		hash, err = s.mgr.AddMagnet(req.Magnet, req.Paused)
	case req.URL != "":
		hash, err = s.addFromURL(req.URL, req.Paused)
	default:
		writeErr(w, http.StatusBadRequest, "нужна magnet-ссылка, URL или .torrent")
		return
	}

	if errors.Is(err, engine.ErrAlreadyAdded) {
		writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "added": false})
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "added": true})
}

func (s *Server) addFromURL(rawURL string, paused bool) (string, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "", errors.New("поддерживаются только http(s)-ссылки")
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("не удалось скачать .torrent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("сервер ответил %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(string(body))), "magnet:") {
		return s.mgr.AddMagnet(strings.TrimSpace(string(body)), paused)
	}
	return s.mgr.AddTorrentBytes(body, paused)
}

func (s *Server) handleTorrentAction(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	action := r.PathValue("action")

	switch action {
	case "pause":
		if err := s.mgr.SetPaused(hash, true); err != nil {
			writeErr(w, statusFor(err), err.Error())
			return
		}
	case "resume":
		if err := s.mgr.SetPaused(hash, false); err != nil {
			writeErr(w, statusFor(err), err.Error())
			return
		}
	case "recheck":
		if err := s.mgr.Recheck(hash); err != nil {
			writeErr(w, statusFor(err), err.Error())
			return
		}
	case "remove":
		deleteData := r.URL.Query().Get("delete_data") == "1"
		if err := s.mgr.Remove(hash, deleteData); err != nil {
			writeErr(w, statusFor(err), err.Error())
			return
		}
	case "reveal":
		s.reveal(hash)
	default:
		writeErr(w, http.StatusNotFound, "неизвестное действие")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) reveal(hash string) {
	cfg := s.mgr.Config()
	if files, err := s.mgr.Files(hash); err == nil && len(files) > 0 {
		full := filepath.Join(cfg.DownloadDir, filepath.FromSlash(files[0].Path))
		if _, err := os.Stat(full); err == nil {
			_ = platform.RevealInExplorer(full)
			return
		}
	}
	_ = platform.OpenFolder(cfg.DownloadDir)
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.mgr.Files(r.PathValue("hash"))
	if err != nil {
		writeErr(w, statusFor(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var next config.Config
	if err := decodeJSON(r, &next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	oldPort := s.mgr.Config().WebPort
	if err := s.mgr.UpdateSettings(next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config":           s.mgr.Config(),
		"restart_required": next.WebPort != oldPort,
	})
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	go func() {
		time.Sleep(150 * time.Millisecond)
		select {
		case s.quit <- struct{}{}:
		default:
		}
	}()
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("некорректный запрос: %w", err)
	}
	return nil
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, engine.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, engine.ErrNoMetadata):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
