package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	objectsapi "github.com/klb3713/ontoaxis/internal/modules/objects/api"
	"github.com/klb3713/ontoaxis/internal/platform/search"
)

// searchBackend adapts the platform Meilisearch client to the narrow
// objects API dependency. It owns the tenant filter and never accepts a tenant
// from the request body.
type searchBackend struct {
	client       *search.Client
	indexVersion string
}

func newSearchBackend(cfg search.Config) (*searchBackend, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	client, err := search.NewClient(cfg.URL, cfg.APIKey, search.WithTimeout(cfg.Timeout))
	if err != nil {
		return nil, err
	}
	return &searchBackend{client: client, indexVersion: cfg.IndexVersion}, nil
}

func (b *searchBackend) Healthy(ctx context.Context) error {
	if b == nil || b.client == nil {
		return fmt.Errorf("search backend is not configured")
	}
	return b.client.Health(ctx)
}

func (b *searchBackend) Search(ctx context.Context, req objectsapi.SearchRequest) (objectsapi.SearchResponse, error) {
	if b == nil || b.client == nil {
		return objectsapi.SearchResponse{}, fmt.Errorf("search backend is not configured")
	}
	filterParts := []string{fmt.Sprintf("tenant_id = %s", strconv.Quote(req.TenantID.String()))}
	if len(req.ObjectTypeIDs) > 0 {
		values := make([]string, 0, len(req.ObjectTypeIDs))
		for _, id := range req.ObjectTypeIDs {
			values = append(values, strconv.Quote(id.String()))
		}
		filterParts = append(filterParts, "object_type_id IN ["+strings.Join(values, ", ")+"]")
	}
	if req.UpdatedAfter != nil {
		filterParts = append(filterParts, fmt.Sprintf("updated_at >= %s", strconv.Quote(req.UpdatedAfter.UTC().Format(time.RFC3339))))
	}
	if req.UpdatedBefore != nil {
		filterParts = append(filterParts, fmt.Sprintf("updated_at <= %s", strconv.Quote(req.UpdatedBefore.UTC().Format(time.RFC3339))))
	}
	sort := []string(nil)
	if req.Sort != "" {
		sort = append(sort, req.Sort)
	}
	index := search.IndexName(b.indexVersion, req.TenantID.String())
	result, err := b.client.Search(ctx, index, search.SearchRequest{
		Q:      req.Query,
		Filter: strings.Join(filterParts, " AND "),
		Sort:   sort,
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return objectsapi.SearchResponse{}, err
	}
	response := objectsapi.SearchResponse{
		Items:     make([]objectsapi.SearchHit, 0, len(result.Hits)),
		Total:     result.EstimatedTotalHits,
		Limit:     result.Limit,
		Offset:    result.Offset,
		Truncated: result.Truncated,
	}
	for _, hit := range result.Hits {
		response.Items = append(response.Items, objectsapi.SearchHit{
			ID:           hit.ID,
			ObjectTypeID: hit.ObjectTypeID,
			DisplayName:  hit.DisplayName,
			Version:      hit.Version,
			UpdatedAt:    hit.UpdatedAt,
		})
	}
	return response, nil
}
