// Package migrations embeds the goose SQL migrations for both dialects.
// The API runs them on boot (see db.Repo.Migrate), so no external migrate
// step is required. Files live in dialect subdirs: postgres/ and sqlite/.
package migrations

import "embed"

//go:embed postgres sqlite
var FS embed.FS
