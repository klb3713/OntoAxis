// Package service implements the connectors use cases on top of domain types
// and a storage port. It never imports generated protocol code.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
)

var (
	// ErrInvalidTenant reports a missing or malformed tenant identifier.
	ErrInvalidTenant = errors.New("invalid tenant id")
	// ErrInvalidInput reports malformed request input outside domain invariants.
	ErrInvalidInput = errors.New("invalid connector input")
	// ErrUnavailable reports that an external connector adapter is unavailable.
	ErrUnavailable = errors.New("connector adapter unavailable")
	// ErrNotImplemented reports an operation whose adapter is not configured.
	ErrNotImplemented = errors.New("connector adapter not implemented")
)

// Store is the persistence port owned by the connectors service. Implementations
// must scope every read and write to the supplied tenant.
type Store interface {
	CreateConnector(ctx context.Context, connector *domain.Connector) error
	GetConnector(ctx context.Context, tenantID, id string) (*domain.Connector, error)
	ListConnectors(ctx context.Context, tenantID string, limit, offset int32) ([]domain.Connector, int64, error)
	UpdateConnector(ctx context.Context, connector *domain.Connector) error
	SoftDeleteConnector(ctx context.Context, tenantID, id string, deletedAt time.Time) error
}

// ConnectorTester is the adapter port for connectivity checks.
type ConnectorTester interface {
	Test(ctx context.Context, tenantID string, connector *domain.Connector) (ok bool, message string, err error)
}

// SchemaInferrer is the adapter port for schema discovery.
type SchemaInferrer interface {
	InferSchema(ctx context.Context, tenantID string, connector *domain.Connector) (map[string]any, error)
}

// SyncStarter is the adapter port for creating an ingestion job. The ingestion
// module provides the production implementation; connectors never imports it.
type SyncStarter interface {
	StartSync(ctx context.Context, tenantID, connectorID string) (jobID string, err error)
}

// SyncPreflight is the adapter port for validating the CDC runtime before an
// ingestion job is created. Implementations must not submit runtime connectors or
// create jobs; they only validate configuration, credentials, and reachability.
type SyncPreflight interface {
	Preflight(ctx context.Context, tenantID string, connector *domain.Connector) error
}

// Options configures optional service dependencies and test seams.
type Option func(*serviceImpl)

// WithClock overrides the clock used for IDs' timestamps.
func WithClock(clock func() time.Time) Option {
	return func(s *serviceImpl) {
		if clock != nil {
			s.clock = clock
		}
	}
}

// WithConnectorTester injects a connectivity test adapter.
func WithConnectorTester(tester ConnectorTester) Option {
	return func(s *serviceImpl) { s.tester = tester }
}

// WithSchemaInferrer injects a schema inference adapter.
func WithSchemaInferrer(inferrer SchemaInferrer) Option {
	return func(s *serviceImpl) { s.inferrer = inferrer }
}

// WithSyncStarter injects the ingestion job starter adapter.
func WithSyncStarter(starter SyncStarter) Option {
	return func(s *serviceImpl) { s.syncStarter = starter }
}

// WithSyncPreflight injects the CDC runtime preflight adapter.
func WithSyncPreflight(preflight SyncPreflight) Option {
	return func(s *serviceImpl) { s.preflight = preflight }
}

// Service is the public connector management use case interface.
type Service interface {
	CreateConnector(ctx context.Context, tenantID string, in CreateInput) (*domain.Connector, error)
	GetConnector(ctx context.Context, tenantID, id string) (*domain.Connector, error)
	ListConnectors(ctx context.Context, tenantID string, page, perPage int32) (*ListResult, error)
	UpdateConnector(ctx context.Context, tenantID, id string, in UpdateInput) (*domain.Connector, error)
	DeleteConnector(ctx context.Context, tenantID, id string) error
	TestConnector(ctx context.Context, tenantID, id string) (*TestResult, error)
	InferSchema(ctx context.Context, tenantID, id string) (map[string]any, error)
	StartSync(ctx context.Context, tenantID, connectorID string) (string, error)
}

