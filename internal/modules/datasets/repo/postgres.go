package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

// PostgresStore is the production Repository implementation. Every query
// carries tenant_id explicitly; RLS is a second line of defense, not a
// substitute for scoping in SQL. Each operation runs in a transaction whose
// local app.tenant_id setting activates the dataset RLS policies.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return domain.ErrUnavailable
	}
	return s.pool.Ping(ctx)
}

// withTenantTx runs fn inside a transaction whose local app.tenant_id setting
// drives the row-level security policies of the dataset schema.
func (s *PostgresStore) withTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	if s == nil || s.pool == nil {
		return domain.ErrUnavailable
	}
	if strings.TrimSpace(tenantID) == "" {
		return domain.ErrTenantRequired
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tenant transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("set app.tenant_id: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant transaction: %w", err)
	}
	return nil
}

const datasetColumns = `id::text, tenant_id::text, name, display_name, description, rid, format, storage_location, COALESCE(owner_id::text, ''), tags, created_at, updated_at, deleted_at`

const branchColumns = `id::text, tenant_id::text, dataset_id::text, name, COALESCE(parent_branch_id::text, ''), COALESCE(head_transaction_id::text, ''), COALESCE(created_from_transaction_id::text, ''), fallback_chain, labels, created_at, updated_at`

const transactionColumns = `id::text, tenant_id::text, dataset_id::text, branch_id::text, transaction_type, status, message, started_at, committed_at, metadata, created_at, updated_at`

const viewColumns = `id::text, tenant_id::text, dataset_id::text, COALESCE(branch_id::text, ''), name, query, version, created_at, updated_at, deleted_at`

