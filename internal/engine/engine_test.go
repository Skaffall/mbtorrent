package engine

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"mbtorrent/internal/config"
)

// TestDownloadFromLocalPeer проверяет полный путь: .torrent → движок →
// скачивание у сидера → файл на диске. Без интернета и трекеров.
func TestDownloadFromLocalPeer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	seedDir := t.TempDir()
	payload := bytes.Repeat([]byte("mbtorrent local peer test\n"), 4000)
	src := filepath.Join(seedDir, "payload.bin")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	info := metainfo.Info{PieceLength: 32 << 10}
	if err := info.BuildFromFilePath(src); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var file bytes.Buffer
	if err := mi.Write(&file); err != nil {
		t.Fatal(err)
	}
	torrentBytes := file.Bytes()

	// Сидер: отдельный torrent-клиент с готовыми данными.
	scfg := torrent.NewDefaultClientConfig()
	scfg.DataDir = seedDir
	scfg.ListenPort = 0
	scfg.NoDefaultPortForwarding = true
	scfg.Seed = true
	scfg.Slogger = slog.New(slog.NewTextHandler(io.Discard, nil))
	seeder, err := torrent.NewClient(scfg)
	if err != nil {
		t.Fatal(err)
	}
	defer seeder.Close()

	st, err := seeder.AddTorrent(&mi)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.VerifyData(); err != nil {
		t.Fatal(err)
	}
	<-st.GotInfo()

	// Личер: наш менеджер.
	cfg := config.Default()
	cfg.DownloadDir = t.TempDir()
	cfg.ListenPort = 0
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	hash, err := m.AddTorrentBytes(torrentBytes, false)
	if err != nil {
		t.Fatal(err)
	}

	m.mu.RLock()
	entry := m.entries[hash]
	m.mu.RUnlock()
	if entry == nil {
		t.Fatal("раздача не появилась в списке")
	}

	if n := entry.t.AddClientPeer(seeder); n == 0 {
		t.Fatal("не удалось соединить клиентов")
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if entry.t.Complete().Bool() {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !entry.t.Complete().Bool() {
		t.Fatalf("не скачалось: %d из %d", entry.t.BytesCompleted(), entry.t.Length())
	}

	got, err := os.ReadFile(filepath.Join(cfg.DownloadDir, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("содержимое файла не совпало с исходным")
	}

	// Состояние в снапшоте должно быть согласованным.
	snap := m.Snapshot()
	if len(snap.Torrents) != 1 {
		t.Fatalf("ожидалась 1 раздача, получено %d", len(snap.Torrents))
	}
	if snap.Torrents[0].Progress < 0.999 {
		t.Fatalf("прогресс %v", snap.Torrents[0].Progress)
	}
}
