package rag

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"

	"acgo/pkg/config"
	"acgo/pkg/log"
)

// httpQdrantClient implements QdrantClient using the official gRPC Go client.
type httpQdrantClient struct {
	grpcClient *qdrant.Client
}

// NewVectorStoreQdrantFromConfig builds a VectorStoreQdrant using Qdrant
// connection information from config.Rag.Qdrant.
func NewVectorStoreQdrantFromConfig(cfg config.QdrantSetting) (*VectorStoreQdrant, error) {
	log.Debugf("[rag] NewVectorStoreQdrantFromConfig: host=%s port=%d collection=%s", cfg.Host, cfg.Port, cfg.Collection)
	client, err := newHTTPQdrantClient(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Collection == "" {
		return nil, fmt.Errorf("qdrant collection name is empty in config")
	}
	return NewVectorStoreQdrant(client, cfg.Collection)
}

func newHTTPQdrantClient(cfg config.QdrantSetting) (*httpQdrantClient, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("qdrant host is empty in config")
	}
	// Qdrant 默认 HTTP 端口 6333、gRPC 端口 6334。
	// 之前配置里的 Port 多半是 HTTP 端口，这里做个兼容：
	grpcPort := cfg.Port
	if grpcPort == 0 || grpcPort == 6333 {
		grpcPort = 6334
	}
	log.Debugf("[rag] Qdrant gRPC client: host=%s port=%d", cfg.Host, grpcPort)
	grpcClient, err := qdrant.NewClient(&qdrant.Config{
		Host:   cfg.Host,
		Port:   grpcPort,
		APIKey: cfg.APIKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create qdrant client: %w", err)
	}
	return &httpQdrantClient{
		grpcClient: grpcClient,
	}, nil
}

func (c *httpQdrantClient) UpsertPoints(ctx context.Context, collection string, points []QdrantPoint) error {
	if len(points) == 0 {
		return nil
	}
	log.Debugf("[rag] Qdrant UpsertPoints: collection=%s points=%d", collection, len(points))
	wait := true
	qPoints := make([]*qdrant.PointStruct, 0, len(points))
	for _, p := range points {
		vec := make([]float32, len(p.Vector))
		copy(vec, p.Vector)
		payload := qdrant.NewValueMap(p.Payload)
		qPoints = append(qPoints, &qdrant.PointStruct{
			Id:      qdrant.NewID(p.ID),
			Vectors: qdrant.NewVectors(vec...),
			Payload: payload,
		})
	}
	_, err := c.grpcClient.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: collection,
		Points:         qPoints,
		Wait:           &wait,
	})
	if err != nil {
		return fmt.Errorf("qdrant upsert: %w", err)
	}
	return nil
}

func (c *httpQdrantClient) Search(ctx context.Context, collection string, vector []float32, topK int, filters map[string]any) ([]QdrantPoint, error) {
	if topK <= 0 {
		topK = 5
	}
	log.Debugf("[rag] Qdrant Search: collection=%s topK=%d vector_dim=%d has_filter=%v", collection, topK, len(vector), filters != nil)
	limit := uint64(topK)
	vec := make([]float32, len(vector))
	copy(vec, vector)

	query := &qdrant.QueryPoints{
		CollectionName: collection,
		Query:          qdrant.NewQuery(vec...),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayload(true),
		WithVectors:    qdrant.NewWithVectors(true),
	}
	// TODO: map `filters` into qdrant.Filter if needed.

	resp, err := c.grpcClient.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("qdrant query: %w", err)
	}
	log.Debugf("[rag] Qdrant Search: collection=%s results=%d", collection, len(resp))

	out := make([]QdrantPoint, 0, len(resp))
	for _, r := range resp {
		if r == nil || r.Id == nil {
			continue
		}
		var vecData []float32
		if r.Vectors != nil && r.Vectors.GetVector() != nil {
			vecData = r.Vectors.GetVector().Data
		}
		// Payload in go-client is map[string]*qdrant.Value; for our use case we
		// only rely on the "text" field which we stored as a string.
		payload := make(map[string]any, len(r.Payload))
		for k, v := range r.Payload {
			if v == nil {
				continue
			}
			switch vv := v.Kind.(type) {
			case *qdrant.Value_StringValue:
				payload[k] = vv.StringValue
			default:
				// For non-string payload values we can extend mapping as needed.
				payload[k] = v
			}
		}
		out = append(out, QdrantPoint{
			ID:      r.Id.GetUuid(),
			Vector:  vecData,
			Payload: payload,
			Score:   r.Score,
		})
	}
	return out, nil
}

func (c *httpQdrantClient) DeleteByIDs(ctx context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	log.Debugf("[rag] Qdrant DeleteByIDs: collection=%s ids=%d", collection, len(ids))
	pointIDs := make([]*qdrant.PointId, 0, len(ids))
	for _, id := range ids {
		pointIDs = append(pointIDs, qdrant.NewID(id))
	}
	wait := true
	_, err := c.grpcClient.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: collection,
		Points:         qdrant.NewPointsSelector(pointIDs...),
		Wait:           &wait,
	})
	if err != nil {
		return fmt.Errorf("qdrant delete: %w", err)
	}
	return nil
}