const fileColumns = `id::text, tenant_id::text, dataset_id::text, COALESCE(branch_id::text, ''), object_key, size_bytes, row_count, etag, content_type, created_at, updated_at, deleted_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDataset(row rowScanner) (domain.Dataset, error) {
	var dataset domain.Dataset
	var format string
	var tags []byte
	err := row.Scan(
		&dataset.ID, &dataset.TenantID, &dataset.Name, &dataset.DisplayName, &dataset.Description,
		&dataset.RID, &format, &dataset.StorageLocation, &dataset.OwnerID, &tags,
		&dataset.CreatedAt, &dataset.UpdatedAt, &dataset.DeletedAt,
	)
	if err != nil {
		return domain.Dataset{}, err
	}
	dataset.Format = domain.DatasetFormat(format)
	dataset.Tags = unmarshalStringMap(tags)
	return dataset, nil
}

func scanBranch(row rowScanner) (domain.Branch, error) {
	var branch domain.Branch
	var fallbackChain, labels []byte
	err := row.Scan(
		&branch.ID, &branch.TenantID, &branch.DatasetID, &branch.Name, &branch.ParentBranchID,
		&branch.HeadTransactionID, &branch.CreatedFromTransactionID, &fallbackChain, &labels,
		&branch.CreatedAt, &branch.UpdatedAt,
	)
	if err != nil {
		return domain.Branch{}, err
	}
	branch.FallbackChain = unmarshalStringSlice(fallbackChain)
	branch.Labels = unmarshalStringMap(labels)
	return branch, nil
}

func scanTransaction(row rowScanner) (domain.Transaction, error) {
	var transaction domain.Transaction
	var transactionType, status string
	var metadata []byte
	err := row.Scan(
		&transaction.ID, &transaction.TenantID, &transaction.DatasetID, &transaction.BranchID,
		&transactionType, &status, &transaction.Message, &transaction.StartedAt,
		&transaction.CommittedAt, &metadata, &transaction.CreatedAt, &transaction.UpdatedAt,
	)
	if err != nil {
		return domain.Transaction{}, err
	}
	transaction.TransactionType = domain.TransactionType(transactionType)
	transaction.Status = domain.TransactionStatus(status)
	transaction.Metadata = unmarshalStringMap(metadata)
	return transaction, nil
}

func scanView(row rowScanner) (domain.View, error) {
	var view domain.View
	err := row.Scan(
		&view.ID, &view.TenantID, &view.DatasetID, &view.BranchID, &view.Name,
		&view.Query, &view.Version, &view.CreatedAt, &view.UpdatedAt, &view.DeletedAt,
	)
	if err != nil {
		return domain.View{}, err
	}
	return view, nil
}

func scanFile(row rowScanner) (domain.File, error) {
	var file domain.File
	err := row.Scan(
		&file.ID, &file.TenantID, &file.DatasetID, &file.BranchID, &file.ObjectKey,
		&file.SizeBytes, &file.RowCount, &file.ETag, &file.ContentType,
		&file.CreatedAt, &file.UpdatedAt, &file.DeletedAt,
	)
	if err != nil {
		return domain.File{}, err
	}
	return file, nil
}

func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return domain.ErrAlreadyExists
		case "23503":
			return domain.ErrNotFound
		}
	}
	return err
}

func marshalStringMap(value map[string]string) ([]byte, error) {
	if value == nil {
		value = map[string]string{}
	}
	return json.Marshal(value)
}

func marshalStringSlice(value []string) ([]byte, error) {
	if value == nil {
		value = []string{}
	}
	return json.Marshal(value)
}

func unmarshalStringMap(raw []byte) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	value := map[string]string{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]string{}
	}
	return value
}

func unmarshalStringSlice(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	value := []string{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return []string{}
	}
	return value
}

func (s *PostgresStore) CreateDataset(ctx context.Context, dataset domain.Dataset, mainBranch domain.Branch) error {
	tags, err := marshalStringMap(dataset.Tags)
	if err != nil {
		return fmt.Errorf("marshal dataset tags: %w", err)
	}
	fallbackChain, err := marshalStringSlice(mainBranch.FallbackChain)
	if err != nil {
		return fmt.Errorf("marshal branch fallback chain: %w", err)
	}
	labels, err := marshalStringMap(mainBranch.Labels)
	if err != nil {
		return fmt.Errorf("marshal branch labels: %w", err)
	}

	return s.withTenantTx(ctx, dataset.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO dataset.datasets
				(id, tenant_id, name, display_name, description, rid, format, storage_location, owner_id, tags, created_at, updated_at)
			VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid, $10::jsonb, $11, $12)`,
			dataset.ID, dataset.TenantID, dataset.Name, dataset.DisplayName, dataset.Description,
			dataset.RID, string(dataset.Format), dataset.StorageLocation, dataset.OwnerID, tags,
			dataset.CreatedAt, dataset.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert dataset: %w", mapWriteError(err))
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO dataset.dataset_branches
				(id, tenant_id, dataset_id, name, parent_branch_id, head_transaction_id, created_from_transaction_id, fallback_chain, labels, created_at, updated_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, $8::jsonb, $9::jsonb, $10, $11)`,
			mainBranch.ID, mainBranch.TenantID, mainBranch.DatasetID, mainBranch.Name,
			mainBranch.ParentBranchID, mainBranch.HeadTransactionID, mainBranch.CreatedFromTransactionID,
			fallbackChain, labels, mainBranch.CreatedAt, mainBranch.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert main branch: %w", mapWriteError(err))
		}
		return nil
	})
}

func (s *PostgresStore) GetDataset(ctx context.Context, tenantID, datasetID string) (domain.Dataset, error) {
	var dataset domain.Dataset
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		dataset, err = scanDataset(tx.QueryRow(ctx, `
			SELECT `+datasetColumns+` FROM dataset.datasets
			WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL`, tenantID, datasetID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get dataset: %w", err)
		}
		return nil
	})
	return dataset, err
}

func (s *PostgresStore) ListDatasets(ctx context.Context, tenantID string, offset, limit int) ([]domain.Dataset, int64, error) {
	var total int64
	items := make([]domain.Dataset, 0)
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM dataset.datasets
			WHERE tenant_id = $1::uuid AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
			return fmt.Errorf("count datasets: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT `+datasetColumns+` FROM dataset.datasets
			WHERE tenant_id = $1::uuid AND deleted_at IS NULL
			ORDER BY created_at DESC, id DESC
			LIMIT $2 OFFSET $3`, tenantID, limit, offset)
		if err != nil {
			return fmt.Errorf("list datasets: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			dataset, err := scanDataset(rows)
			if err != nil {
				return fmt.Errorf("scan dataset: %w", err)
			}
			items = append(items, dataset)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate datasets: %w", err)
		}
		return nil
	})
	return items, total, err
}

