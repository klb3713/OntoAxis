package domain

import (
	"fmt"
	"strings"
	"time"
)

// DatasetFormat is the storage format of a dataset. It mirrors
// dataset.v1.DatasetFormat after proto enum conversion.
type DatasetFormat string

const (
	DatasetFormatUnspecified DatasetFormat = ""
	DatasetFormatIceberg     DatasetFormat = "iceberg"
	DatasetFormatParquet     DatasetFormat = "parquet"
)

// ParseDatasetFormat validates a wire value. UNSPECIFIED values are rejected
// on writes per ARCHITECTURE 5.5.
func ParseDatasetFormat(raw string) (DatasetFormat, error) {
	switch DatasetFormat(strings.ToLower(strings.TrimSpace(raw))) {
	case DatasetFormatIceberg:
		return DatasetFormatIceberg, nil
	case DatasetFormatParquet:
		return DatasetFormatParquet, nil
	default:
		return DatasetFormatUnspecified, fmt.Errorf("%w: unsupported dataset format %q", ErrInvalidArgument, raw)
	}
}

func (f DatasetFormat) Valid() bool {
	return f == DatasetFormatIceberg || f == DatasetFormatParquet
}

// Dataset is the ontology-level metadata of an Iceberg table. The actual
// table, branches and commits live in the underlying lakehouse; this module
// only stores the catalog records.
type Dataset struct {
	ID              string
	TenantID        string
	Name            string
	DisplayName     string
	Description     string
	RID             string
	Format          DatasetFormat
	StorageLocation string
	OwnerID         string
	Tags            map[string]string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// DatasetRID builds the stable resource identifier defined in ARCHITECTURE
// 16.3: ri.ontoaxis.main.dataset.<uuid-v7>.
func DatasetRID(id string) string {
	return "ri.ontoaxis.main.dataset." + id
}

// Branch is the catalog record of a Nessie branch.
type Branch struct {
	ID                       string
	TenantID                 string
	DatasetID                string
	Name                     string
	ParentBranchID           string
	HeadTransactionID        string
	CreatedFromTransactionID string
	FallbackChain            []string
	Labels                   map[string]string
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

// TransactionType mirrors dataset.v1 transaction types. The proto does not
// currently expose the type on DatasetTransaction, but the table does.
type TransactionType string

const (
	TransactionTypeSnapshot TransactionType = "snapshot"
	TransactionTypeAppend   TransactionType = "append"
	TransactionTypeUpdate   TransactionType = "update"
	TransactionTypeDelete   TransactionType = "delete"
)

func ParseTransactionType(raw string) (TransactionType, error) {
	if strings.TrimSpace(raw) == "" {
		return TransactionTypeSnapshot, nil
	}
	switch TransactionType(strings.ToLower(strings.TrimSpace(raw))) {
	case TransactionTypeSnapshot, TransactionTypeAppend, TransactionTypeUpdate, TransactionTypeDelete:
		return TransactionType(strings.ToLower(strings.TrimSpace(raw))), nil
	default:
		return "", fmt.Errorf("%w: unsupported transaction type %q", ErrInvalidArgument, raw)
	}
}

// TransactionStatus mirrors dataset.v1.TransactionStatus.
type TransactionStatus string

const (
	TransactionStatusOpen      TransactionStatus = "open"
	TransactionStatusCommitted TransactionStatus = "committed"
	TransactionStatusAborted   TransactionStatus = "aborted"
)

// Transaction is the catalog record of a Nessie commit. Only the open state
// may transition; committed and aborted are terminal.
type Transaction struct {
	ID              string
	TenantID        string
	DatasetID       string
	BranchID        string
	TransactionType TransactionType
	Status          TransactionStatus
	Message         string
	StartedAt       time.Time
	CommittedAt     *time.Time
	Metadata        map[string]string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Commit moves an open transaction to committed and records the commit time.
func (t *Transaction) Commit(at time.Time) error {
	if t.Status != TransactionStatusOpen {
		return fmt.Errorf("%w: transaction %s is %s", ErrInvalidState, t.ID, t.Status)
	}
	t.Status = TransactionStatusCommitted
	t.CommittedAt = &at
	t.UpdatedAt = at
	return nil
}

// Abort moves an open transaction to aborted. Aborts do not move a branch head.
func (t *Transaction) Abort(at time.Time) error {
	if t.Status != TransactionStatusOpen {
		return fmt.Errorf("%w: transaction %s is %s", ErrInvalidState, t.ID, t.Status)
	}
	t.Status = TransactionStatusAborted
	t.UpdatedAt = at
	return nil
}

// View is a catalog view (a Nessie tag or SQL view) scoped to a dataset.
type View struct {
	ID        string
	TenantID  string
	DatasetID string
	BranchID  string
	Name      string
	Query     string
	Version   int32
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// File is S3 object metadata associated with a dataset. It never contains
// object bytes or credentials.
type File struct {
	ID          string
	TenantID    string
	DatasetID   string
	BranchID    string
	ObjectKey   string
	SizeBytes   int64
	RowCount    int64
	ETag        string
	ContentType string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}
