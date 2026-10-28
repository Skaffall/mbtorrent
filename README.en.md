# mbtorrent

**An unofficial qBittorrent fork for Windows: one `.exe`, no Qt, no installer, no dependencies.**

[![Release](https://img.shields.io/github/v/release/Skaffall/mbtorrent?color=2f81f7&label=release)](https://github.com/Skaffall/mbtorrent/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-2ea043.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%2F11-0078d4.svg)](#download)
[![Go](https://img.shields.io/badge/Go-1.24-00add8.svg?logo=go&logoColor=white)](https://go.dev)
[![No Qt](https://img.shields.io/badge/Qt-not%20required-41cd52.svg)](#why-no-qt)

[Русская версия](README.md) · [Download](https://github.com/Skaffall/mbtorrent/releases/latest) · [Build](#build) · [HTTP API](#http-api)

![mbtorrent UI](docs/preview.png)

---

> ## 🧩 This is a qBittorrent fork
>
> **mbtorrent is an unofficial fork of qBittorrent.** It keeps what people love about
> qBittorrent — a clean torrent list, sensible settings, keyboard shortcuts and a
> client that stays out of the way — but it is a fork of the **idea and UX**, not of
> the code. The original qBittorrent is C++/Qt; mbtorrent is an independent Go
> reimplementation with an HTML/CSS/JS UI.
>
> * We are **not affiliated** with the qBittorrent project or its authors.
> * qBittorrent is a project by [qbittorrent.org](https://www.qbittorrent.org/) and
>   its contributors; all rights to the name and original code belong to them.
> * No qBittorrent code is used here. The engine is
>   [`anacrolix/torrent`](https://github.com/anacrolix/torrent) (MPL-2.0).
>
> Need every qBittorrent feature? Get the original
> [here](https://www.qbittorrent.org/download). Need a 20 MB client that starts with
> a double click and ships no Qt DLLs? That is mbtorrent.

## Download

1. Open the [latest release](https://github.com/Skaffall/mbtorrent/releases/latest).
2. Grab `mbtorrent.exe` (x64) or `mbtorrent-arm64.exe`.
3. Run it. Your browser opens the panel at `http://127.0.0.1:8642`.

No installation: put the file anywhere. The client keeps running in the background
until you press “Quit” in the panel.

> Windows SmartScreen may warn about an unknown publisher: the binary is built from
> this repository's sources and is not signed with a paid certificate. You can
> [build it yourself](#build) with one command.

## What it is

```
mbtorrent.exe   ≈ 21 MB   engine + UI + icons
```

A lightweight BitTorrent client for Windows only. A Go engine plus a local web UI
embedded into the binary with `go:embed`. No DLLs, installers, Electron, .NET or Qt.

| | qBittorrent | mbtorrent |
|---|---|---|
| UI | Qt6 Widgets | HTML/CSS/JS in the browser |
| Distribution | tens of MB + Qt DLLs | a single `.exe`, ≈ 21 MB |
| Installation | installer | none |
| Engine | libtorrent-rasterbar (C++) | `anacrolix/torrent` (Go) |
| Platforms | Windows, Linux, macOS | Windows 10/11 (x64, ARM64) |
| Features | everything | the essentials |

## Features

- magnet links, `.torrent` files and http(s) links to `.torrent`
- DHT, PEX, trackers, header encryption (MSE), uTP/TCP
- pause/resume, recheck, remove with or without data
- speed limits, change download folder and ports on the fly
- progress survives restarts **without rehashing**
- dark and light themes, search, filters, drag & drop, torrent details with file list
- fully offline UI (no CDN requests)
- single instance: launching again just opens the running panel

## Why no Qt

qBittorrent is C++/Qt Widgets: the entire UI, dialogs and tray are Qt6. You cannot
build it “without heavy Qt6 widgets” — you can only drop the UI entirely (that is
`qbittorrent-nox`, which has no official Windows build, and it still needs
QtCore/QtNetwork/QtSql).

So mbtorrent takes a different route:

| Usually heavy | Replaced with |
|---|---|
| Qt6 Widgets (~80–150 MB of DLLs) | HTML/CSS/JS embedded with `go:embed` |
| libtorrent-rasterbar (C++, Boost) | [`anacrolix/torrent`](https://github.com/anacrolix/torrent) — pure Go, no cgo |
| Installer and runtimes | one static `.exe`, `CGO_ENABLED=0` |
| Tray and native GUI | the default browser (Edge/WebView2 is already on Windows 10/11) |
| Rehashing on startup | a bolt database of completed pieces (`%AppData%\mbtorrent\resume`) |
| A second client instance | ping our own port → just open the panel |

The layout and shortcuts are inspired by qBittorrent, but all code is original.

## Build

All you need is [Go](https://go.dev/dl/) 1.24+.

**Windows (PowerShell):**

```powershell
.\build.ps1
# → dist\mbtorrent.exe
```

**Linux/macOS (cross-compile, amd64 + arm64):**

```bash
./build.sh
# → dist/mbtorrent.exe  (amd64) and dist/mbtorrent-arm64.exe
```

Build flags: `-H=windowsgui` (no console window), `-s -w` (smaller binary).
The icon is embedded via `.syso` files, which is why `rsrc_windows_*.syso` is in the repo.

## Run

Double-click `mbtorrent.exe` → the browser opens the panel. Launching it again does
not start a second client; it just opens the running panel. The app stays in the
background until you press “Quit” (closing the browser tab does not stop it).

> The BitTorrent port defaults to automatic (`0` in settings). If you need port
> forwarding on your router, set a fixed port such as 51413.

CLI flags:

```text
-port 8642      panel port (default 8642)
-no-browser     do not open the browser
-debug          verbose log to console
-version        print version
```

## Test

```bash
CGO_ENABLED=0 go test ./...
```

The test spins up two clients in one process (seeder and leecher), transfers a file
over the real BitTorrent protocol and compares the bytes. No internet required.

## Keyboard shortcuts

| Key | Action |
|---|---|
| `Ctrl+N` | add torrent |
| `/` | search |
| `Ctrl+V` | paste a magnet from the clipboard and open the add dialog |
| `Esc` | close dialog/panel |

## Data location

Everything lives in `%AppData%\mbtorrent`:

```
config.json        settings
state.json         torrent list (magnet, .torrent, paused)
torrents\*.torrent saved metadata
resume\            completed-piece database (do not delete — otherwise rehashing)
mbtorrent.log      log (rotated at 5 MB)
```

Default downloads: `%UserProfile%\Downloads\mbtorrent`.

## HTTP API

The UI talks to the engine over a local JSON API, which your scripts can use too:

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/state` | torrents, speeds, settings |
| GET | `/api/ping` | “is this mbtorrent?” |
| POST | `/api/torrents` | `{magnet\|url\|torrent(base64), paused}` |
| POST | `/api/torrents/{hash}/pause` | pause |
| POST | `/api/torrents/{hash}/resume` | resume |
| POST | `/api/torrents/{hash}/recheck` | recheck files |
| POST | `/api/torrents/{hash}/reveal` | open the folder in Explorer |
| POST | `/api/torrents/{hash}/remove?delete_data=1` | remove |
| GET | `/api/torrents/{hash}/files` | torrent files |
| GET/PUT | `/api/settings` | settings |
| POST | `/api/quit` | shut down cleanly |

The server binds to `127.0.0.1` only. Mutating requests require an
`X-MBTorrent: 1` header and `Content-Type: application/json`, which blocks CSRF from
other sites. The panel port is changed in settings and applied after a restart.

## Limitations in v0.1

- no tray icon or autostart (the GUI build simply stays in the background);
- no RSS, tracker search, sequential download or per-file selection;
- no proxy or IP filters;
- a single settings profile, no categories or tags;
- DHT is always on; there is no separate toggle.

These are the next steps; the architecture is ready for them.

## Project layout

```
main.go                  startup, single-instance, logs, browser
internal/config          settings and paths
internal/engine          engine, state, resume, speeds
internal/api             JSON API, security, static files
internal/platform        Win32: browser, Explorer, free space, MessageBox
web/assets               index.html, style.css, app.js (embedded into the exe)
```

## License and credits

- mbtorrent code — [MIT](LICENSE).
- [`anacrolix/torrent`](https://github.com/anacrolix/torrent) engine — MPL-2.0.
- Inspiration, UX and naming reference — [qBittorrent](https://www.qbittorrent.org/).
  mbtorrent is not affiliated with the qBittorrent project; see
  [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

If this project is useful to you, give it a ⭐ — it is the best way to help it get noticed.