func (s *PostgresStore) UpdateDataset(ctx context.Context, dataset domain.Dataset) error {
	var affected int64
	err := s.withTenantTx(ctx, dataset.TenantID, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			UPDATE dataset.datasets
			SET display_name = $3, description = $4, updated_at = $5
			WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL`,
			dataset.TenantID, dataset.ID, dataset.DisplayName, dataset.Description, dataset.UpdatedAt)
		if err != nil {
			return fmt.Errorf("update dataset: %w", mapWriteError(err))
		}
		affected = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteDataset(ctx context.Context, tenantID, datasetID string, deletedAt time.Time) error {
	var affected int64
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			UPDATE dataset.datasets
			SET deleted_at = $3, updated_at = $3
			WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL`, tenantID, datasetID, deletedAt)
		if err != nil {
			return fmt.Errorf("delete dataset: %w", err)
		}
		affected = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateBranch(ctx context.Context, branch domain.Branch) error {
	fallbackChain, err := marshalStringSlice(branch.FallbackChain)
	if err != nil {
		return fmt.Errorf("marshal branch fallback chain: %w", err)
	}
	labels, err := marshalStringMap(branch.Labels)
	if err != nil {
		return fmt.Errorf("marshal branch labels: %w", err)
	}
	return s.withTenantTx(ctx, branch.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO dataset.dataset_branches
				(id, tenant_id, dataset_id, name, parent_branch_id, head_transaction_id, created_from_transaction_id, fallback_chain, labels, created_at, updated_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, $8::jsonb, $9::jsonb, $10, $11)`,
			branch.ID, branch.TenantID, branch.DatasetID, branch.Name, branch.ParentBranchID,
			branch.HeadTransactionID, branch.CreatedFromTransactionID, fallbackChain, labels,
			branch.CreatedAt, branch.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert branch: %w", mapWriteError(err))
		}
		return nil
	})
}

func (s *PostgresStore) GetBranch(ctx context.Context, tenantID, datasetID, branchID string) (domain.Branch, error) {
	var branch domain.Branch
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		branch, err = scanBranch(tx.QueryRow(ctx, `
			SELECT `+branchColumns+` FROM dataset.dataset_branches
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid`, tenantID, datasetID, branchID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get branch: %w", err)
		}
		return nil
	})
	return branch, err
}

func (s *PostgresStore) ListBranches(ctx context.Context, tenantID, datasetID string) ([]domain.Branch, error) {
	items := make([]domain.Branch, 0)
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+branchColumns+` FROM dataset.dataset_branches
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid
			ORDER BY created_at ASC, id ASC`, tenantID, datasetID)
		if err != nil {
			return fmt.Errorf("list branches: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			branch, err := scanBranch(rows)
			if err != nil {
				return fmt.Errorf("scan branch: %w", err)
			}
			items = append(items, branch)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate branches: %w", err)
		}
		return nil
	})
	return items, err
}

func (s *PostgresStore) CreateTransaction(ctx context.Context, transaction domain.Transaction) error {
	metadata, err := marshalStringMap(transaction.Metadata)
	if err != nil {
		return fmt.Errorf("marshal transaction metadata: %w", err)
	}
	return s.withTenantTx(ctx, transaction.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO dataset.dataset_transactions
				(id, tenant_id, dataset_id, branch_id, transaction_type, status, message, started_at, committed_at, metadata, created_at, updated_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, NULL, $9::jsonb, $10, $11)`,
			transaction.ID, transaction.TenantID, transaction.DatasetID, transaction.BranchID,
			string(transaction.TransactionType), string(transaction.Status), transaction.Message,
			transaction.StartedAt, metadata, transaction.CreatedAt, transaction.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert transaction: %w", mapWriteError(err))
		}
		return nil
	})
}

func (s *PostgresStore) GetTransaction(ctx context.Context, tenantID, datasetID, transactionID string) (domain.Transaction, error) {
	var transaction domain.Transaction
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		transaction, err = scanTransaction(tx.QueryRow(ctx, `
			SELECT `+transactionColumns+` FROM dataset.dataset_transactions
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid`, tenantID, datasetID, transactionID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get transaction: %w", err)
		}
		return nil
	})
	return transaction, err
}

