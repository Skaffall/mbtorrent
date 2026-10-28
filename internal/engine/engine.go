// Package engine — обёртка над BitTorrent-движком: добавление раздач,
// пауза, удаление, настройки и хранение состояния между запусками.
package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"

	"mbtorrent/internal/buildinfo"
	"mbtorrent/internal/config"
)

var (
	// ErrNotFound — такой раздачи нет в списке.
	ErrNotFound = errors.New("раздача не найдена")
	// ErrAlreadyAdded — раздача уже добавлена.
	ErrAlreadyAdded = errors.New("эта раздача уже добавлена")
	// ErrNoMetadata — метаданные ещё не получены.
	ErrNoMetadata = errors.New("метаданные ещё не получены")
)

// Entry — одна раздача и её рантайм-состояние.
type Entry struct {
	t        *torrent.Torrent
	hash     string
	name     string
	magnet   string
	metafile string
	added    time.Time
	paused   bool
	checking bool
	errText  string

	down, up            int64
	lastRead, lastWrite int64
}

// Manager владеет torrent-клиентом и списком раздач.
type Manager struct {
	mu  sync.RWMutex
	cfg config.Config
	cl  *torrent.Client
	log *slog.Logger

	entries map[string]*Entry
	order   []string

	statePath   string
	torrentsDir string
	resumeDir   string

	downSpeed, upSpeed              int64
	sessionDown, sessionUp          int64
	lastGlobalRead, lastGlobalWrite int64
	started                         time.Time
	stop                            chan struct{}
	stopOnce                        sync.Once
}

type persistedState struct {
	Torrents []persistedTorrent `json:"torrents"`
}

type persistedTorrent struct {
	Hash   string    `json:"hash"`
	Magnet string    `json:"magnet,omitempty"`
	File   string    `json:"file,omitempty"`
	Added  time.Time `json:"added"`
	Paused bool      `json:"paused"`
}

// New создаёт менеджер, поднимает движок и восстанавливает список раздач.
func New(cfg config.Config, log *slog.Logger) (*Manager, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}

	m := &Manager{
		cfg:         cfg,
		log:         log,
		entries:     make(map[string]*Entry),
		statePath:   filepath.Join(dir, "state.json"),
		torrentsDir: filepath.Join(dir, "torrents"),
		resumeDir:   filepath.Join(dir, "resume"),
		started:     time.Now(),
		stop:        make(chan struct{}),
	}
	for _, d := range []string{m.torrentsDir, m.resumeDir, cfg.DownloadDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("не удалось создать %s: %w", d, err)
		}
	}

	if err := m.startClient(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.loadStateLocked()
	m.mu.Unlock()

	go m.loop()
	return m, nil
}

// startClient поднимает новый torrent-клиент по текущим настройкам.
// Resume-база лежит отдельно от данных, поэтому после перезапуска
// не нужно перехешировать файлы заново.
func (m *Manager) startClient() error {
	pc, err := storage.NewDefaultPieceCompletionForDir(m.resumeDir)
	if err != nil {
		return fmt.Errorf("resume-база: %w", err)
	}

	tc := torrent.NewDefaultClientConfig()
	tc.DataDir = m.cfg.DownloadDir
	tc.DefaultStorage = storage.NewFileWithCompletion(m.cfg.DownloadDir, pc)
	tc.ListenPort = m.cfg.ListenPort
	tc.Seed = true
	tc.Slogger = m.log
	tc.HTTPUserAgent = buildinfo.UserAgent
	tc.ExtendedHandshakeClientVersion = buildinfo.UserAgent
	tc.UpnpID = buildinfo.UserAgent
	tc.DisableIPv6 = false

	if m.cfg.DownLimitKB > 0 {
		tc.DownloadRateLimiter = rate.NewLimiter(rate.Limit(m.cfg.DownLimitKB*1024), 0)
	}
	if m.cfg.UpLimitKB > 0 {
		tc.UploadRateLimiter = rate.NewLimiter(rate.Limit(m.cfg.UpLimitKB*1024), 0)
	}

	cl, err := torrent.NewClient(tc)
	if err != nil {
		return fmt.Errorf("не удалось запустить BitTorrent-движок: %w", err)
	}
	m.cl = cl
	m.log.Info("движок запущен", "port", cl.LocalPort(), "dir", m.cfg.DownloadDir)
	return nil
}

