module github.com/beexar-games/beexar-go

// Deliberately below the toolchain we build with. A `go` directive above what
// an operator runs forces them to download a toolchain on every build, and this
// package uses nothing newer.
go 1.22
