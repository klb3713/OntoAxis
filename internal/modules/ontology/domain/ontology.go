// Package domain contains the Ontology metadata entities and business rules.
// It has no dependency on HTTP, SQL or any transport.
package domain

import "time"

// Status is the lifecycle status of an object type.
type Status string

const (
	StatusActive     Status = "active"
	StatusDraft      Status = "draft"
	StatusDeprecated Status = "deprecated"
)

// Cardinality describes how many instances of one link type may relate.
type Cardinality string

const (
	CardinalityOneToOne   Cardinality = "one_to_one"
	CardinalityOneToMany  Cardinality = "one_to_many"
	CardinalityManyToMany Cardinality = "many_to_many"
)

// ObjectType is an ontology object type definition.
type ObjectType struct {
	ID                 string
	TenantID           string
	Name               string
	DisplayName        string
	Description        string
	PrimaryKeyProperty string
	BackingDatasetID   string
	DatasetSyncMode    string
	Status             Status
	Properties         []Property
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Property is a field definition belonging to an object type.
type Property struct {
	ID               string
	TenantID         string
	ObjectTypeID     string
	Name             string
	DisplayName      string
	Description      string
	PropertyType     string
	Required         bool
	UniqueConstraint bool
	Config           map[string]any
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// LinkType is a typed relationship between two object types.
type LinkType struct {
	ID           string
	TenantID     string
	Name         string
	DisplayName  string
	Description  string
	SourceTypeID string
	TargetTypeID string
	Cardinality  Cardinality
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Page describes a validated pagination request.
type Page struct {
	Page    int
	PerPage int
}

const (
	DefaultPage    = 1
	DefaultPerPage = 50
	MaxPerPage     = 200
)

// PageResult is a page of entities plus the total matching row count.
type PageResult[T any] struct {
	Items   []T
	Total   int
	Page    int
	PerPage int
}
