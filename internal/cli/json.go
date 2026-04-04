package cli

import (
	"encoding/json"
	"io"
)

type collectionListJSON struct {
	CWD         string                  `json:"cwd"`
	Collections []collectionSummaryJSON `json:"collections"`
}

type collectionSummaryJSON struct {
	Name          string `json:"name"`
	RootPath      string `json:"root_path"`
	DocumentCount int64  `json:"document_count"`
	IsDefault     bool   `json:"is_default"`
}

type collectionSourcesJSON struct {
	CWD        string                 `json:"cwd"`
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
	CWD       string `json:"cwd"`
	DBPath    string `json:"db_path"`
	DBSource  string `json:"db_source"`
	Workspace string `json:"workspace"`
}

type dbSourcesJSON struct {
	CWD       string              `json:"cwd"`
	DBPath    string              `json:"db_path"`
	DBSource  string              `json:"db_source"`
	Workspace string              `json:"workspace"`
	Filters   dbSourcesFilterJSON `json:"filters"`
	Sources   []dbSourceEntryJSON `json:"sources"`
}

type dbSourcesFilterJSON struct {
	Collection   string `json:"collection,omitempty"`
	Type         string `json:"type,omitempty"`
	EnabledOnly  bool   `json:"enabled_only"`
	DisabledOnly bool   `json:"disabled_only"`
	PathPrefix   string `json:"path_prefix,omitempty"`
	Sort         string `json:"sort"`
	Desc         bool   `json:"desc"`
	WithStats    bool   `json:"with_stats"`
}

type dbSourceEntryJSON struct {
	SourceID        int64  `json:"source_id"`
	CollectionID    int64  `json:"collection_id"`
	Collection      string `json:"collection"`
	Type            string `json:"type"`
	Path            string `json:"path"`
	IgnoreFile      string `json:"ignore_file"`
	Enabled         bool   `json:"enabled"`
	AddedAt         string `json:"added_at"`
	UpdatedAt       string `json:"updated_at"`
	IndexedDocs     *int64 `json:"indexed_docs,omitempty"`
	LatestIndexedAt string `json:"latest_indexed_at,omitempty"`
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