// ensureDownloading включает все куски в очередь. Без этого anacrolix/torrent
// не начинает качать: приоритеты по умолчанию — «не качать».
func ensureDownloading(t *torrent.Torrent) {
	if t.Info() != nil {
		t.DownloadAll()
		return
	}
	go func() {
		<-t.GotInfo()
		t.DownloadAll()
	}()
}

func (m *Manager) loadStateLocked() {
	raw, err := os.ReadFile(m.statePath)
	if err != nil {
		return
	}
	var st persistedState
	if err := json.Unmarshal(raw, &st); err != nil {
		m.log.Warn("state.json повреждён, начинаем с пустого списка", "err", err)
		return
	}

	for _, pt := range st.Torrents {
		t := m.restoreTorrent(pt)
		if t == nil {
			continue
		}
		hash := t.InfoHash().HexString()
		if _, ok := m.entries[hash]; ok {
			continue
		}
		e := &Entry{
			t:        t,
			hash:     hash,
			name:     pt.Hash,
			magnet:   pt.Magnet,
			metafile: pt.File,
			added:    pt.Added,
			paused:   pt.Paused,
		}
		if e.added.IsZero() {
			e.added = time.Now()
		}
		if pt.Paused {
			t.DisallowDataDownload()
			t.DisallowDataUpload()
		}
		ensureDownloading(t)
		m.entries[hash] = e
		m.order = append(m.order, hash)
	}
	m.log.Info("список раздач восстановлен", "count", len(m.order))
}

func (m *Manager) restoreTorrent(pt persistedTorrent) *torrent.Torrent {
	if pt.File != "" {
		mi, err := metainfo.LoadFromFile(filepath.Join(m.torrentsDir, pt.File))
		if err == nil {
			if t, err := m.cl.AddTorrent(mi); err == nil {
				return t
			}
		}
	}
	if pt.Magnet != "" {
		if t, err := m.cl.AddMagnet(pt.Magnet); err == nil {
			return t
		}
	}
	return nil
}

