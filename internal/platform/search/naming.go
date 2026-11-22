package search

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

var indexVersionPattern = regexp.MustCompile(`^v[0-9]+$`)

// TenantHash derives the stable, non-reversible tenant identifier embedded in
// index names: the first 16 characters of the lower-case hex SHA-256 of the
// tenant UUID.
func TenantHash(tenantID string) string {
	sum := sha256.Sum256([]byte(normalizeTenantID(tenantID)))
	return hex.EncodeToString(sum[:])[:16]
}

// normalizeTenantID hashes the canonical UUID representation when the input is
// a valid UUID so equivalent spellings map to the same index. Invalid input is
// trimmed and lower-cased; callers are expected to pass a tenant UUID.
func normalizeTenantID(tenantID string) string {
	trimmed := strings.TrimSpace(tenantID)
	if parsed, err := uuid.Parse(trimmed); err == nil {
		return parsed.String()
	}
	return strings.ToLower(trimmed)
}

// IndexName returns objects_<version>_<hex16(sha256(tenantUUID))>. The index
// name contains only a derived tenant identifier, never the raw tenant UUID.
func IndexName(version, tenantID string) string {
	return "objects_" + version + "_" + TenantHash(tenantID)
}

// StagingIndexName returns the temporary index name used during a rebuild.
func StagingIndexName(indexUID string, unixTS int64) string {
	return indexUID + "_r" + strconv.FormatInt(unixTS, 10)
}

// ValidateIndexVersion reports whether version matches ^v[0-9]+$.
func ValidateIndexVersion(version string) error {
	if !indexVersionPattern.MatchString(version) {
		return Errorf(ErrInvalid, "validate_index_version", "index version %q must match ^v[0-9]+$", version)
	}
	return nil
}
