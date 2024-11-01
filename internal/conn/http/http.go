// Package http provides a client for interacting with the ARN receiver API using an
// azcore.Client.
package http

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"sync"
	"testing"

	"github.com/Azure/arn-sdk/internal/build"

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
	New: func() any {
		return bytes.NewReader(nil)
	},
}

var flatePool = sync.Pool{
	New: func() any {
		return &bytes.Buffer{}
	},
}

// zlibTransport is a custom RoundTripper that applies Deflate compression at the desired level.
type zlibTransport struct {
	deflateLevel int
	flatePool    chan *zlib.Writer
}

func newFlateTransport() *zlibTransport {
	return &zlibTransport{
		flatePool: make(chan *zlib.Writer, 20),
	}
}

// Do performs the actual request and compresses the body using Deflate.
func (t *zlibTransport) Do(req *policy.Request) (*http.Response, error) {
	// Get the underlying http.Request
	httpReq := req.Raw()

	// If the request has a body, apply Deflate compression.
	if httpReq.Body != nil && httpReq.ContentLength > 0 {
		// Read the original body content.
		var buf bytes.Buffer
		_, err := io.Copy(&buf, httpReq.Body)
		if err != nil {
			return nil, err
		}

		// Compress the content using Deflate at the specified level.
		compressedBuffer := flatePool.Get().(*bytes.Buffer)
		defer func() {
			compressedBuffer.Reset()
			flatePool.Put(compressedBuffer)
		}()

		var writer *zlib.Writer
		select {
		case writer = <-t.flatePool:
		default:
			writer, err = zlib.NewWriterLevel(compressedBuffer, 5)
			if err != nil {
				return nil, err
			}
		}
		writer.Reset(compressedBuffer)
		_, err = writer.Write(buf.Bytes())
		if err != nil {
			return nil, err
		}
		writer.Close()
		select {
		case t.flatePool <- writer:
		default:
		}

		// Update the request with the compressed body.
		httpReq.Body = io.NopCloser(compressedBuffer)
		httpReq.ContentLength = int64(compressedBuffer.Len())
		httpReq.Header.Set("Content-Encoding", "deflate")
	}

	// Use the base RoundTripper to perform the actual request.
	return req.Next()
}

// Client is a client for interacting with the ARN receiver API.
type Client struct {
	endpoint string
	client   *azcore.Client
	compress bool

	fakeSender Sender
}

// Option is a function that configures the client.
type Option func(*Client) error

// WihtoutCompression turns off deflate compression for the client.
func WithoutCompression() Option {
	return func(c *Client) error {
		c.compress = false
		return nil
	}
}

// Sender is an interface to provide a fake sender for testing.
type Sender interface {
	Send(ctx context.Context, event []byte) error
}

// WithFake configures the client to use a fake sender for testing.
// This will be used instead of .Send(). Can only be used in tests.
func WithFake(s Sender) Option {
	return func(c *Client) error {
		if !testing.Testing() {
			return fmt.Errorf("http.WithFakeSender() can only be used in tests")
		}
		c.fakeSender = s
		return nil
	}
}

// New returns a new Client for accessing the ARN receiver API.
func New(endpoint string, cred azcore.TokenCredential, opts *policy.ClientOptions, options ...Option) (*Client, error) {
	if opts == nil {
		opts = &policy.ClientOptions{}
	}

	c := &Client{
		endpoint: endpoint,
		compress: true,
	}
	for _, option := range options {
		if err := option(c); err != nil {
			return nil, err
		}
	}

	if c.fakeSender != nil {
		return c, nil
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
	if c.compress {
		plOpts.PerRetry = append(plOpts.PerRetry, newFlateTransport())
	}

	azclient, err := azcore.NewClient("arn.Client", build.Version, plOpts, opts)
	if err != nil {
		return nil, err
	}

	if path.Dir(endpoint) != "arnnotify" {
		endpoint = runtime.JoinPaths(endpoint, "/arnnotify")
	}

	return &Client{
		endpoint: endpoint,
		client:   azclient,
	}, nil
}

// Send sends an event (converted to JSON bytes) to the ARN receiver API.
func (c *Client) Send(ctx context.Context, event []byte, headers []string) error {
	if c.fakeSender != nil {
		return c.fakeSender.Send(ctx, event)
	}
	if len(headers)%2 != 0 {
		return fmt.Errorf("headers must be key-value pairs")
	}

	read := readerPool.Get().(*bytes.Reader)
	read.Reset(event)
	defer readerPool.Put(read)

	req, err := c.setup(ctx, read, headers)
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

// appJSON is the Accept header for application/json. Set as a package
// variable to avoid allocations.
var appJSON = []string{"application/json"}

// setup creates a new request with the event as the body.
func (c *Client) setup(ctx context.Context, event *bytes.Reader, headers []string) (*policy.Request, error) {
	if event.Len() == 0 {
		return nil, fmt.Errorf("event is empty")
	}

	r := rsc{event}

	req, err := runtime.NewRequest(ctx, http.MethodPost, c.endpoint)
	if err != nil {
		return nil, err
	}
	req.Raw().Header["Accept"] = appJSON
	for i := 0; i < len(headers); i += 2 {
		req.Raw().Header.Add(headers[i], headers[i+1])
	}
	return req, req.SetBody(r, "application/json")
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
