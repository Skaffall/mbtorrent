package engine

import (
	"time"

	"mbtorrent/internal/buildinfo"
	"mbtorrent/internal/config"
	"mbtorrent/internal/platform"
)

// Состояния раздачи, которые понимает интерфейс.
const (
	StateMetadata    = "metadata"    // ждём метаданные magnet-ссылки
	StateDownloading = "downloading" // качается
	StateStalled     = "stalled"     // качается, но скорость нулевая
	StateSeeding     = "seeding"     // раздаёт
	StateCompleted   = "completed"   // готово, раздача выключена
	StatePaused      = "paused"      // пауза
	StateChecking    = "checking"    // проверка данных
	StateError       = "error"       // ошибка
)

// TorrentView — то, что видит интерфейс для одной раздачи.
type TorrentView struct {
	Hash       string    `json:"hash"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Done       int64     `json:"done"`
	Progress   float64   `json:"progress"`
	State      string    `json:"state"`
	DownSpeed  int64     `json:"down_speed"`
	UpSpeed    int64     `json:"up_speed"`
	Peers      int       `json:"peers"`
	Seeds      int       `json:"seeds"`
	ETA        int64     `json:"eta"`
	Ratio      float64   `json:"ratio"`
	Added      time.Time `json:"added"`
	SavePath   string    `json:"save_path"`
	FileCount  int       `json:"file_count"`
	Downloaded int64     `json:"downloaded"`
	Uploaded   int64     `json:"uploaded"`
	Magnet     string    `json:"magnet,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Global — сводка по клиенту.
type Global struct {
	DownSpeed   int64 `json:"down_speed"`
	UpSpeed     int64 `json:"up_speed"`
	SessionDown int64 `json:"session_down"`
	SessionUp   int64 `json:"session_up"`
	Torrents    int   `json:"torrents"`
	Active      int   `json:"active"`
	Port        int   `json:"port"`
	FreeSpace   int64 `json:"free_space"`
	Uptime      int64 `json:"uptime"`
}

// Snapshot — полный ответ /api/state.
type Snapshot struct {
	Torrents []TorrentView `json:"torrents"`
	Global   Global        `json:"global"`
	Config   config.Config `json:"config"`
	Version  string        `json:"version"`
}

// FileView — файл внутри раздачи.
type FileView struct {
	Path     string  `json:"path"`
	Length   int64   `json:"length"`
	Done     int64   `json:"done"`
	Progress float64 `json:"progress"`
}

// Snapshot собирает актуальное состояние для интерфейса.
func (m *Manager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := Snapshot{
		Config:   m.cfg,
		Version:  buildinfo.Version,
		Torrents: make([]TorrentView, 0, len(m.order)),
		Global: Global{
			DownSpeed:   m.downSpeed,
			UpSpeed:     m.upSpeed,
			SessionDown: m.sessionDown,
			SessionUp:   m.sessionUp,
			Torrents:    len(m.order),
			Uptime:      int64(time.Since(m.started).Seconds()),
		},
	}

	if m.cl != nil {
		snap.Global.Port = m.cl.LocalPort()
	}
	if free, err := platform.FreeSpace(m.cfg.DownloadDir); err == nil {
		snap.Global.FreeSpace = free
	}

	for _, hash := range m.order {
		e, ok := m.entries[hash]
		if !ok {
			continue
		}
		view := m.viewLocked(e)
		if view.State != StatePaused && view.State != StateCompleted && view.State != StateError {
			snap.Global.Active++
		}
		snap.Torrents = append(snap.Torrents, view)
	}
	return snap
}

// Config возвращает текущие настройки.
func (m *Manager) Config() config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) viewLocked(e *Entry) TorrentView {
	t := e.t
	v := TorrentView{
		Hash:      e.hash,
		Name:      e.name,
		Added:     e.added,
		SavePath:  m.cfg.DownloadDir,
		DownSpeed: e.down,
		UpSpeed:   e.up,
		Magnet:    e.magnet,
		Error:     e.errText,
	}

	if name := t.Name(); name != "" {
		v.Name = name
	}
	if v.Name == "" && len(e.hash) >= 12 {
		v.Name = "Раздача " + e.hash[:12]
	}

	if t.Info() != nil {
		v.Size = t.Length()
		v.Done = t.BytesCompleted()
		v.FileCount = len(t.Files())
		if v.Size > 0 {
			v.Progress = float64(v.Done) / float64(v.Size)
		}
	}

	st := t.Stats()
	v.Downloaded = st.BytesReadUsefulData.Int64()
	v.Uploaded = st.BytesWrittenData.Int64()
	if v.Downloaded > 0 {
		v.Ratio = float64(v.Uploaded) / float64(v.Downloaded)
	}
	v.Peers = st.ActivePeers
	v.Seeds = st.ConnectedSeeders
	v.State = m.stateLocked(e)
	v.ETA = e.eta()
	return v
}

func (m *Manager) stateLocked(e *Entry) string {
	switch {
	case e.errText != "":
		return StateError
	case e.paused:
		return StatePaused
	case e.checking:
		return StateChecking
	case e.t.Info() == nil:
		return StateMetadata
	case e.t.Complete().Bool():
		if m.cfg.Seed {
			return StateSeeding
		}
		return StateCompleted
	case e.down > 0:
		return StateDownloading
	default:
		return StateStalled
	}
}

func (e *Entry) eta() int64 {
	if e.t.Info() == nil {
		return -1
	}
	size := e.t.Length()
	done := e.t.BytesCompleted()
	if size <= 0 || done >= size {
		return 0
	}
	if e.down <= 0 {
		return -1
	}
	return (size - done) / e.down
}

// Files возвращает список файлов раздачи с прогрессом.
func (m *Manager) Files(hash string) ([]FileView, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	e, ok := m.entries[hash]
	if !ok {
		return nil, ErrNotFound
	}
	if e.t.Info() == nil {
		return nil, ErrNoMetadata
	}

	files := e.t.Files()
	out := make([]FileView, 0, len(files))
	for _, f := range files {
		fv := FileView{
			Path:   f.DisplayPath(),
			Length: f.Length(),
			Done:   f.BytesCompleted(),
		}
		if fv.Length > 0 {
			fv.Progress = float64(fv.Done) / float64(fv.Length)
		}
		out = append(out, fv)
	}
	return out, nil
}

// sampleLocked считает скорости как разницу счётчиков раз в секунду.
func (m *Manager) sampleLocked() {
	for _, e := range m.entries {
		st := e.t.Stats()
		read := st.BytesReadUsefulData.Int64()
		written := st.BytesWrittenData.Int64()

		if d := read - e.lastRead; d >= 0 {
			e.down = d
		} else {
			e.down = 0
		}
		if d := written - e.lastWrite; d >= 0 {
			e.up = d
		} else {
			e.up = 0
		}
		e.lastRead, e.lastWrite = read, written

		if !m.cfg.Seed && e.t.Info() != nil && e.t.Complete().Bool() && !e.paused {
			e.t.DisallowDataUpload()
			e.up = 0
		}
	}

	cs := m.cl.Stats()
	read := cs.BytesReadUsefulData.Int64()
	written := cs.BytesWrittenData.Int64()
	if d := read - m.lastGlobalRead; d >= 0 {
		m.downSpeed = d
		m.sessionDown += d
	} else {
		m.downSpeed = 0
	}
	if d := written - m.lastGlobalWrite; d >= 0 {
		m.upSpeed = d
		m.sessionUp += d
	} else {
		m.upSpeed = 0
	}
	m.lastGlobalRead, m.lastGlobalWrite = read, written
}
