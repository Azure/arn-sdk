/*
Package storage provides a client around Azure Blob Storage for sending data
that will be used by the ARN service.

Usage:

	cli, err := storage.New("https://myaccount.blob.core.windows.net", cred)
	if err != nil {
		log.Fatal(err)
	}
	defer cli.Close()

	if err := cli.Upload(ctx, "my-id", []byte("my-data")); err != nil {
		log.Fatal(err)
	}

Changes from the original:

- Moved to the slog package and from passing logrus via context.
  - No longer use any logging from azcore.

- Made the credential cache non-blocking with background refreshes instead of stop the world refreshes
- Used the exponential backoff package for retries
- Moved to standard go options for constructors
- Removed use of "to" package for pointer creation, replaced with a simple generic function
- Wrote tests and changed structure to help with testing
- Did some re-ordering of events to avoid making unnecessary calls in certain failiure cases
*/
package storage

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
)

// Client is a client for interacting with Azure Blob Storage for pushing and pulling data
// used by the ARN service.
type Client struct {
	now           func() time.Time
	cli           *service.Client
	clientOptions policy.ClientOptions
	creds         *credCache
	contExt       string

	log *slog.Logger

	// fakeUploader is used for testing purposes to simulate this client's response.
	fakeUploader Uploader

	fakeSignParams func(sigVals sas.BlobSignatureValues, cred *service.UserDelegationCredential) (encoder, error)
}

// Option is a function that sets an option on the client.
type Option func(*Client) error

// WithLogger sets the logger on the client. By default it uses slog.Default().
func WithLogger(log *slog.Logger) Option {
	return func(c *Client) error {
		c.log = log
		return nil
	}
}

// WithPolicyOptions sets the policy options for the service.Client.
// By default it uses the default policy options.
func WithPolicyOptions(opts policy.ClientOptions) Option {
	return func(c *Client) error {
		c.clientOptions = opts
		return nil
	}
}

var contRE = regexp.MustCompile(`^[a-z0-9-]{1,41}$`)

// WithContainerExt sets a name extension for a blob container. This can be useful for
// doing discovery of containers that are created by a particular client.
// Names are in the format "arm-ext-nt-YYYY-MM-DD-HH". This will cause the client to create
// "arm-ext-nt-[ext]-YYYY-MM-DD-HH". Note characters must be letters, numbers, or hyphens.
// Any letters will be automatically lowercased. The ext cannot be more than 41 characters.
func WithContainerExt(ext string) Option {
	return func(c *Client) error {
		if !contRE.MatchString(ext) {
			return fmt.Errorf("container extension must be lowercase letters, numbers, or hyphens")
		}
		c.contExt = ext
		return nil
	}
}

// Uploader is an interface for testing purposes to simulate the Upload() method.
type Uploader interface {
	// Upload simulates the Upload() method.
	Upload(ctx context.Context, id string, b []byte) (*url.URL, error)
}

// WithFake sets a fake uploader for testing purposes. This will cause the client to use the fake
// Upload() method instead of the real one. Can only be used in testing.
func WithFake(f Uploader) Option {
	return func(c *Client) error {
		if !testing.Testing() {
			return fmt.Errorf("storage.WithFake() can only be used in testing")
		}
		c.fakeUploader = f
		return nil
	}
}

// New creates a new storage client. endpoint is the Azure Blob Storage endpoint, cred is the
// Azure SDK TokenCredential, and opts are the policy options for the service.Client.
func New(endpoint string, cred azcore.TokenCredential, options ...Option) (*Client, error) {
	client := &Client{
		now: time.Now,
	}

	for _, o := range options {
		if err := o(client); err != nil {
			return nil, err
		}
	}

	if client.log == nil {
		client.log = slog.Default()
	}

	if client.fakeUploader != nil {
		return client, nil
	}

	sClient, err := service.NewClient(endpoint, cred, &service.ClientOptions{ClientOptions: client.clientOptions})
	if err != nil {
		return nil, err
	}
	client.cli = sClient

	// TODO: We need to check if the storage containers delete themselves after a certain period of time.
	// If not fail.

	client.creds, err = newCredCache(sClient, withLogger(client.log))
	if err != nil {
		return nil, err
	}

	return client, nil
}

// Close closes the client.
func (c *Client) Close() {
	if c.fakeUploader != nil {
		return
	}
	c.creds.close()
}