// serviceImpl implements Service over a Store and optional adapters.
type serviceImpl struct {
	store       Store
	clock       func() time.Time
	tester      ConnectorTester
	inferrer    SchemaInferrer
	syncStarter SyncStarter
	preflight   SyncPreflight
}

// NewService builds a connectors service. A nil store is allowed for route
// registration before module initialization and causes ErrUnavailable on use.
func NewService(store Store, opts ...Option) Service {
	s := &serviceImpl{store: store, clock: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateInput is the transport-independent input for CreateConnector.
type CreateInput struct {
	Name        string
	DisplayName string
	Type        string
	Config      map[string]any
	Tags        []string
	SecretRef   string
	CreatedBy   string
}

// UpdateInput carries optional fields; nil means "leave unchanged".
type UpdateInput struct {
	Name        *string
	DisplayName *string
	Config      *map[string]any
	Tags        *[]string
	SecretRef   *string
	Status      *string
}

// ListResult is one page of connectors.
type ListResult struct {
	Connectors []domain.Connector
	Page       int32
	PerPage    int32
	Total      int64
	TotalPages int32
}

// TestResult is the connectivity test outcome.
type TestResult struct {
	OK      bool
	Message string
}

// CreateConnector validates and persists a new active connector.
func (s *serviceImpl) CreateConnector(ctx context.Context, tenantID string, in CreateInput) (*domain.Connector, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	if err := domain.ValidateName(in.Name); err != nil {
		return nil, err
	}
	connectorType, err := domain.ParseType(in.Type)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateConfig(in.Config); err != nil {
		return nil, err
	}
	if err := domain.ValidateTags(in.Tags); err != nil {
		return nil, err
	}
	if err := domain.ValidateCreatedBy(in.CreatedBy); err != nil {
		return nil, err
	}
	now := s.clock().UTC()
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate connector id: %w", err)
	}
	connector := &domain.Connector{
		ID:          id.String(),
		TenantID:    tenantID,
		Name:        in.Name,
		DisplayName: in.DisplayName,
		Type:        connectorType,
		Config:      cloneMap(in.Config),
		Tags:        append([]string(nil), in.Tags...),
		SecretRef:   in.SecretRef,
		Status:      domain.StatusActive,
		CreatedBy:   in.CreatedBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateConnector(ctx, connector); err != nil {
		return nil, err
	}
	return connector, nil
}

// GetConnector returns one connector scoped to tenantID.
func (s *serviceImpl) GetConnector(ctx context.Context, tenantID, id string) (*domain.Connector, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	if err := validateID(id); err != nil {
		return nil, err
	}
	return s.store.GetConnector(ctx, tenantID, id)
}

// ListConnectors returns a page ordered by creation time descending.
func (s *serviceImpl) ListConnectors(ctx context.Context, tenantID string, page, perPage int32) (*ListResult, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	page, perPage = normalizePage(page, perPage)
	items, total, err := s.store.ListConnectors(ctx, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, err
	}
	totalPages := int32(0)
	if total > 0 {
		totalPages = int32((total + int64(perPage) - 1) / int64(perPage))
	}
	return &ListResult{Connectors: items, Page: page, PerPage: perPage, Total: total, TotalPages: totalPages}, nil
}

// UpdateConnector applies a partial update after validating every provided field.
func (s *serviceImpl) UpdateConnector(ctx context.Context, tenantID, id string, in UpdateInput) (*domain.Connector, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	if err := validateID(id); err != nil {
		return nil, err
	}
	connector, err := s.store.GetConnector(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		if err := domain.ValidateName(*in.Name); err != nil {
			return nil, err
		}
		connector.Name = *in.Name
	}
	if in.DisplayName != nil {
		connector.DisplayName = *in.DisplayName
	}
	if in.Config != nil {
		if err := domain.ValidateConfig(*in.Config); err != nil {
			return nil, err
		}
		connector.Config = cloneMap(*in.Config)
	}
	if in.Tags != nil {
		if err := domain.ValidateTags(*in.Tags); err != nil {
			return nil, err
		}
		connector.Tags = append([]string(nil), (*in.Tags)...)
	}
	if in.SecretRef != nil {
		connector.SecretRef = *in.SecretRef
	}
	if in.Status != nil {
		status, err := domain.ParseStatus(*in.Status)
		if err != nil {
			return nil, err
		}
		connector.Status = status
	}
	now := s.clock().UTC()
	if !now.After(connector.CreatedAt) {
		now = connector.CreatedAt.Add(time.Millisecond)
	}
	connector.UpdatedAt = now
	if err := s.store.UpdateConnector(ctx, connector); err != nil {
		return nil, err
	}
	return connector, nil
}

// DeleteConnector soft-deletes a connector within tenantID.
func (s *serviceImpl) DeleteConnector(ctx context.Context, tenantID, id string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if err := validateTenant(tenantID); err != nil {
		return err
	}
	if err := validateID(id); err != nil {
		return err
	}
	return s.store.SoftDeleteConnector(ctx, tenantID, id, s.clock().UTC())
}

// TestConnector runs the injected connectivity adapter.
func (s *serviceImpl) TestConnector(ctx context.Context, tenantID, id string) (*TestResult, error) {
	connector, err := s.GetConnector(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if s.tester == nil {
		return nil, fmt.Errorf("%w: connector tester is not configured", ErrUnavailable)
	}
	ok, message, err := s.tester.Test(ctx, tenantID, connector)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return &TestResult{OK: ok, Message: message}, nil
}

// InferSchema runs the injected schema inference adapter.
func (s *serviceImpl) InferSchema(ctx context.Context, tenantID, id string) (map[string]any, error) {
	connector, err := s.GetConnector(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if s.inferrer == nil {
		return nil, fmt.Errorf("%w: schema inferrer is not configured", ErrNotImplemented)
	}
	schema, err := s.inferrer.InferSchema(ctx, tenantID, connector)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if schema == nil {
		return nil, fmt.Errorf("%w: schema inferrer returned nil", ErrUnavailable)
	}
	return schema, nil
}

// StartSync preflights the CDC runtime, then asks the ingestion adapter to create
// a sync job for an active connector. Preflight failures never create a job.
func (s *serviceImpl) StartSync(ctx context.Context, tenantID, connectorID string) (string, error) {
	connector, err := s.GetConnector(ctx, tenantID, connectorID)
	if err != nil {
		return "", err
	}
	if connector.Status != domain.StatusActive {
		return "", fmt.Errorf("%w: connector %s is %s", domain.ErrConflict, connector.ID, connector.Status)
	}
	if s.syncStarter == nil {
		return "", fmt.Errorf("%w: sync starter is not configured", ErrUnavailable)
	}
	if s.preflight != nil {
		if err := s.preflight.Preflight(ctx, tenantID, connector); err != nil {
			return "", classifyPreflightError(err)
		}
	}
	jobID, err := s.syncStarter.StartSync(ctx, tenantID, connectorID)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if jobID == "" {
		return "", fmt.Errorf("%w: sync starter returned an empty job id", ErrUnavailable)
	}
	return jobID, nil
}

// classifyPreflightError keeps parameter and conflict errors distinguishable for
// the transport layer and reports every other preflight failure as unavailable.
func classifyPreflightError(err error) error {
	if errors.Is(err, ErrInvalidInput) || errors.Is(err, domain.ErrConflict) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func (s *serviceImpl) ready() error {
	if s == nil || s.store == nil {
		return fmt.Errorf("%w: connectors store is not configured", ErrUnavailable)
	}
	return nil
}

func validateTenant(tenantID string) error {
	if tenantID == "" {
		return fmt.Errorf("%w: tenant id is empty", ErrInvalidTenant)
	}
	if _, err := uuid.Parse(tenantID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTenant, err)
	}
	return nil
}

func validateID(id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return fmt.Errorf("%w: invalid connector id %q", ErrInvalidInput, id)
	}
	return nil
}

func normalizePage(page, perPage int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
