package keys

type VectorStoreType string

const (
	VectorStoreTypeMemory VectorStoreType = "memory"
	VectorStoreTypeQdrant VectorStoreType = "qdrant"
)