// AddMagnet добавляет magnet-ссылку. Метаданные догружаются в фоне.
func (m *Manager) AddMagnet(uri string, paused bool) (string, error) {
	uri = strings.TrimSpace(uri)
	if !strings.HasPrefix(strings.ToLower(uri), "magnet:") {
		return "", errors.New("это не magnet-ссылка")
	}

	var displayName string
	if mag, err := metainfo.ParseMagnetUri(uri); err == nil {
		displayName = mag.DisplayName
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, err := m.cl.AddMagnet(uri)
	if err != nil {
		return "", fmt.Errorf("движок отклонил magnet: %w", err)
	}
	hash := t.InfoHash().HexString()
	if _, ok := m.entries[hash]; ok {
		return hash, ErrAlreadyAdded
	}
	if displayName != "" {
		t.SetDisplayName(displayName)
	}

	e := &Entry{
		t:      t,
		hash:   hash,
		name:   displayName,
		magnet: uri,
		added:  time.Now(),
		paused: paused || m.cfg.StartPaused,
	}
	if e.paused {
		t.DisallowDataDownload()
		t.DisallowDataUpload()
	}
	ensureDownloading(t)

	m.entries[hash] = e
	m.order = append(m.order, hash)
	m.saveLocked()

	go m.awaitMetadata(hash)
	return hash, nil
}

// AddTorrentBytes добавляет раздачу из содержимого .torrent-файла.
func (m *Manager) AddTorrentBytes(data []byte, paused bool) (string, error) {
	if len(data) == 0 {
		return "", errors.New("пустой .torrent-файл")
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return "", errors.New("файл не похож на .torrent")
	}
	if _, err := mi.UnmarshalInfo(); err != nil {
		return "", errors.New("в .torrent нет корректного описания раздачи")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	t, err := m.cl.AddTorrent(mi)
	if err != nil {
		return "", fmt.Errorf("движок отклонил торрент: %w", err)
	}
	hash := t.InfoHash().HexString()
	if _, ok := m.entries[hash]; ok {
		return hash, ErrAlreadyAdded
	}

	file := hash + ".torrent"
	if err := m.writeMetainfo(file, mi); err != nil {
		m.log.Warn("не удалось сохранить .torrent", "err", err)
	}

	e := &Entry{
		t:        t,
		hash:     hash,
		name:     t.Name(),
		metafile: file,
		added:    time.Now(),
		paused:   paused || m.cfg.StartPaused,
	}
	if e.paused {
		t.DisallowDataDownload()
		t.DisallowDataUpload()
	}
	ensureDownloading(t)

	m.entries[hash] = e
	m.order = append(m.order, hash)
	m.saveLocked()
	return hash, nil
}

// awaitMetadata сохраняет .torrent, когда метаданные magnet-ссылки догрузились:
// следующий запуск не будет зависеть от DHT и трекеров.
func (m *Manager) awaitMetadata(hash string) {
	m.mu.RLock()
	e := m.entries[hash]
	m.mu.RUnlock()
	if e == nil {
		return
	}

	select {
	case <-e.t.GotInfo():
	case <-time.After(3 * time.Minute):
		return
	case <-m.stop:
		return
	}

	mi := e.t.Metainfo()
	file := hash + ".torrent"
	if err := m.writeMetainfo(file, &mi); err != nil {
		m.log.Warn("не удалось сохранить метаданные", "hash", hash, "err", err)
		return
	}

	m.mu.Lock()
	if cur, ok := m.entries[hash]; ok {
		cur.metafile = file
		if name := cur.t.Name(); name != "" {
			cur.name = name
		}
		m.saveLocked()
	}
	m.mu.Unlock()
}

func (m *Manager) writeMetainfo(file string, mi *metainfo.MetaInfo) error {
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		return err
	}
	tmp := filepath.Join(m.torrentsDir, file+".tmp")
	final := filepath.Join(m.torrentsDir, file)
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// SetPaused ставит раздачу на паузу или снимает её.
func (m *Manager) SetPaused(hash string, paused bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	e, ok := m.entries[hash]
	if !ok {
		return ErrNotFound
	}
	e.paused = paused
	e.errText = ""
	if paused {
		e.t.DisallowDataDownload()
		e.t.DisallowDataUpload()
		e.down, e.up = 0, 0
	} else {
		e.t.AllowDataDownload()
		e.t.AllowDataUpload()
	}
	m.saveLocked()
	return nil
}

// Recheck запускает проверку локальных данных в фоне.
func (m *Manager) Recheck(hash string) error {
	m.mu.Lock()
	e, ok := m.entries[hash]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	if e.t.Info() == nil {
		m.mu.Unlock()
		return ErrNoMetadata
	}
	e.checking = true
	e.errText = ""
	m.mu.Unlock()

	go func() {
		err := e.t.VerifyData()
		m.mu.Lock()
		e.checking = false
		if err != nil {
			e.errText = err.Error()
		}
		m.mu.Unlock()
	}()
	return nil
}

// Remove убирает раздачу из списка, при необходимости — вместе с файлами.
func (m *Manager) Remove(hash string, deleteData bool) error {
	m.mu.Lock()
	e, ok := m.entries[hash]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}

	var paths []string
	if deleteData && e.t.Info() != nil {
		paths = m.dataPathsLocked(e)
	}

	e.t.Drop()
	delete(m.entries, hash)
	m.order = removeString(m.order, hash)
	_ = os.Remove(filepath.Join(m.torrentsDir, hash+".torrent"))
	m.saveLocked()
	m.mu.Unlock()

	if deleteData {
		m.deleteFiles(paths)
	}
	return nil
}

func (m *Manager) dataPathsLocked(e *Entry) []string {
	base := filepath.Clean(m.cfg.DownloadDir)
	var paths []string
	for _, f := range e.t.Files() {
		p := filepath.Join(base, f.Path())
		rel, err := filepath.Rel(base, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

// deleteFiles удаляет файлы с повторами: на Windows антивирус или индексатор
// могут держать файл ещё пару сотен миллисекунд после закрытия.
func (m *Manager) deleteFiles(paths []string) {
	base := filepath.Clean(m.cfg.DownloadDir)
	for _, p := range paths {
		for attempt := 0; attempt < 6; attempt++ {
			err := os.Remove(p)
			if err == nil || errors.Is(err, os.ErrNotExist) {
				break
			}
			if attempt == 5 {
				m.log.Warn("не удалось удалить файл", "path", p, "err", err)
			}
			time.Sleep(150 * time.Millisecond)
		}
		// Подчищаем опустевшие подпапки раздачи.
		dir := filepath.Dir(p)
		for dir != base && strings.HasPrefix(dir, base+string(filepath.Separator)) {
			if err := os.Remove(dir); err != nil {
				break
			}
			dir = filepath.Dir(dir)
		}
	}
}

// UpdateSettings применяет настройки. Смена папки, порта или лимитов
// пересоздаёт движок; список раздач восстанавливается из state.json.
func (m *Manager) UpdateSettings(next config.Config) error {
	if err := next.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(next.DownloadDir, 0o755); err != nil {
		return fmt.Errorf("папка загрузок недоступна: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.cfg
	if next == prev {
		return nil
	}

	rebuild := next.DownloadDir != prev.DownloadDir ||
		next.ListenPort != prev.ListenPort ||
		next.DownLimitKB != prev.DownLimitKB ||
		next.UpLimitKB != prev.UpLimitKB

	m.cfg = next
	if !rebuild {
		return next.Save()
	}

	m.saveLocked()
	m.cl.Close()
	m.cl = nil
	m.entries = make(map[string]*Entry)
	m.order = nil
	m.lastGlobalRead, m.lastGlobalWrite = 0, 0

	if err := m.startClient(); err != nil {
		m.cfg = prev
		if err2 := m.startClient(); err2 != nil {
			m.log.Error("движок не поднялся после отката настроек", "err", err2)
		}
		m.loadStateLocked()
		return fmt.Errorf("не удалось применить настройки: %w", err)
	}
	m.loadStateLocked()
	return next.Save()
}

// Close останавливает движок, сохраняя состояние и resume-базу.
func (m *Manager) Close() {
	m.stopOnce.Do(func() { close(m.stop) })
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveLocked()
	if m.cl != nil {
		m.cl.Close()
		m.cl = nil
	}
}

func (m *Manager) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.mu.Lock()
			if m.cl != nil {
				m.sampleLocked()
			}
			m.mu.Unlock()
		case <-m.stop:
			return
		}
	}
}

func (m *Manager) saveLocked() {
	st := persistedState{Torrents: make([]persistedTorrent, 0, len(m.order))}
	for _, hash := range m.order {
		e, ok := m.entries[hash]
		if !ok {
			continue
		}
		st.Torrents = append(st.Torrents, persistedTorrent{
			Hash:   e.hash,
			Magnet: e.magnet,
			File:   e.metafile,
			Added:  e.added,
			Paused: e.paused,
		})
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	tmp := m.statePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		m.log.Warn("не удалось сохранить состояние", "err", err)
		return
	}
	if err := os.Rename(tmp, m.statePath); err != nil {
		m.log.Warn("не удалось сохранить состояние", "err", err)
	}
}

func removeString(list []string, target string) []string {
	out := list[:0]
	for _, s := range list {
		if s != target {
			out = append(out, s)
		}
	}
	return out
}
