package ingest

import (
	"bufio"
	"context"
	"github.com/vince-0202/acgo/pkg/rag/embedder"
	"github.com/vince-0202/acgo/pkg/rag/vector"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/vince-0202/acgo/pkg/log"
)

// DocumentLoader walks one or more root directories and yields file paths
// that match the configured extensions.
type DocumentLoader struct {
	cfg IngestConfig
}

func NewDocumentLoader(cfg IngestConfig) *DocumentLoader {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 2000
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = 200
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 32
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 4
	}
	return &DocumentLoader{cfg: cfg}
}

// Load walks all configured data dirs and returns a list of file paths.
func (l *DocumentLoader) Load() ([]string, error) {
	var files []string
	seen := make(map[string]struct{})
	log.Debugf("[rag] DocumentLoader.Load: data_dirs=%v include_exts=%v skip_hidden=%v",
		l.cfg.DataDirs, l.cfg.IncludeExts, l.cfg.SkipHiddenFiles)
	for _, root := range l.cfg.DataDirs {
		if root == "" {
			continue
		}
		var count int
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				log.Debugf("[rag] DocumentLoader.WalkDir skip path=%s err=%v", path, err)
				return nil
			}
			if d.IsDir() {
				if l.cfg.SkipHiddenFiles && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if l.cfg.SkipHiddenFiles && strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if len(l.cfg.IncludeExts) > 0 {
				match := false
				for _, e := range l.cfg.IncludeExts {
					if ext == strings.ToLower(e) {
						match = true
						break
					}
				}
				if !match {
					return nil
				}
			}
			if _, ok := seen[path]; ok {
				return nil
			}
			seen[path] = struct{}{}
			files = append(files, path)
			count++
			return nil
		})
		if err != nil {
			return nil, err
		}
		log.Debugf("[rag] DocumentLoader.Load: root=%s files_found=%d total_so_far=%d", root, count, len(files))
	}
	log.Debugf("[rag] DocumentLoader.Load: total_files=%d", len(files))
	return files, nil
}

// Splitter splits raw text into overlapping chunks.
type Splitter struct {
	ChunkSize    int
	ChunkOverlap int
}

