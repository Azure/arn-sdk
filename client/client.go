/*
Package client provides a client for interacting with the ARN service.

NOTE: AKS engineers: It is highly unlikely that you should be using this package. Please contact AKS runtime eng. for more information.

Allows you to run in two modes:
- Synchronous: Use Notify() to send a notification and block until it is sent.
- Asynchronous: Use Async() to send notifications without blocking for the result. This will not block until the send channel is full.

The Asynchronous mode provides the concept of a promise. You can send a notification and get a promise back that
will be resolved when the notification is sent or an error occurs.

If you do not want a promise, no notification will be sent, but errors will be recorded to an errors channel that you
can receive from with ARN.Errors(). This is useful for logging purposes. Errors are dropped when the channel fills, so you
are not required to listen on this channel.

These features allow you to make decisions for your service on how important the accuracy of information is where notifications are
taking excess time, the ARN service is down, the network is congested, etc.

Example - boilerplate that is needed on AKS to make ARN connections:

	// You would need to customize these for yourself. You need an ARN endpoint from the ARN team along with
	// associated credentials.
	var (
		arnEndpoint    = flag.String("arnEndpoint", "https://ms-containerservice-df.receiver.arn-df.core.windows.net", "The ARN endpoint to use")
		storageAccount = flag.String("storageAccount", "https://accountname.blob.core.windows.net", "The storage account to use")
		location       = flag.String("location", "westus2", "The location of the resource, like eastus")
		msid           = flag.String("msid", "/subscriptions/26fe00f8-9173-4872-9134-bb1d2e00343a/resourcegroups/aksarntest/providers/Microsoft.ManagedIdentity/userAssignedIdentities/aksarnidentity", "The managed identity ID to use")
	)

	// aaa is a helper function to get the k8s clientset and managed identity credential.
	func aaa() (*kubernetes.Clientset, *azidentity.ManagedIdentityCredential, error) {
			// If you are not sending notifications from K8, then you would not need this.
			clientSet, err := k8Clientset()
			if err != nil {
				return nil, nil, err
			}

			msiCred, err := msiCred()
			if err != nil {
				return nil, nil, err
			}

			return clientSet, msiCred, nil
	}

	// k8Clientset returns a kubernetes clientset.
	func k8Clientset() (*kubernetes.Clientset, error) {
			var kubeconfig string
			if home := homedir.HomeDir(); home != "" {
				kubeconfig = filepath.Join(home, ".kube", "config")
			} else {
				kubeconfig = ""
			}

			config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				return nil, err
			}

			return kubernetes.NewForConfig(config)
	}

	// msiCred returns a managed identity credential.
	func msiCred() (*azidentity.ManagedIdentityCredential, error) {
			msiResc := azidentity.ResourceID(*msid)
			msiOpts := azidentity.ManagedIdentityCredentialOptions{ID: msiResc}
			cred, err := azidentity.NewManagedIdentityCredential(&msiOpts)
			if err != nil {
				return nil, err
			}
			return cred, nil
	}

	func main() {
		flag.Parse()

		// ARN client uses UUIDs, this greatly improves the performance of UUID generation.
		uuid.EnableRandPool()

		if arnRscID == "" {
			panic("RESOURCE_ID environment variable must be set")
		}

		// This gets our AAA resources.
		// Note: I am ignoring the K8 clientset here, as it is not needed for the ARN client.
		// It would be needed for getting K8 data (if that is your source data) to send to ARN.
		_, cred, err := aaa()
		if err != nil {
			panic(err)
		}

		// Create the ARN client.
		arnClient, err := client.New(
			bkCtx,
			client.Args{
				HTTP: HTTPArgs{
					Endpoint: *arnEndpoint,
					Cred:    cred,
				},
				Blob: BlobArgs{
					Endpoint: *storageAccount,
					Cred:     cred,
				},
			},
		)
		if err != nil {
			panic(err)
		}

Example - sending a notification synchronously using the v3 model using a AKS node event:

	// Note: node is a k8 Node object that is JSON serializable
	// Note: rscID is the *arm.ResourceID of the node, which is created with  github.com/Azure/azure-sdk-for-go/sdk/azcore/arm.ParseResourceID()
	// You can get a rescID with arm.ParseResourceID(path.Join(p.rescPrefix, suffix))
	// where rescPrefix looks like: /subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/test/providers/Microsoft.ContainerService/managedClusters/something/
	// and suffix is something like: nodes/aks-nodepool1-12345678-vmss000000
	// Suffix is negotiated with the ARN team.

	armRsc, err := NewArmResource(types.ActSnapshot, rscID, "2024-01-01", nil)
	if err != nil {
		return err
	}

	notification := msgs.Notification{
		ResourceLocation: "eastus",
		PublisherInfo: "Microsoft.ContainerService",
		APIVersion: "2024-01-01",
		Data: []types.NotificationResource{
			{
				Data: node, // This is the Node object that will be serialized to JSON.
				ResourceEventTime: n.GetCreationTimestamp().Time.UTC(),
				ArmResource: armRsc,
				ResourceID: rescID.String(),
				ResourceSystemProperties: types.ResourceSystemProperties{
					Updated: n.GetCreationTimestamp().Time.UTC(),
					ChangeAction: types.CAUpdate,
				},
			},
			...
		}
	}

	// This is a blocking call.
	err := arnClient.Notify(ctx, notification)

Example - sending a notification asynchronously using the v3 model using a AKS node event and using a promise:

	notification := arnClient.Async(ctx, notificiation, true)
	... // Do stuff

	if err := notification.Promise(); err != nil {
		// Handle error
	}
	notification.Recycle() // Reuses the promise for the next notification

Example - sending a notification asynchronously using the v3 model using a AKS node event and without a promise:

	go func() {
		for _, err := range arnClient.Errors() {
			slog.Default().Error(err.Error())
		}
	}()

	for _, notification := range notifications {
		arnClient.Async(ctx, notificiation, false)
	}
*/
package client

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/Azure/arn-sdk/internal/conn"
	"github.com/Azure/arn-sdk/internal/conn/http"
	"github.com/Azure/arn-sdk/internal/conn/maxvals"
	"github.com/Azure/arn-sdk/internal/conn/storage"
	"github.com/Azure/arn-sdk/models"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// ARN is a client for interacting with the ARN service.
