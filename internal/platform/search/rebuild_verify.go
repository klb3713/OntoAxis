package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// One fixed-size hash per document keeps memory independent of document size.
// The scope digest is the SHA-256 of those hashes in document ID order.
type documentHashes map[string][sha256.Size]byte

func (d documentHashes) add(doc Document) error {
	if id, err := uuid.Parse(doc.ID); err != nil || id == uuid.Nil || id.String() != doc.ID {
		return fmt.Errorf("search verify: invalid document id")
	}
	if _, exists := d[doc.ID]; exists {
		return fmt.Errorf("search verify: duplicate document id")
	}
	doc.CreatedAt = doc.CreatedAt.UTC()
	doc.UpdatedAt = doc.UpdatedAt.UTC()
	encoded, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("search verify: cannot encode a document")
	}
	d[doc.ID] = sha256.Sum256(encoded)
	return nil
}

func (d documentHashes) digest() string {
	ids := make([]string, 0, len(d))
	for id := range d {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	overall := sha256.New()
	for _, id := range ids {
		sum := d[id]
		_, _ = overall.Write(sum[:])
	}
	return hex.EncodeToString(overall.Sum(nil))
}

func (r *Rebuilder) verifyDocuments(ctx context.Context, tenantID uuid.UUID, objectTypeID *uuid.UUID, indexUID string) (string, error) {
	dbDocs, err := r.databaseDocumentHashes(ctx, tenantID, objectTypeID)
	if err != nil {
		return "", err
	}
	indexDocs, err := r.indexDocumentHashes(ctx, tenantID, objectTypeID, indexUID)
	if err != nil {
		return "", err
	}
	if len(dbDocs) != len(indexDocs) {
		return "", fmt.Errorf("search verify: tenant %s document count mismatch: database=%d index=%d", tenantID, len(dbDocs), len(indexDocs))
	}
	dbDigest, indexDigest := dbDocs.digest(), indexDocs.digest()
	if dbDigest != indexDigest {
		return "", fmt.Errorf("search verify: tenant %s document digest mismatch: documents=%d database_digest=%s index_digest=%s", tenantID, len(dbDocs), dbDigest, indexDigest)
	}
	return dbDigest, nil
}

func (r *Rebuilder) databaseDocumentHashes(ctx context.Context, tenantID uuid.UUID, objectTypeID *uuid.UUID) (documentHashes, error) {
	// Keep all PostgreSQL pages on the same snapshot. Meilisearch does not
	// provide a shared snapshot with Postgres, so concurrent writes may require
	// retrying verification after the worker has caught up.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("search verify: begin database snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	docs := make(documentHashes)
	for after := uuid.Nil; ; {
		objects, next, err := scanObjectBatch(ctx, tx, tenantID, objectTypeID, after)
		if err != nil {
			return nil, err
		}
		for _, object := range objects {
			doc, err := BuildDocument(object)
			if err != nil {
				return nil, fmt.Errorf("search verify: build expected document: %w", err)
			}
			if err := docs.add(doc); err != nil {
				return nil, err
			}
		}
		if len(objects) < rebuildBatchSize {
			break
		}
		after = next
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("search verify: commit database snapshot: %w", err)
	}
	return docs, nil
}

func (r *Rebuilder) indexDocumentHashes(ctx context.Context, tenantID uuid.UUID, objectTypeID *uuid.UUID, indexUID string) (documentHashes, error) {
	// Bound the page to the client's response budget even for escaped text.
	const pageSize = 100
	docs := make(documentHashes)
	seen := make(map[string]struct{})
	total := -1
	for offset := 0; ; {
		page, err := r.client.ListDocuments(ctx, indexUID, offset, pageSize)
		if err != nil {
			return nil, fmt.Errorf("search verify: list index documents: %w", err)
		}
		if total < 0 {
			total = page.Total
		}
		if total < 0 || page.Total != total || len(page.Documents) != min(pageSize, max(0, total-offset)) {
			return nil, fmt.Errorf("search verify: index documents changed while paging")
		}
		for _, doc := range page.Documents {
			if doc.TenantID != tenantID.String() {
				return nil, fmt.Errorf("search verify: index document outside tenant scope")
			}
			if _, exists := seen[doc.ID]; exists {
				return nil, fmt.Errorf("search verify: duplicate document id in index pages")
			}
			seen[doc.ID] = struct{}{}
			if objectTypeID == nil || doc.ObjectTypeID == objectTypeID.String() {
				if err := docs.add(doc); err != nil {
					return nil, err
				}
			}
		}
		offset += len(page.Documents)
		if offset == total {
			return docs, nil
		}
	}
}