func (s *PostgresStore) ListTransactions(ctx context.Context, tenantID, datasetID string, offset, limit int) ([]domain.Transaction, int64, error) {
	var total int64
	items := make([]domain.Transaction, 0)
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM dataset.dataset_transactions
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid`, tenantID, datasetID).Scan(&total); err != nil {
			return fmt.Errorf("count transactions: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT `+transactionColumns+` FROM dataset.dataset_transactions
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid
			ORDER BY started_at DESC, id DESC
			LIMIT $3 OFFSET $4`, tenantID, datasetID, limit, offset)
		if err != nil {
			return fmt.Errorf("list transactions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			transaction, err := scanTransaction(rows)
			if err != nil {
				return fmt.Errorf("scan transaction: %w", err)
			}
			items = append(items, transaction)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate transactions: %w", err)
		}
		return nil
	})
	return items, total, err
}

func (s *PostgresStore) CommitTransaction(ctx context.Context, tenantID, datasetID, transactionID string, committedAt time.Time) (domain.Transaction, error) {
	var transaction domain.Transaction
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		transaction, err = scanTransaction(tx.QueryRow(ctx, `
			UPDATE dataset.dataset_transactions
			SET status = 'committed', committed_at = $4, updated_at = $4
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid AND status = 'open'
			RETURNING `+transactionColumns, tenantID, datasetID, transactionID, committedAt))
		if errors.Is(err, pgx.ErrNoRows) {
			var status string
			lookupErr := tx.QueryRow(ctx, `
				SELECT status FROM dataset.dataset_transactions
				WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid`,
				tenantID, datasetID, transactionID).Scan(&status)
			switch {
			case lookupErr == nil:
				return fmt.Errorf("%w: transaction is %s", domain.ErrInvalidState, status)
			case errors.Is(lookupErr, pgx.ErrNoRows):
				return domain.ErrNotFound
			default:
				return fmt.Errorf("lookup transaction state: %w", lookupErr)
			}
		}
		if err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE dataset.dataset_branches
			SET head_transaction_id = $4::uuid, updated_at = $4
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid`,
			tenantID, datasetID, transaction.BranchID, committedAt); err != nil {
			return fmt.Errorf("update branch head: %w", err)
		}
		return nil
	})
	return transaction, err
}

func (s *PostgresStore) AbortTransaction(ctx context.Context, tenantID, datasetID, transactionID string, abortedAt time.Time) (domain.Transaction, error) {
	var transaction domain.Transaction
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		transaction, err = scanTransaction(tx.QueryRow(ctx, `
			UPDATE dataset.dataset_transactions
			SET status = 'aborted', updated_at = $4
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid AND status = 'open'
			RETURNING `+transactionColumns, tenantID, datasetID, transactionID, abortedAt))
		if errors.Is(err, pgx.ErrNoRows) {
			var status string
			lookupErr := tx.QueryRow(ctx, `
				SELECT status FROM dataset.dataset_transactions
				WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid`,
				tenantID, datasetID, transactionID).Scan(&status)
			switch {
			case lookupErr == nil:
				return fmt.Errorf("%w: transaction is %s", domain.ErrInvalidState, status)
			case errors.Is(lookupErr, pgx.ErrNoRows):
				return domain.ErrNotFound
			default:
				return fmt.Errorf("lookup transaction state: %w", lookupErr)
			}
		}
		if err != nil {
			return fmt.Errorf("abort transaction: %w", err)
		}
		return nil
	})
	return transaction, err
}

func (s *PostgresStore) CreateView(ctx context.Context, view domain.View) error {
	return s.withTenantTx(ctx, view.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO dataset.dataset_views
				(id, tenant_id, dataset_id, branch_id, name, query, version, created_at, updated_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, NULLIF($4, '')::uuid, $5, $6, $7, $8, $9)`,
			view.ID, view.TenantID, view.DatasetID, view.BranchID, view.Name, view.Query,
			view.Version, view.CreatedAt, view.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert view: %w", mapWriteError(err))
		}
		return nil
	})
}

func (s *PostgresStore) GetView(ctx context.Context, tenantID, datasetID, viewID string) (domain.View, error) {
	var view domain.View
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		view, err = scanView(tx.QueryRow(ctx, `
			SELECT `+viewColumns+` FROM dataset.dataset_views
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid AND deleted_at IS NULL`,
			tenantID, datasetID, viewID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get view: %w", err)
		}
		return nil
	})
	return view, err
}

