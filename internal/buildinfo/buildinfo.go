package buildinfo

// Version подставляется линкером (-X) при сборке; значение по умолчанию —
// для `go run` и отладочных сборок.
var Version = "0.1.0"

// UserAgent вычисляется после инициализации, поэтому видит версию из линкера.
var UserAgent = "mbtorrent/" + Version