type ARN struct {
	logger *slog.Logger
	conn   *conn.Service

	in   chan models.Notifications
	errs chan error

	orderID atomic.Uint64

	testConn func(n models.Notifications)

	sigSenderClosed chan struct{}
}

// Option is a function that sets an option on the client.
type Option func(*ARN) error

// WithLogger sets the logger on the client. By default it uses slog.Default().
func WithLogger(log *slog.Logger) Option {
	return func(c *ARN) error {
		c.logger = log
		return nil
	}
}

// WithNotifyCh sets the notification channel on the client. By default it uses a new channel
// with a buffer size of 1.
func WithNotifyCh(in chan models.Notifications) Option {
	return func(c *ARN) error {
		c.in = in
		return nil
	}
}

// Args are the arguments for creating a new ARN client.
type Args struct {
	// HTTP is used to configure the HTTP client to talk to ARN.
	HTTP HTTPArgs

	// Blob is the blob storage client used for large messages.
	Blob BlobArgs

	logger *slog.Logger
}

// toClients creates an http and storage client from the args. This also
// validates the args.
func (a Args) toClients() (*http.Client, *storage.Client, error) {
	if err := a.validate(); err != nil {
		return nil, nil, err
	}

	httpOpts := []http.Option{}
	if !a.HTTP.Compression {
		httpOpts = append(httpOpts, http.WithoutCompression())
	}

	httpClient, err := http.New(a.HTTP.Endpoint, a.HTTP.Cred, a.HTTP.Opts, httpOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	blobOpts := []storage.Option{
		storage.WithLogger(a.logger),
	}

	if a.Blob.Opts != nil {
		blobOpts = append(blobOpts, storage.WithPolicyOptions(*a.Blob.Opts))
	}
	if a.Blob.ContainerExt != "" {
		blobOpts = append(blobOpts, storage.WithContainerExt(a.Blob.ContainerExt))
	}

	blobClient, err := storage.New(a.Blob.Endpoint, a.Blob.Cred, blobOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create blob client: %w", err)
	}

	return httpClient, blobClient, nil
}

func (a Args) validate() error {
	if err := a.HTTP.validate(); err != nil {
		return fmt.Errorf("invalid HTTP args: %w", err)
	}

	if err := a.Blob.validate(); err != nil {
		return fmt.Errorf("invalid blob args: %w", err)
	}
	return nil
}

// HTTPArgs are the arguments for creating a new ARN HTTP client.
type HTTPArgs struct {
	// Endpoint is the ARN endpoint.
	Endpoint string
	// Cred is the token credential to use for authentication to ARN.
	Cred azcore.TokenCredential
	// Opts are opttions for the azcore HTTP client.
	Opts *policy.ClientOptions
	// Compression is a flag to enable deflate compression on the HTTP client.
	Compression bool
}

func (a HTTPArgs) validate() error {
	if a.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if a.Cred == nil {
		return fmt.Errorf("cred is required")
	}
	return nil
}

// BlobArgs are the arguments for creating a new ARN blob client used for large transfers.
type BlobArgs struct {
	// Endpoint is the blob storage endpoint.
	Endpoint string
	// Cred is the token credential to use for authentication to blob storage.
	Cred azcore.TokenCredential
	// ContainerExt sets a name extension for a blob container. This can be useful for
	// doing discovery of containers that are created by a particular client.
	// Names are in the format "arm-ext-nt-YYYY-MM-DD". This will cause the client to create
	// "arm-ext-nt-[ext]-YYYY-MM-DD". Note characters must be letters, numbers, or hyphens.
	// Any letters will be automatically lowercased. The ext cannot be more than 41 characters.
	ContainerExt string
	// Opts are opttions for the azcore HTTP client.
	Opts *policy.ClientOptions
}

func (a BlobArgs) validate() error {
	if a.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if a.Cred == nil {
		return fmt.Errorf("cred is required")
	}
	return nil
}

// New creates a new ARN client.
func New(ctx context.Context, args Args, options ...Option) (*ARN, error) {
	a := &ARN{
		errs:            make(chan error, 1),
		sigSenderClosed: make(chan struct{}),
	}

	for _, o := range options {
		if err := o(a); err != nil {
			return nil, err
		}
	}
	if a.in == nil {
		a.in = make(chan models.Notifications, 1)
	}

	args.logger = a.logger
	h, s, err := args.toClients()
	if err != nil {
		return nil, fmt.Errorf("problem getting clients: %v", err)
	}

	a.conn, err = conn.New(h, s, a.errs, conn.WithLogger(a.logger))
	if err != nil {
		return nil, fmt.Errorf("problem with conn client: %v", err)
	}

	go a.sender()

	return a, nil
}

// Close closes the client. This will close the In() channel.
func (a *ARN) Close() {
	close(a.in)

	if a.sigSenderClosed != nil {
		<-a.sigSenderClosed
		if a.conn != nil {
			a.conn.Close()
		}
	}
}

// Errors returns a channel that will receive any errors that occur in the client where a
// promise is not used. If using Notify(), this will not be used.
func (a *ARN) Errors() <-chan error {
	return a.errs
}

// Notify sends a notification to the ARN service. This is similar to sending via Async(),
// however this will block until the notification is sent and returns any error. In reality, this
// is a thin wrapper around Async() that uses a promise to send the results.
// If the context is canceled, this will return the context error. Thread-safe (however, order usually matters
// in ARN).
func (a *ARN) Notify(ctx context.Context, n models.Notifications) error {
	x := n.DataCount()
	switch {
	case x == 0:
		return nil
	case x > maxvals.NotificationItems:
		return models.ErrBatchSize
	}

	n = n.SetCtx(ctx)
	n = n.SetPromise(conn.PromisePool.Get().(chan error))
	defer n.Recycle()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case a.in <- n:
	}

	return n.Promise(context.Background())
}

