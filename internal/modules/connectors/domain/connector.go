// Package domain contains the connectors business types and invariants. It has
// no dependency on transport, persistence, or generated protocol code.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ConnectorType is the supported external data source type.
type ConnectorType string

const (
	TypePostgres ConnectorType = "postgres"
	TypeMySQL    ConnectorType = "mysql"
	TypeS3       ConnectorType = "s3"
	TypeRESTAPI  ConnectorType = "rest_api"
	TypeKafka    ConnectorType = "kafka"
)

var supportedTypes = map[ConnectorType]struct{}{
	TypePostgres: {},
	TypeMySQL:    {},
	TypeS3:       {},
	TypeRESTAPI:  {},
	TypeKafka:    {},
}

// Status is the lifecycle state of a connector.
type Status string

const (
	StatusActive Status = "active"
	StatusPaused Status = "paused"
	StatusError  Status = "error"
)

var supportedStatuses = map[Status]struct{}{
	StatusActive: {},
	StatusPaused: {},
	StatusError:  {},
}

var (
	// ErrInvalidName reports a missing or malformed connector name.
	ErrInvalidName = errors.New("invalid connector name")
	// ErrInvalidType reports an unsupported connector type.
	ErrInvalidType = errors.New("invalid connector type")
	// ErrInvalidStatus reports an unsupported connector status.
	ErrInvalidStatus = errors.New("invalid connector status")
	// ErrInvalidConfig reports config that is not a JSON object or has invalid values.
	ErrInvalidConfig = errors.New("invalid connector config")
	// ErrSensitiveConfig reports credentials placed directly in config.
	ErrSensitiveConfig = errors.New("sensitive connector config")
	// ErrInvalidTags reports an invalid tag value.
	ErrInvalidTags = errors.New("invalid connector tags")
	// ErrInvalidCreatedBy reports an invalid optional created_by value.
	ErrInvalidCreatedBy = errors.New("invalid connector created_by")
	// ErrNotFound reports a connector absent from the current tenant.
	ErrNotFound = errors.New("connector not found")
	// ErrConflict reports a name conflict or illegal state change.
	ErrConflict = errors.New("connector conflict")
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

// Connector is a tenant-owned external data source definition. SecretRef is
// stored for adapters but must never be exposed by transport responses.
type Connector struct {
	ID          string
	TenantID    string
	Name        string
	DisplayName string
	Type        ConnectorType
	Config      map[string]any
	Tags        []string
	SecretRef   string
	Status      Status
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// ParseType validates and normalizes a connector type.
func ParseType(value string) (ConnectorType, error) {
	t := ConnectorType(strings.ToLower(strings.TrimSpace(value)))
	if _, ok := supportedTypes[t]; !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidType, value)
	}
	return t, nil
}

// ParseStatus validates and normalizes a connector status.
func ParseStatus(value string) (Status, error) {
	s := Status(strings.ToLower(strings.TrimSpace(value)))
	if _, ok := supportedStatuses[s]; !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidStatus, value)
	}
	return s, nil
}

// ValidateName enforces the stable machine-name format used for uniqueness.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w: must match %s", ErrInvalidName, namePattern.String())
	}
	return nil
}

// ValidateTags rejects empty tag values.
func ValidateTags(tags []string) error {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("%w: tag must not be empty", ErrInvalidTags)
		}
	}
	return nil
}

// ValidateCreatedBy accepts an empty value or a UUID string.
func ValidateCreatedBy(value string) error {
	if value == "" {
		return nil
	}
	if len(value) != 36 {
		return fmt.Errorf("%w: must be a uuid", ErrInvalidCreatedBy)
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return fmt.Errorf("%w: must be a uuid", ErrInvalidCreatedBy)
			}
		default:
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				return fmt.Errorf("%w: must be a uuid", ErrInvalidCreatedBy)
			}
		}
	}
	return nil
}

// sensitiveKeys are compared case-insensitively after removing underscores and
// hyphens so that password, client_secret, apiKey, and api-key are all caught.
var sensitiveKeys = map[string]struct{}{
	"password":         {},
	"passwd":           {},
	"token":            {},
	"accesstoken":      {},
	"refreshtoken":     {},
	"authtoken":        {},
	"apikey":           {},
	"secret":           {},
	"clientsecret":     {},
	"secretkey":        {},
	"secretaccesskey":  {},
	"accesskeyid":      {},
	"accesskeysecret":  {},
	"privatekey":       {},
	"credentials":      {},
	"credential":       {},
	"authorization":    {},
	"bearer":           {},
	"connectionstring": {},
}

// ValidateConfig rejects non-object configs and any nested key that looks like
// a credential. Connector config must never persist secrets.
func ValidateConfig(config map[string]any) error {
	if config == nil {
		return fmt.Errorf("%w: must be a JSON object", ErrInvalidConfig)
	}
	return validateConfigValue(config)
}

func validateConfigValue(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			normalized := normalizeKey(key)
			if _, sensitive := sensitiveKeys[normalized]; sensitive {
				return fmt.Errorf("%w: key %q is not allowed; use secret_ref", ErrSensitiveConfig, key)
			}
			if err := validateConfigValue(nested); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := validateConfigValue(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return key
}
