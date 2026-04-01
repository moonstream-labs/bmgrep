package cli

import (
	"encoding/json"
	"io"
)

type collectionListJSON struct {
	Collections []collectionSummaryJSON `json:"collections"`
}

type collectionSummaryJSON struct {
	Name          string `json:"name"`
	RootPath      string `json:"root_path"`
	DocumentCount int64  `json:"document_count"`
	IsDefault     bool   `json:"is_default"`
}

type collectionSourcesJSON struct {
	Collection string                 `json:"collection"`
	Sources    []collectionSourceJSON `json:"sources"`
}

type collectionSourceJSON struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	Path       string `json:"path"`
	IgnoreFile string `json:"ignore_file"`
	Enabled    bool   `json:"enabled"`
}

type dbCurrentJSON struct {
	DBPath    string `json:"db_path"`
	DBSource  string `json:"db_source"`
	Workspace string `json:"workspace"`
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
