package rag

import (
	"context"
	"time"
)

// DocumentChunk is one chunk of text used for retrieval.
type DocumentChunk struct {
	ID       string
	Text     string
	Score    float32
	Metadata map[string]any
}

// VectorRecord is the payload written to / read from the vector store.
type VectorRecord struct {
	ID       string
	Vector   []float32
	Text     string
	Metadata map[string]any
}

// SearchResult is a vector store search hit.
type SearchResult struct {
	Record VectorRecord
	Score  float32
}

// Embedder produces embeddings for documents and queries.
type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
	// Dim returns the embedding dimension if known, or 0 otherwise.
	Dim() int
}

// VectorStore is an abstract vector database.
type VectorStore interface {
	Upsert(ctx context.Context, records []VectorRecord) error
	Search(ctx context.Context, query []float32, topK int, filters map[string]any) ([]SearchResult, error)
	DeleteByDocID(ctx context.Context, ids []string) error
}

// Retriever turns a natural-language query into relevant chunks.
type Retriever interface {
	Retrieve(ctx context.Context, query string, topK int, filters map[string]any) ([]DocumentChunk, error)
}

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
type ingestJob struct {
	id       string
	text     string
	metadata map[string]any
}
