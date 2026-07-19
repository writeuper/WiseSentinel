package client

import (
	"context"
	"fmt"
	"time"

	"wisesentinel-platform/internal/pkg/configx"

	"github.com/gogf/gf/v2/frame/g"
	milvus "github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

const (
	defaultDB         = "agent"
	defaultCollection = "biz"
	VectorDim         = 2048
	vectorDim         = VectorDim
)

// MilvusClient wraps the Milvus SDK with WiseSentinel schema conventions.
type MilvusClient struct {
	client     milvus.Client
	address    string
	dbName     string
	collection string
}

// Config holds Milvus connection settings.
type Config struct {
	Address    string
	DBName     string
	Collection string
	Username   string
	Password   string
}

// LoadConfig reads Milvus settings from GoFrame config.
func LoadConfig(ctx context.Context) Config {
	address := configx.String(ctx, "milvus.address", "MILVUS_ADDRESS")
	if address == "" {
		address = "127.0.0.1:19530"
	}
	cfg := Config{
		Address:    address,
		DBName:     g.Cfg().MustGet(ctx, "milvus.db", defaultDB).String(),
		Collection: g.Cfg().MustGet(ctx, "milvus.collection", defaultCollection).String(),
		Username:   g.Cfg().MustGet(ctx, "milvus.username", "").String(),
		Password:   g.Cfg().MustGet(ctx, "milvus.password", "").String(),
	}
	return cfg
}

// connectTimeout is the maximum time to wait for Milvus gRPC connection.
const connectTimeout = 5 * time.Second

// NewMilvusClient connects to Milvus and ensures database/collection exist.
func NewMilvusClient(ctx context.Context) (*MilvusClient, error) {
	cfg := LoadConfig(ctx)

	connCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	defaultClient, err := milvus.NewClient(connCtx, milvus.Config{
		Address:  cfg.Address,
		DBName:   "default",
		Username: cfg.Username,
		Password: cfg.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("connect milvus default db: %w", err)
	}
	defer defaultClient.Close()

	if err := ensureDatabase(ctx, defaultClient, cfg.DBName); err != nil {
		return nil, err
	}

	agentClient, err := milvus.NewClient(connCtx, milvus.Config{
		Address:  cfg.Address,
		DBName:   cfg.DBName,
		Username: cfg.Username,
		Password: cfg.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("connect milvus %s db: %w", cfg.DBName, err)
	}

	mc := &MilvusClient{
		client:     agentClient,
		address:    cfg.Address,
		dbName:     cfg.DBName,
		collection: cfg.Collection,
	}

	if err := mc.ensureCollection(ctx); err != nil {
		agentClient.Close()
		return nil, err
	}

	if err := agentClient.LoadCollection(ctx, cfg.Collection, false); err != nil {
		g.Log().Warningf(ctx, "load collection %s: %v", cfg.Collection, err)
	}

	return mc, nil
}

func ensureDatabase(ctx context.Context, c milvus.Client, dbName string) error {
	databases, err := c.ListDatabases(ctx)
	if err != nil {
		return fmt.Errorf("list databases: %w", err)
	}
	for _, db := range databases {
		if db.Name == dbName {
			return nil
		}
	}
	if err := c.CreateDatabase(ctx, dbName); err != nil {
		return fmt.Errorf("create database %s: %w", dbName, err)
	}
	return nil
}

func (m *MilvusClient) ensureCollection(ctx context.Context) error {
	exists, err := m.client.HasCollection(ctx, m.collection)
	if err != nil {
		return fmt.Errorf("has collection: %w", err)
	}
	if exists {
		return nil
	}

	schema := &entity.Schema{
		CollectionName: m.collection,
		Description:    "WiseSentinel business knowledge collection",
		Fields:         collectionFields(),
	}
	if err := m.client.CreateCollection(ctx, schema, entity.DefaultShardNumber); err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	idx, err := entity.NewIndexHNSW(entity.L2, 16, 200)
	if err != nil {
		return fmt.Errorf("create hnsw index params: %w", err)
	}
	if err := m.client.CreateIndex(ctx, m.collection, "vector", idx, false); err != nil {
		return fmt.Errorf("create vector index: %w", err)
	}

	return nil
}

func collectionFields() []*entity.Field {
	return []*entity.Field{
		{
			Name:       "id",
			DataType:   entity.FieldTypeVarChar,
			TypeParams: map[string]string{"max_length": "256"},
			PrimaryKey: true,
		},
		{
			Name:       "vector",
			DataType:   entity.FieldTypeFloatVector,
			TypeParams: map[string]string{"dim": fmt.Sprintf("%d", vectorDim)},
		},
		{
			Name:       "content",
			DataType:   entity.FieldTypeVarChar,
			TypeParams: map[string]string{"max_length": "8192"},
		},
		{
			Name:     "metadata",
			DataType: entity.FieldTypeJSON,
		},
	}
}

// Client returns the underlying Milvus SDK client.
func (m *MilvusClient) Client() milvus.Client {
	return m.client
}

// Collection returns the configured collection name.
func (m *MilvusClient) Collection() string {
	return m.collection
}

// Ping verifies Milvus connectivity.
func (m *MilvusClient) Ping(ctx context.Context) error {
	if m == nil || m.client == nil {
		return fmt.Errorf("milvus client is nil")
	}
	_, err := m.client.ListCollections(ctx)
	return err
}

// Close releases the Milvus connection.
func (m *MilvusClient) Close() {
	if m != nil && m.client != nil {
		m.client.Close()
	}
}
