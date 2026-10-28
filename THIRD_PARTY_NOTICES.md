# Third-party notices

mbtorrent is an independent, unofficial fork of qBittorrent's idea and user
experience. It does not contain qBittorrent source code.

## qBittorrent

- Project: https://www.qbittorrent.org/
- Source: https://github.com/qbittorrent/qBittorrent
- License: GPL-2.0-or-later (for the original project)
- Relation: mbtorrent is **not** affiliated with, endorsed by, or sponsored by
  the qBittorrent project or its authors. The qBittorrent name and logo belong
  to their respective owners. The name is used only to describe the project's
  origin and inspiration, as permitted by nominative fair use.

## anacrolix/torrent

- Project: https://github.com/anacrolix/torrent
- License: Mozilla Public License 2.0 (MPL-2.0)
- Usage: BitTorrent engine, linked as a Go module.
- Source for the MPL-covered code is available in this repository's module
  dependencies and at the upstream URL above. MPL-2.0 requires that the source
  of MPL-covered files remain available; it does not affect the licensing of
  mbtorrent's own files.

## Other Go dependencies

All other modules are listed in `go.mod` / `go.sum` with their respective
licenses. They are used as unmodified Go modules. The main ones include:

- `golang.org/x/sys` — BSD-3-Clause
- `golang.org/x/time` — BSD-3-Clause
- `go.etcd.io/bbolt` — MIT
- `github.com/anacrolix/dht/v2` — MPL-2.0
- `github.com/anacrolix/upnp` — MPL-2.0
- `github.com/anacrolix/go-libutp` — MIT
- `github.com/pion/*` — MIT
- `modernc.org/*` — BSD-3-Clause

## Icons and UI

- Application icon: original work generated for this project (`tools/genicon.py`).
- UI: original HTML/CSS/JS written for this project.
- No qBittorrent artwork, icons, or translations are redistributed.
