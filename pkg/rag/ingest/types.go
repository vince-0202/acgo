package ingest

import "time"

// IngestConfig controls how documents are ingested.
type IngestConfig struct {
	DataDirs        []string  // root directories to scan
	IncludeExts     []string  // file extensions to include (e.g. .md, .txt)
	ChunkSize       int       // approximate characters per chunk
	ChunkOverlap    int       // overlapping characters between chunks
	MaxConcurrency  int       // max parallel embedding jobs
	BatchSize       int       // number of chunks per embedding batch
	ModifiedSince   time.Time // only index files modified since this time (zero for all)
	SkipHiddenFiles bool      // whether to skip dotfiles and hidden paths
}

// ingestJob is the internal representation of one text chunk to embed.
type IngestJob struct {
	ID       string
	Text     string
	Metadata map[string]any
}

// DocumentChunk is one chunk of text used for retrieval.
type DocumentChunk struct {
	ID       string
	Text     string
	Score    float32
	Metadata map[string]any
}