// Async sends a notification to the ARN service asynchronously. This will not block waiting for a response.
// If the promise is true, .Promise() will be used to send the results. If not, any errors will be sent
// to the ARN.Errors() channel. The returned Notification will have the Promise set if promise == true.
// NOTE: If you don't use the returned Notification for a Promise instead of the one you passed, you
// will not get the results.
// Thread-safe.
func (a *ARN) Async(ctx context.Context, n models.Notifications, promise bool) models.Notifications {
	n = n.SetCtx(ctx)
	if promise {
		n = n.SetPromise(conn.PromisePool.Get().(chan error))
	}

	x := n.DataCount()
	switch {
	case x == 0:
		n.SendPromise(nil, a.errs)
		return n
	case x > maxvals.NotificationItems:
		n.SendPromise(models.ErrBatchSize, a.errs)
		return n
	}

	if ctx.Err() != nil {
		n.SendPromise(ctx.Err(), a.errs)
		return n
	}

	select {
	case <-ctx.Done():
		n.SendPromise(ctx.Err(), a.errs)
		return n
	case a.in <- n:
	}

	return n
}

// sender loops on our input channel and sends notifications to the ARN service.
func (a *ARN) sender() {
	defer close(a.sigSenderClosed)

	for n := range a.in {
		if a.testConn != nil {
			a.testConn(n)
			continue
		}
		a.conn.Send(n)
	}
}
