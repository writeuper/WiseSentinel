package client

import (
	"context"
	"fmt"
	"strconv"
	"sync"
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
	mu         sync.RWMutex
	client     milvus.Client
	cfg        Config
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
		DBName:     configx.String(ctx, "milvus.db", "MILVUS_DB"),
		Collection: configx.String(ctx, "milvus.collection", "MILVUS_COLLECTION"),
		Username:   g.Cfg().MustGet(ctx, "milvus.username", "").String(),
		Password:   g.Cfg().MustGet(ctx, "milvus.password", "").String(),
	}
	if cfg.DBName == "" {
		cfg.DBName = defaultDB
	}
	if cfg.Collection == "" {
		cfg.Collection = defaultCollection
	}
	return cfg
}

// connectTimeout is the maximum time to wait for Milvus gRPC connection.
const connectTimeout = 5 * time.Second

// NewMilvusClient connects to Milvus and ensures database/collection exist.
func NewMilvusClient(ctx context.Context) (*MilvusClient, error) {
	cfg := LoadConfig(ctx)
	return newMilvusClient(ctx, cfg)
}

func newMilvusClient(ctx context.Context, cfg Config) (*MilvusClient, error) {

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
		cfg:        cfg,
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
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// Collection returns the configured collection name.
func (m *MilvusClient) Collection() string {
	return m.collection
}

// Ping verifies Milvus connectivity.
func (m *MilvusClient) Ping(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("milvus client is nil")
	}
	m.mu.RLock()
	c := m.client
	m.mu.RUnlock()
	if c == nil {
		return fmt.Errorf("milvus client is nil")
	}
	if _, err := c.ListCollections(ctx); err == nil {
		return nil
	} else if reconnectErr := m.reconnect(ctx); reconnectErr != nil {
		return fmt.Errorf("milvus ping: %w; reconnect: %v", err, reconnectErr)
	}
	return nil
}

// reconnect replaces a stale SDK connection after Milvus has restarted. The
// wrapper is shared by RAG, index and GC components, so swapping the client in
// place lets all existing components recover without rebuilding the App.
func (m *MilvusClient) reconnect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err := m.client.ListCollections(probeCtx)
		cancel()
		if err == nil {
			return nil
		}
	}
	next, err := newMilvusClient(ctx, m.cfg)
	if err != nil {
		return err
	}
	old := m.client
	m.client = next.client
	if old != nil {
		old.Close()
	}
	return nil
}

// PhysicalVectorCount returns the configured collection's complete physical
// row count. This is deliberately not tenant-scoped and includes vectors that
// may be legacy, superseded or pending GC; callers must present it separately
// from logical active chunks.
func (m *MilvusClient) PhysicalVectorCount(ctx context.Context) (int, error) {
	if m == nil || m.client == nil {
		return 0, fmt.Errorf("milvus client is nil")
	}
	statistics, err := m.client.GetCollectionStatistics(ctx, m.collection)
	if err != nil {
		return 0, err
	}
	return parsePhysicalVectorCount(statistics)
}

func parsePhysicalVectorCount(statistics map[string]string) (int, error) {
	value, ok := statistics["row_count"]
	if !ok || value == "" {
		return 0, fmt.Errorf("milvus row_count is missing")
	}
	count, err := strconv.ParseUint(value, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("parse milvus row_count: %w", err)
	}
	return int(count), nil
}

// Close releases the Milvus connection.
func (m *MilvusClient) Close() {
	if m != nil && m.client != nil {
		m.client.Close()
	}
}