// Upload uploads bytes to a blob named id in today's container.  It returns a SAS link enabling the blob to be read.
func (c *Client) Upload(ctx context.Context, id string, b []byte) (*url.URL, error) {
	var cName string

	if c.fakeUploader != nil {
		return c.fakeUploader.Upload(ctx, id, b)
	}

	cName = c.cName()
	bName := id + ".txt"

	cClient := c.cli.NewContainerClient(cName)
	bClient := cClient.NewBlockBlobClient(bName)

	u, err := url.Parse(bClient.URL())
	if err != nil {
		return nil, fmt.Errorf("URL returend by blob client is not a valid URL: %w", err)
	}

	args := uploadArgs{
		id:     id,
		b:      b,
		cName:  cName,
		bName:  bName,
		upload: bClient,
		create: cClient,
		url:    u,
	}

	return c.upload(ctx, args)
}

// cName returns the container name to be used.
func (c *Client) cName() string {
	const contPrefix = "arm-ext-nt"
	if c.contExt == "" {
		return fmt.Sprintf("%s-%s-%d", contPrefix, c.now().UTC().Format(time.DateOnly), c.now().Hour())
	}
	return fmt.Sprintf("%s-%s-%s-%d", contPrefix, c.contExt, c.now().UTC().Format(time.DateOnly), c.now().Hour())
}

// uploadBuffer is an interface for uploading a buffer. Implemented by *blockblob.BlockBlobClient.
type uploadBuffer interface {
	UploadBuffer(ctx context.Context, buffer []byte, o *blockblob.UploadBufferOptions) (blockblob.UploadBufferResponse, error)
}

// createContainer is an interface for creating a container.
// Implemented by *container.Client.
type createContainer interface {
	Create(ctx context.Context, options *container.CreateOptions) (container.CreateResponse, error)
}

// uploadArgs is a struct for holding the arguments to the upload function.
// It is field-aligned.
type uploadArgs struct {
	upload uploadBuffer
	create createContainer
	url    *url.URL
	id     string
	cName  string
	bName  string
	b      []byte
}

// upload uploads the the data (args.b) to a container. If the container doesn't exist, it creates it.
// It then returns the URL with an SAS token and returns it. This is used to signal ARN of the blob location.
func (c *Client) upload(ctx context.Context, args uploadArgs) (*url.URL, error) {
	cred, err := c.creds.get(ctx)
	if err != nil {
		return nil, err
	}

	if err := upload(ctx, args); err != nil {
		return nil, err
	}

	sigVals := sas.BlobSignatureValues{
		Protocol:      sas.ProtocolHTTPS,
		StartTime:     time.Now().UTC().Add(time.Second * -10),
		ExpiryTime:    c.now().UTC().Add(7 * 24 * time.Hour),
		Permissions:   (&sas.BlobPermissions{Read: true}).String(),
		ContainerName: args.cName,
		BlobName:      args.bName,
	}

	c.log.Debug(fmt.Sprintf("Uploading to blob. Container: %s, Blob: %s", args.cName, args.bName))

	enc, err := c.signParams(sigVals, cred)
	if err != nil {
		return nil, err
	}

	args.url.RawQuery = enc.Encode()
	return args.url, nil
}

type encoder interface {
	Encode() string
}

// signParams signs the parameters for the blob and returns a query string that can be used
// to access the blob. This is here because this is particularly difficult to test and needs to be faked,
// but if it doesn't work the whole system will fail.
func (c *Client) signParams(sigVals sas.BlobSignatureValues, cred *service.UserDelegationCredential) (encoder, error) {
	if c.fakeSignParams != nil {
		return c.fakeSignParams(sigVals, cred)
	}
	params, err := sigVals.SignWithUserDelegation(cred)
	if err != nil {
		return nil, err
	}
	return &params, nil
}

func upload(ctx context.Context, args uploadArgs) error {
	_, err := args.upload.UploadBuffer(ctx, args.b, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.ContainerNotFound) {
			_, err = args.create.Create(ctx, nil)
			if err != nil && !bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
				return err
			}
			// Retry upload after container creation
			_, err = args.upload.UploadBuffer(ctx, args.b, nil)
			return err
		}
		return err
	}
	return nil
}

// toPtr returns a pointer to any value. Do not replace this with the various
// old "to" packages.
func toPtr[T any](v T) *T {
	return &v
}
