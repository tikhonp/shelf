// Package migrations holds the platform's own tables. Tables without a module
// prefix belong to the platform.
package migrations

import "io/fs"

// FS is the platform's goose migrations. It stays nil until the platform has
// a table, because go:embed refuses a pattern that matches no file.
var FS fs.FS
