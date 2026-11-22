package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const (
	// namePrefix is the stable namespace prefix for runtime connector names.
	namePrefix = "oa"
	// topicSeparator separates tenant, connector, schema, and table in topics.
	topicSeparator = "."
)

// runtimeRefPattern matches generated runtime references. Only sink names are
// accepted as references; the source name is derived deterministically.
var runtimeRefPattern = regexp.MustCompile(`^oa-([0-9a-f]{8})-([0-9a-f]{8})-sink$`)

// Names are the deterministic runtime object names for one connector. Names only
// contain [a-z0-9.-] so they satisfy Kafka Connect naming constraints.
type Names struct {
	TenantHash    string
	ConnectorHash string
	Source        string
	Sink          string
	TopicPrefix   string
}

// Topic returns the deterministic topic for one source table.
func (n Names) Topic(schema, table string) string {
	return n.TopicPrefix + topicSeparator + schema + topicSeparator + table
}

// Topics returns the deterministic topics for a schema and its tables.
func (n Names) Topics(schema string, tables []string) []string {
	topics := make([]string, 0, len(tables))
	for _, table := range tables {
		topics = append(topics, n.Topic(schema, table))
	}
	return topics
}

// DefaultSlotName returns the deterministic Postgres replication slot name.
func (n Names) DefaultSlotName() string {
	return namePrefix + "_" + n.TenantHash + "_" + n.ConnectorHash + "_slot"
}

// DefaultPublicationName returns the deterministic Postgres publication name.
func (n Names) DefaultPublicationName() string {
	return namePrefix + "_" + n.TenantHash + "_" + n.ConnectorHash + "_pub"
}

// Naming derives deterministic runtime names from tenant and connector identifiers.
type Naming struct{}

// NewNaming builds the runtime naming strategy.
func NewNaming() Naming { return Naming{} }

// NamesFor validates both identifiers and derives their runtime namespace.
func (Naming) NamesFor(tenantID, connectorID string) (Names, error) {
	tenant, err := uuid.Parse(tenantID)
	if err != nil {
		return Names{}, fmt.Errorf("%w: tenant id must be a uuid", ErrInvalidTenant)
	}
	connector, err := uuid.Parse(connectorID)
	if err != nil {
		return Names{}, fmt.Errorf("%w: connector id must be a uuid", ErrInvalidConnector)
	}
	return namesFor(tenant.String(), connector.String()), nil
}

// NamesFromRuntimeRef validates a runtime reference against the caller tenant and
// re-derives the source name. A reference from another tenant namespace is
// rejected without touching the runtime.
func (Naming) NamesFromRuntimeRef(tenantID, runtimeRef string) (Names, error) {
	tenant, err := uuid.Parse(tenantID)
	if err != nil {
		return Names{}, fmt.Errorf("%w: tenant id must be a uuid", ErrInvalidTenant)
	}
	ref := strings.ToLower(strings.TrimSpace(runtimeRef))
	match := runtimeRefPattern.FindStringSubmatch(ref)
	if match == nil {
		return Names{}, fmt.Errorf("%w: runtime reference must be a generated sink name", ErrInvalidRuntimeRef)
	}
	tenantHash := hash8(strings.ToLower(tenant.String()))
	if match[1] != tenantHash {
		return Names{}, fmt.Errorf("%w: runtime reference does not belong to the tenant namespace", ErrInvalidRuntimeRef)
	}
	connectorHash := match[2]
	base := namePrefix + "-" + tenantHash + "-" + connectorHash
	return Names{
		TenantHash:    tenantHash,
		ConnectorHash: connectorHash,
		Source:        base + "-source",
		Sink:          base + "-sink",
		TopicPrefix:   namePrefix + topicSeparator + tenantHash + topicSeparator + connectorHash,
	}, nil
}

func namesFor(tenantID, connectorID string) Names {
	tenantHash := hash8(strings.ToLower(tenantID))
	// Hash the connector UUID instead of taking its first 32 bits: UUIDv7
	// prefixes are timestamps, so nearby connectors would otherwise collide.
	connectorHash := hash8(strings.ToLower(connectorID))
	base := namePrefix + "-" + tenantHash + "-" + connectorHash
	return Names{
		TenantHash:    tenantHash,
		ConnectorHash: connectorHash,
		Source:        base + "-source",
		Sink:          base + "-sink",
		TopicPrefix:   namePrefix + topicSeparator + tenantHash + topicSeparator + connectorHash,
	}
}

func hash8(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}
