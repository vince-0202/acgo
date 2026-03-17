package config

import "acgo/pkg/keys"

// RagSetting holds configuration for retrieval-augmented generation (RAG) features.
type RagSetting struct {
	VectorStoreType keys.VectorStoreType `mapstructure:"vector_store_type"`
	Qdrant          QdrantSetting        `mapstructure:"qdrant"`
}

// QdrantSetting describes how to connect to a Qdrant instance.
type QdrantSetting struct {
	// Host is the Qdrant host, e.g. "localhost" or "qdrant.local".
	Host string `mapstructure:"host"`
	// Port is the Qdrant HTTP port, typically 6333.
	Port int `mapstructure:"port"`
	// APIKey is the optional Qdrant API key when authentication is enabled.
	APIKey string `mapstructure:"api_key"`
	// Collection is the default collection name to use for RAG.
	Collection string `mapstructure:"collection"`
}
