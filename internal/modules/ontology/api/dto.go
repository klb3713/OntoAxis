package api

import (
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
)

type propertyDTO struct {
	ID               string    `json:"id"`
	ObjectTypeID     string    `json:"object_type_id"`
	Name             string    `json:"name"`
	DisplayName      string    `json:"display_name"`
	Description      string    `json:"description"`
	PropertyType     string    `json:"property_type"`
	Required         bool      `json:"required"`
	UniqueConstraint bool      `json:"unique_constraint"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type objectTypeDTO struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	DisplayName        string        `json:"display_name"`
	Description        string        `json:"description"`
	PrimaryKeyProperty string        `json:"primary_key_property"`
	BackingDatasetID   string        `json:"backing_dataset_id,omitempty"`
	DatasetSyncMode    string        `json:"dataset_sync_mode,omitempty"`
	Status             string        `json:"status"`
	Properties         []propertyDTO `json:"properties"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

type linkTypeDTO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	DisplayName  string    `json:"display_name"`
	Description  string    `json:"description"`
	SourceTypeID string    `json:"source_type_id"`
	TargetTypeID string    `json:"target_type_id"`
	Cardinality  string    `json:"cardinality"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type objectTypeListDTO struct {
	Items   []objectTypeDTO `json:"items"`
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	PerPage int             `json:"per_page"`
}

type linkTypeListDTO struct {
	Items   []linkTypeDTO `json:"items"`
	Total   int           `json:"total"`
	Page    int           `json:"page"`
	PerPage int           `json:"per_page"`
}

type materializeBindingDTO struct {
	OperationID string `json:"operation_id"`
}

func toPropertyDTO(property domain.Property) propertyDTO {
	return propertyDTO{
		ID:               property.ID,
		ObjectTypeID:     property.ObjectTypeID,
		Name:             property.Name,
		DisplayName:      property.DisplayName,
		Description:      property.Description,
		PropertyType:     property.PropertyType,
		Required:         property.Required,
		UniqueConstraint: property.UniqueConstraint,
		CreatedAt:        property.CreatedAt,
		UpdatedAt:        property.UpdatedAt,
	}
}

func toObjectTypeDTO(objectType domain.ObjectType) objectTypeDTO {
	properties := make([]propertyDTO, 0, len(objectType.Properties))
	for _, property := range objectType.Properties {
		properties = append(properties, toPropertyDTO(property))
	}
	return objectTypeDTO{
		ID:                 objectType.ID,
		Name:               objectType.Name,
		DisplayName:        objectType.DisplayName,
		Description:        objectType.Description,
		PrimaryKeyProperty: objectType.PrimaryKeyProperty,
		BackingDatasetID:   objectType.BackingDatasetID,
		DatasetSyncMode:    objectType.DatasetSyncMode,
		Status:             string(objectType.Status),
		Properties:         properties,
		CreatedAt:          objectType.CreatedAt,
		UpdatedAt:          objectType.UpdatedAt,
	}
}

func toLinkTypeDTO(linkType domain.LinkType) linkTypeDTO {
	return linkTypeDTO{
		ID:           linkType.ID,
		Name:         linkType.Name,
		DisplayName:  linkType.DisplayName,
		Description:  linkType.Description,
		SourceTypeID: linkType.SourceTypeID,
		TargetTypeID: linkType.TargetTypeID,
		Cardinality:  string(linkType.Cardinality),
		CreatedAt:    linkType.CreatedAt,
		UpdatedAt:    linkType.UpdatedAt,
	}
}
