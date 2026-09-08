module github.com/mbauer83/effect-golang-sql

go 1.27.0

require (
	// The three drivers the generated schema is checked against, used only by
	// the tests. sqlite runs everywhere these tests run, so the derivation is
	// established with it; the other two are what say the statements are ones
	// Postgres and MySQL accept. Nothing in the module imports any of them,
	// because sql depends on a port and not on a driver.
	github.com/go-sql-driver/mysql v1.10.1
	github.com/jackc/pgx/v5 v5.11.0
	github.com/mbauer83/effect-golang v0.1.0
	github.com/mbauer83/effect-golang-schema v0.1.0
	modernc.org/sqlite v1.58.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// Every module of effect-golang is versioned together and released in
// dependency order, so a version here is a version that exists. While several
// are being worked on at once, the go.work above this directory resolves them
// to the working copies beside each other -- which is what a workspace is for,
// and what a `replace` was being misused for before: a replace is ignored by
// anything that depends on the module carrying it, so it said nothing to a
// consumer and only ever described one person's layout.
