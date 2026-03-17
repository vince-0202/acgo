package rag

import (
	"context"
	"strings"
)

// BuildPipelineFromArgs is a small helper for CLI commands.
// It parses comma-separated directory and extension lists and returns a ready-to-run Pipeline
// with a provided Embedder and VectorStore.
func BuildPipelineFromArgs(dirsArg, extsArg string, embedder Embedder, store VectorStore) *Pipeline {
	if embedder == nil || store == nil {
		return nil
	}
	var dataDirs []string
	for _, part := range strings.Split(dirsArg, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			dataDirs = append(dataDirs, p)
		}
	}
	if len(dataDirs) == 0 {
		return nil
	}
	var includeExts []string
	if extsArg != "" {
		for _, part := range strings.Split(extsArg, ",") {
			e := strings.TrimSpace(part)
			if e == "" {
				continue
			}
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			includeExts = append(includeExts, e)
		}
	}
	cfg := IngestConfig{
		DataDirs:        dataDirs,
		IncludeExts:     includeExts,
		ChunkSize:       2000,
		ChunkOverlap:    200,
		MaxConcurrency:  4,
		BatchSize:       32,
		SkipHiddenFiles: true,
	}
	return NewPipeline(cfg, embedder, store)
}

// RunPipeline is a thin wrapper to run a Pipeline with a background context.
func RunPipeline(p *Pipeline) error {
	if p == nil {
		return nil
	}
	return p.Run(context.Background())
}