func (s *PostgresStore) ListViews(ctx context.Context, tenantID, datasetID string, offset, limit int) ([]domain.View, int64, error) {
	var total int64
	items := make([]domain.View, 0)
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM dataset.dataset_views
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND deleted_at IS NULL`,
			tenantID, datasetID).Scan(&total); err != nil {
			return fmt.Errorf("count views: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT `+viewColumns+` FROM dataset.dataset_views
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND deleted_at IS NULL
			ORDER BY created_at DESC, id DESC
			LIMIT $3 OFFSET $4`, tenantID, datasetID, limit, offset)
		if err != nil {
			return fmt.Errorf("list views: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			view, err := scanView(rows)
			if err != nil {
				return fmt.Errorf("scan view: %w", err)
			}
			items = append(items, view)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate views: %w", err)
		}
		return nil
	})
	return items, total, err
}

func (s *PostgresStore) UpdateView(ctx context.Context, view domain.View) error {
	var affected int64
	err := s.withTenantTx(ctx, view.TenantID, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			UPDATE dataset.dataset_views
			SET branch_id = NULLIF($4, '')::uuid, query = $5, version = $6, updated_at = $7
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid AND deleted_at IS NULL`,
			view.TenantID, view.DatasetID, view.ID, view.BranchID, view.Query, view.Version, view.UpdatedAt)
		if err != nil {
			return fmt.Errorf("update view: %w", mapWriteError(err))
		}
		affected = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteView(ctx context.Context, tenantID, datasetID, viewID string, deletedAt time.Time) error {
	var affected int64
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			UPDATE dataset.dataset_views
			SET deleted_at = $4, updated_at = $4
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid AND deleted_at IS NULL`,
			tenantID, datasetID, viewID, deletedAt)
		if err != nil {
			return fmt.Errorf("delete view: %w", err)
		}
		affected = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateFile(ctx context.Context, file domain.File) error {
	return s.withTenantTx(ctx, file.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO dataset.dataset_files
				(id, tenant_id, dataset_id, branch_id, object_key, size_bytes, row_count, etag, content_type, created_at, updated_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, NULLIF($4, '')::uuid, $5, $6, $7, $8, $9, $10, $11)`,
			file.ID, file.TenantID, file.DatasetID, file.BranchID, file.ObjectKey, file.SizeBytes,
			file.RowCount, file.ETag, file.ContentType, file.CreatedAt, file.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert file metadata: %w", mapWriteError(err))
		}
		return nil
	})
}

func (s *PostgresStore) GetFile(ctx context.Context, tenantID, datasetID, fileID string) (domain.File, error) {
	var file domain.File
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		file, err = scanFile(tx.QueryRow(ctx, `
			SELECT `+fileColumns+` FROM dataset.dataset_files
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid AND deleted_at IS NULL`,
			tenantID, datasetID, fileID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get file metadata: %w", err)
		}
		return nil
	})
	return file, err
}

func (s *PostgresStore) ListFiles(ctx context.Context, tenantID, datasetID, branchID string, offset, limit int) ([]domain.File, int64, error) {
	var total int64
	items := make([]domain.File, 0)
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM dataset.dataset_files
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND deleted_at IS NULL
			  AND ($3 = '' OR branch_id = NULLIF($3, '')::uuid)`,
			tenantID, datasetID, branchID).Scan(&total); err != nil {
			return fmt.Errorf("count file metadata: %w", err)
		}
		rows, err := tx.Query(ctx, `
			SELECT `+fileColumns+` FROM dataset.dataset_files
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND deleted_at IS NULL
			  AND ($3 = '' OR branch_id = NULLIF($3, '')::uuid)
			ORDER BY created_at DESC, id DESC
			LIMIT $4 OFFSET $5`, tenantID, datasetID, branchID, limit, offset)
		if err != nil {
			return fmt.Errorf("list file metadata: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			file, err := scanFile(rows)
			if err != nil {
				return fmt.Errorf("scan file metadata: %w", err)
			}
			items = append(items, file)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate file metadata: %w", err)
		}
		return nil
	})
	return items, total, err
}

func (s *PostgresStore) DeleteFile(ctx context.Context, tenantID, datasetID, fileID string, deletedAt time.Time) error {
	var affected int64
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `
			UPDATE dataset.dataset_files
			SET deleted_at = $4, updated_at = $4
			WHERE tenant_id = $1::uuid AND dataset_id = $2::uuid AND id = $3::uuid AND deleted_at IS NULL`,
			tenantID, datasetID, fileID, deletedAt)
		if err != nil {
			return fmt.Errorf("delete file metadata: %w", err)
		}
		affected = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
