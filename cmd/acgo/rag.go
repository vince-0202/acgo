package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vince-0202/acgo/pkg/log"
	"github.com/vince-0202/acgo/pkg/rag/embedder"
	"github.com/vince-0202/acgo/pkg/rag/ingest"
)

// ragCmd groups RAG-related subcommands.
var ragCmd = &cobra.Command{
	Use:   "rag",
	Short: "RAG (Retrieval-Augmented Generation) utilities",
}

// ragIndexCmd builds or rebuilds a local vector index from documents.
// Example:
//
//	acgo rag index --dirs ./docs,./kb --exts .md,.txt
var ragIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index documents for RAG (load, split, embed, write to store)",
	RunE: func(cmd *cobra.Command, args []string) error {
		dirs, _ := cmd.Flags().GetString("dirs")
		exts, _ := cmd.Flags().GetString("exts")
		if dirs == "" {
			return fmt.Errorf("--dirs is required (comma-separated list of directories)")
		}
		log.Debugf("[rag] index: dirs=%s exts=%s", dirs, exts)

		emb := embedder.GetEmbedder()
		if emb == nil {
			return fmt.Errorf("no embedding provider configured (check settings)")
		}

		// Build ingest pipeline using the globally configured VectorStore.
		p := ingest.BuildPipelineFromArgs(dirs, exts, emb)
		if p == nil {
			return fmt.Errorf("failed to build ingest pipeline (check rag vector store and dirs config)")
		}
		fmt.Printf("Indexing documents from %s ...\n", dirs)
		if err := ingest.RunPipeline(p); err != nil {
			log.Debugf("[rag] index: RunPipeline err=%v", err)
			return err
		}
		log.Debugf("[rag] index: RunPipeline completed")
		fmt.Println("RAG indexing completed.")
		return nil
	},
}

func init() {
	ragIndexCmd.Flags().String("dirs", "", "Comma-separated list of root directories to index (required)")
	ragIndexCmd.Flags().String("exts", ".md,.txt", "Comma-separated list of file extensions to include (default: .md,.txt)")
	ragCmd.AddCommand(ragIndexCmd)
	rootCmd.AddCommand(ragCmd)
}