func NewSplitter(chunkSize, chunkOverlap int) *Splitter {
	if chunkSize <= 0 {
		chunkSize = 2000
	}
	if chunkOverlap < 0 {
		chunkOverlap = 200
	}
	return &Splitter{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// SplitText splits the given text into overlapping chunks, approximating tokens by rune count.
func (s *Splitter) SplitText(text string) []string {
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if len(runes) <= s.ChunkSize {
		return []string{text}
	}
	var chunks []string
	step := s.ChunkSize - s.ChunkOverlap
	if step <= 0 {
		step = s.ChunkSize
	}
	for start := 0; start < len(runes); start += step {
		end := start + s.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}

// Pipeline ties together loading, splitting, embedding, and writing to a VectorStore.
type Pipeline struct {
	cfg      IngestConfig
	loader   *DocumentLoader
	splitter *Splitter
	embedder *embedder.Wrapper
	store    vector.Store
}

func NewPipeline(cfg IngestConfig, embedder *embedder.Wrapper, store vector.Store) *Pipeline {
	return &Pipeline{
		cfg:      cfg,
		loader:   NewDocumentLoader(cfg),
		splitter: NewSplitter(cfg.ChunkSize, cfg.ChunkOverlap),
		embedder: embedder,
		store:    store,
	}
}

// Run performs a full ingest: load files, split, embed, and upsert into the vector store.
func (p *Pipeline) Run(ctx context.Context) error {
	start := time.Now()
	log.Debugf("[rag] Pipeline.Run: start batch_size=%d max_concurrency=%d chunk_size=%d chunk_overlap=%d",
		p.cfg.BatchSize, p.cfg.MaxConcurrency, p.cfg.ChunkSize, p.cfg.ChunkOverlap)

	paths, err := p.loader.Load()
	if err != nil {
		log.Debugf("[rag] Pipeline.Run: Load failed err=%v", err)
		return err
	}
	log.Debugf("[rag] Pipeline.Run: loaded %d files in %v", len(paths), time.Since(start))

	jobs := make(chan IngestJob, p.cfg.BatchSize*p.cfg.MaxConcurrency)
	var wg sync.WaitGroup
	var totalChunks int64
	var batchCount atomic.Int64

	// workers
	for i := 0; i < p.cfg.MaxConcurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			batch := make([]IngestJob, 0, p.cfg.BatchSize)
			for j := range jobs {
				batch = append(batch, j)
				if len(batch) >= p.cfg.BatchSize {
					if err := p.processBatch(ctx, batch); err != nil {
						log.Debugf("[rag] Pipeline.processBatch worker=%d err=%v", workerID, err)
					} else {
						n := batchCount.Add(1)
						log.Debugf("[rag] Pipeline.processBatch worker=%d batch_size=%d batch_num=%d", workerID, len(batch), n)
					}
					batch = batch[:0]
				}
			}
			if len(batch) > 0 {
				if err := p.processBatch(ctx, batch); err != nil {
					log.Debugf("[rag] Pipeline.processBatch worker=%d final err=%v", workerID, err)
				} else {
					n := batchCount.Add(1)
					log.Debugf("[rag] Pipeline.processBatch worker=%d final batch_size=%d batch_num=%d", workerID, len(batch), n)
				}
			}
		}(i)
	}

	for _, path := range paths {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			log.Debugf("[rag] Pipeline.Run: cancelled after %v", time.Since(start))
			return ctx.Err()
		default:
		}
		data, err := os.Open(path)
		if err != nil {
			log.Debugf("[rag] Pipeline.Run: skip open path=%s err=%v", path, err)
			continue
		}
		content, err := readAll(data)
		_ = data.Close()
		if err != nil {
			log.Debugf("[rag] Pipeline.Run: skip read path=%s err=%v", path, err)
			continue
		}
		chunks := p.splitter.SplitText(content)
		totalChunks += int64(len(chunks))
		log.Debugf("[rag] Pipeline.Run: file=%s chunks=%d runes=%d", path, len(chunks), len([]rune(content)))
		for idx, c := range chunks {
			uid := uuid.New().String()
			j := IngestJob{
				ID:   uid,
				Text: c,
				Metadata: map[string]any{
					"mem_kind":     "document",
					"file_path":    path,
					"chunk_index":  idx,
					"chunk_doc_id": path + "#" + strconv.Itoa(idx),
				},
			}
			jobs <- j
		}
	}
	close(jobs)
	wg.Wait()
	log.Debugf("[rag] Pipeline.Run: done total_files=%d total_chunks=%d batches=%d elapsed=%v",
		len(paths), totalChunks, batchCount.Load(), time.Since(start))
	return nil
}

func (p *Pipeline) processBatch(ctx context.Context, batch []IngestJob) error {
	if len(batch) == 0 {
		return nil
	}
	embedStart := time.Now()
	texts := make([]string, 0, len(batch))
	for _, j := range batch {
		texts = append(texts, j.Text)
	}
	vectors, err := p.embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		log.Debugf("[rag] Pipeline.processBatch EmbedDocuments batch_len=%d err=%v", len(batch), err)
		return err
	}
	if len(vectors) != len(batch) {
		log.Debugf("[rag] Pipeline.processBatch EmbedDocuments returned %d vectors, expected %d", len(vectors), len(batch))
		return nil
	}
	log.Debugf("[rag] Pipeline.processBatch EmbedDocuments batch_len=%d dim=%d elapsed=%v",
		len(batch), len(vectors[0]), time.Since(embedStart))

	records := make([]vector.Record, 0, len(batch))
	for i, j := range batch {
		records = append(records, vector.Record{
			ID:       j.ID,
			Vector:   vectors[i],
			Text:     j.Text,
			Metadata: j.Metadata,
		})
	}
	upsertStart := time.Now()
	err = p.store.Upsert(ctx, records)
	if err != nil {
		log.Debugf("[rag] Pipeline.processBatch Upsert records=%d err=%v", len(records), err)
		return err
	}
	log.Debugf("[rag] Pipeline.processBatch Upsert records=%d elapsed=%v", len(records), time.Since(upsertStart))
	return nil
}

func readAll(f *os.File) (string, error) {
	var b strings.Builder
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		b.WriteString(scanner.Text())
		b.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}
