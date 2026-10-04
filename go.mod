module github.com/beexar-games/beexar-go

// The oldest Go release upstream still supports (it keeps the last two), not
// the toolchain we build with: a `go` directive above what an operator runs
// forces them to download a toolchain on every build.
go 1.26
