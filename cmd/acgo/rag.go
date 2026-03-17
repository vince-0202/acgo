package main

import (
	"fmt"

	"acgo/pkg/config"
	"acgo/pkg/keys"
	"acgo/pkg/llm/openai"
	"acgo/pkg/log"
	"acgo/pkg/rag"

	"github.com/spf13/cobra"
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

		settings, err := config.LoadSettings()
		if err != nil {
			return err
		}
		if len(settings.Agent.Providers) == 0 || settings.Agent.Providers[0] == nil {
			return fmt.Errorf("no providers configured in settings")
		}

		// 从 default_embedding_provider 和 default_embedding_model 获取 embedding 配置
		embProvider := settings.Agent.DefaultEmbeddingProvider
		if embProvider == "" {
			embProvider = settings.Agent.DefaultProvider
		}
		provider := config.FindProviderSetting(settings.Agent.Providers, embProvider)
		if provider == nil {
			provider = settings.Agent.Providers[0]
		}

		embedModel := settings.Agent.DefaultEmbeddingModel
		if embedModel == "" {
			embedModel = keys.GetDefaultEmbeddingModel(provider.Provider)
			if embedModel == "" {
				embedModel = settings.Agent.DefaultModel
			}
		}

		embClient := openai.NewEmbeddingClient(provider.BaseURL, provider.ApiKey, embedModel, nil)
		embedder := rag.NewOpenAIEmbedder(embClient)

		// Choose vector store implementation based on RagSetting.VectorStoreType.
		var store rag.VectorStore
		log.Debugf("[rag] index: vector_store_type=%s", settings.Rag.VectorStoreType)
		switch settings.Rag.VectorStoreType {
		case keys.VectorStoreTypeQdrant:
			s, err := rag.NewVectorStoreQdrantFromConfig(settings.Rag.Qdrant)
			if err != nil {
				return fmt.Errorf("init qdrant vector store: %w", err)
			}
			store = s
			log.Debugf("[rag] index: using Qdrant host=%s port=%d collection=%s",
				settings.Rag.Qdrant.Host, settings.Rag.Qdrant.Port, settings.Rag.Qdrant.Collection)
			fmt.Printf("Using Qdrant vector store: host=%s port=%d collection=%s\n",
				settings.Rag.Qdrant.Host, settings.Rag.Qdrant.Port, settings.Rag.Qdrant.Collection)
		case "", keys.VectorStoreTypeMemory:
			fallthrough
		default:
			store = rag.NewInMemoryVectorStore()
			log.Debugf("[rag] index: using in-memory vector store")
			fmt.Println("Using in-memory vector store (rag.vector_store_type=memory or empty).")
		}

		p := rag.BuildPipelineFromArgs(dirs, exts, embedder, store)
		if p == nil {
			return fmt.Errorf("no valid directories provided for indexing")
		}
		fmt.Printf("Indexing documents from %s ...\n", dirs)
		if err := rag.RunPipeline(p); err != nil {
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
