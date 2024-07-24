// Package http provides a client for interacting with the ARN receiver API using an
// azcore.Client.
package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/Azure/arn/internal/build"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

/*
Note: these come from: https://eng.ms/docs/cloud-ai-platform/azure-core/azure-management-and-platforms/control-plane-bburns/azure-resource-notifications/azure-resource-notifications-documentation/partners/publisher/receiver-api-usage#authentication

Environment	                    AAD Cloud    Tenant	                                Audience
DogfoodProd	                    Prod         72f988bf-86f1-41af-91ab-2d7cd011db47	api://41fc9deb-1ccc-4fcc-871d-12bf54ad8986/
DogfoodPPE	                    PPE	         ea8a4392-515e-481f-879e-6571ff2a8a36	api://41fc9deb-1ccc-4fcc-871d-12bf54ad8986/
Public Cloud (including canary)	Prod         33e01921-4d64-4f8c-a055-5bdaffd5e33d	api://41fc9deb-1ccc-4fcc-871d-12bf54ad8986/
AzureUSGovernment	            Prod         cab8a31a-1906-4287-a0d8-4eef66b95f6e	api://41fc9deb-1ccc-4fcc-871d-12bf54ad8986/
PAzureChinaCloud	            Prod         a55a4d5b-9241-49b1-b4ff-befa8db00269	api://41fc9deb-1ccc-4fcc-871d-12bf54ad8986/
*/
const (
	scopeDefault = "https://arg.management.core.windows.net//.default"
	allOthers    = "api://41fc9deb-1ccc-4fcc-871d-12bf54ad8986//.default"
)

// Note: The SDK does not seem to have anything for DogfoodProd or DogfoodPPE.
var changeScope = map[string]bool{
	cloud.AzureChina.ActiveDirectoryAuthorityHost:      true,
	cloud.AzureGovernment.ActiveDirectoryAuthorityHost: true,
	cloud.AzurePublic.ActiveDirectoryAuthorityHost:     true,
}

var readerPool = sync.Pool{
	New: func() interface{} {
		return bytes.NewReader(nil)
	},
}

// Client is a client for interacting with the ARN receiver API.
type Client struct {
	endpoint string
	client   *azcore.Client
}

// New returns a new Client for accessing the ARN receiver API.
func New(endpoint string, cred azcore.TokenCredential, opts *policy.ClientOptions) (*Client, error) {
	if opts == nil {
		opts = &policy.ClientOptions{}
	}

	var scope = scopeDefault
	if changeScope[opts.Cloud.ActiveDirectoryAuthorityHost] {
		scope = allOthers
	}

	plOpts := runtime.PipelineOptions{
		PerRetry: []policy.Policy{
			runtime.NewBearerTokenPolicy(cred, []string{scope}, nil),
		},
	}
	azclient, err := azcore.NewClient("arn.Client", build.Version, plOpts, opts)
	if err != nil {
		return nil, err
	}

	return &Client{
		endpoint: endpoint,
		client:   azclient,
	}, nil
}

// Send sends an event (converted to JSON bytes) to the ARN receiver API.
func (c *Client) Send(ctx context.Context, event []byte) error {

	req, err := c.setup(ctx, event)
	if err != nil {
		return err
	}

	// Send the event to the ARN service.
	resp, err := c.client.Pipeline().Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

// setup creates a new request with the event as the body.
func (c *Client) setup(ctx context.Context, event []byte) (*policy.Request, error) {
	if len(event) == 0 {
		return nil, fmt.Errorf("event is empty")
	}

	read := readerPool.Get().(*bytes.Reader)
	defer readerPool.Put(read)
	read.Reset(event)
	r := rsc{read}

	req, err := runtime.NewRequest(ctx, http.MethodPost, c.endpoint)
	if err != nil {
		return nil, err
	}
	req.SetBody(r, "application/json")
	return req, nil
}

// Compile-time check to verify implements interface.
var _ io.ReadSeekCloser = rsc{}

// rsc is an implementation of ReadSeekCloser.
type rsc struct {
	*bytes.Reader
}

// Close is a no-op for byteReadSeekCloser.
func (b rsc) Close() error {
	return nil
}
