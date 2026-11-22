package authz

import (
	"context"
	"fmt"

	fgaSdk "github.com/openfga/go-sdk"
	"github.com/openfga/go-sdk/client"
	"github.com/openfga/go-sdk/credentials"
)

// CheckInput is one OpenFGA relationship query with per-request contextual
// tuples. No tuple is persisted.
type CheckInput struct {
	User             string
	Relation         string
	Object           string
	ContextualTuples []fgaSdk.TupleKey
}

// CheckResult is the relationship decision only; Cedar still runs afterwards.
type CheckResult struct {
	Allowed bool
	ModelID string
}

// FGAClient is the narrow OpenFGA surface used by the decision point. Unit
// tests may inject a fake; integration tests use the real service through the
// same interface.
type FGAClient interface {
	Check(ctx context.Context, input CheckInput) (CheckResult, error)
	Startup(ctx context.Context) error
	Close() error
}

type openFGAClient struct {
	sdk     *client.OpenFgaClient
	storeID string
	modelID string
}

// NewOpenFGAClient creates the production OpenFGA adapter. It deliberately
// retries no authorization call: a timeout is a failure, not a reason to wait.
func NewOpenFGAClient(cfg Config) (FGAClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	clientCfg := &client.ClientConfiguration{
		ApiUrl:               cfg.OpenFGAApiURL,
		StoreId:              cfg.OpenFGAStoreID,
		AuthorizationModelId: cfg.OpenFGAModelID,
	}
	if cfg.OpenFGAAPIToken != "" {
		creds, err := credentials.NewCredentials(credentials.Credentials{
			Method: credentials.CredentialsMethodApiToken,
			Config: &credentials.Config{ApiToken: cfg.OpenFGAAPIToken},
		})
		if err != nil {
			return nil, fmt.Errorf("configure OpenFGA credentials: %w", err)
		}
		clientCfg.Credentials = creds
	}
	sdk, err := client.NewSdkClient(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("create OpenFGA client: %w", err)
	}
	return &openFGAClient{sdk: sdk, storeID: cfg.OpenFGAStoreID, modelID: cfg.OpenFGAModelID}, nil
}

func (c *openFGAClient) Check(ctx context.Context, input CheckInput) (CheckResult, error) {
	consistency := fgaSdk.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	resp, err := c.sdk.Check(ctx).
		Body(client.ClientCheckRequest{
			User:             input.User,
			Relation:         input.Relation,
			Object:           input.Object,
			ContextualTuples: input.ContextualTuples,
		}).
		Options(client.ClientCheckOptions{
			AuthorizationModelId: &c.modelID,
			StoreId:              &c.storeID,
			Consistency:          &consistency,
		}).
		Execute()
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{Allowed: resp.GetAllowed(), ModelID: c.modelID}, nil
}

// Startup verifies the deployment contract before the service becomes ready:
// the store and model exist, the configured model ID matches, and the store is
// empty. A non-empty store would mean persistent tuples, which v1 does not
// support.
func (c *openFGAClient) Startup(ctx context.Context) error {
	store, err := c.sdk.GetStore(ctx).Execute()
	if err != nil {
		return fmt.Errorf("read OpenFGA store: %w", err)
	}
	if store.Id != c.storeID {
		return fmt.Errorf("OpenFGA store ID mismatch: got %q", store.Id)
	}
	model, err := c.sdk.ReadAuthorizationModel(ctx).
		Options(client.ClientReadAuthorizationModelOptions{
			AuthorizationModelId: &c.modelID,
			StoreId:              &c.storeID,
		}).
		Execute()
	if err != nil {
		return fmt.Errorf("read OpenFGA authorization model: %w", err)
	}
	if model.AuthorizationModel == nil || model.AuthorizationModel.Id != c.modelID {
		return fmt.Errorf("OpenFGA authorization model mismatch")
	}
	page := int32(1)
	consistency := fgaSdk.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	tuples, err := c.sdk.Read(ctx).
		Options(client.ClientReadOptions{
			PageSize:    &page,
			StoreId:     &c.storeID,
			Consistency: &consistency,
		}).
		Body(client.ClientReadRequest{}).
		Execute()
	if err != nil {
		return fmt.Errorf("read OpenFGA tuples: %w", err)
	}
	if len(tuples.Tuples) != 0 {
		return fmt.Errorf("OpenFGA store must be empty in v1; found %d tuple(s)", len(tuples.Tuples))
	}
	return nil
}

func (c *openFGAClient) Close() error { return nil }
