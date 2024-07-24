package storage

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
)

// withTestCred sets the credCache to use the given credData and prevents
// the background refresh from starting.
func withTestCred(cd *credData) ccOption {
	return func(c *credCache) error {
		c.cred.Store(cd)
		c.start = false
		return nil
	}
}

func TestWithContainerExt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ext     string
		wantErr bool
	}{
		{
			name:    "Error: name contains uppercase letter",
			ext:     "UPPERCASE",
			wantErr: true,
		},
		{
			name:    "Error: name contains special character",
			ext:     "special!",
			wantErr: true,
		},
		{
			name:    "Error: name is too short",
			ext:     "",
			wantErr: true,
		},
		{
			name:    "Error: name is too long",
			ext:     "123456789012345678901234567890123456789012",
			wantErr: true,
		},
		{
			name: "Success",
			ext:  "lowercase-1234",
		},
	}

	for _, test := range tests {
		c := &Client{}
		err := WithContainerExt(test.ext)(c)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestWithExt(%s): got err == nil, want err != nil ", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestWithExt(%s): got err == %s, want err == nil ", test.name, err)
			continue
		case err != nil:
			continue
		}

		if c.contExt != test.ext {
			t.Errorf("TestWithExt(%s): got c.contExt == %s, want c.contExt == %s ", test.name, c.contExt, test.ext)
		}
	}
}

func TestUploadPrivate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cred           *credData
		fakeUploader   fakeUploader
		fakeCreder     fakeCreder
		fakeContClient fakeContClient
		fakeSignParams func(sigVals sas.BlobSignatureValues, cred *service.UserDelegationCredential) (encoder, error)
		wantErr        bool
		wantURL        string
	}{
		{
			name: "Error: can't get user delegation credential",
			fakeCreder: fakeCreder{
				err: errors.New("error"),
			},
			wantErr: true,
		},
		{
			name:       "Error: permanent error in upload",
			fakeCreder: fakeCreder{},
			fakeUploader: fakeUploader{
				err: errors.New("ContainerNotFound"),
			},
			fakeContClient: fakeContClient{
				err: errors.New("error"),
			},
			wantErr: true,
		},
		{
			name: "Success",
			cred: &credData{
				cred:    &service.UserDelegationCredential{},
				expires: time.Now().Add(1 * time.Hour),
			},
			fakeUploader:   fakeUploader{},
			fakeCreder:     fakeCreder{},
			fakeContClient: fakeContClient{},
			fakeSignParams: func(sigVals sas.BlobSignatureValues, cred *service.UserDelegationCredential) (encoder, error) {
				return fakeEncoder{
					qs: "qs=1",
				}, nil
			},
			wantURL: "https://example.com?qs=1",
		},
	}

	baseURL, err := url.Parse("https://example.com")
	if err != nil {
		panic(err)
	}

	for _, test := range tests {
		cc, err := newCredCache(&test.fakeCreder, withTestCred(test.cred))
		if err != nil {
			panic(err)
		}

		c := &Client{
			now:            time.Now,
			log:            slog.Default(),
			creds:          cc,
			fakeSignParams: test.fakeSignParams,
		}

		args := uploadArgs{
			b:      []byte("data"),
			upload: &test.fakeUploader,
			create: &test.fakeContClient,
			url:    baseURL,
			id:     "id",
			cName:  "cName",
			bName:  "bName",
		}

		gotURL, err := c.upload(context.Background(), args)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestUploadPrivate(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestUploadPrivate(%s): got err != nil, want err == nil", test.name)
			continue
		case err != nil:
			continue
		}

		if gotURL.String() != test.wantURL {
			t.Errorf("TestUploadPrivate(%s): got URL == %s, want URL == %s", test.name, gotURL, test.wantURL)
		}
	}
}

func TestHandleUploadErr(t *testing.T) {
	t.Parallel()

	cnfErr := &azcore.ResponseError{
		ErrorCode: string(bloberror.ContainerNotFound),
	}
	contExistErr := &azcore.ResponseError{
		ErrorCode: string(bloberror.ContainerAlreadyExists),
	}

	tests := []struct {
		name           string
		fakeContClient fakeContClient
		err            error
		wantErr        bool
		wantTryCreate  bool
	}{
		{
			name:    "nil error",
			err:     nil,
			wantErr: false,
		},
		{
			name:    "normal error, not a create container error",
			err:     errors.New("normal error"),
			wantErr: true,
		},
		{
			name: "Container not found, Create() returns a normal error",
			fakeContClient: fakeContClient{
				err: errors.New("normal error"),
			},
			err:           cnfErr,
			wantTryCreate: true,
			wantErr:       true,
		},
		{
			name: "Container not found, Create() returns no error",
			fakeContClient: fakeContClient{
				err: nil,
			},
			err:           cnfErr,
			wantTryCreate: true,
		},
		{
			name: "Container not found, Create() returns ContainerAlreadyExists",
			fakeContClient: fakeContClient{
				err: contExistErr,
			},
			err:           cnfErr,
			wantTryCreate: true,
		},
	}

	for _, test := range tests {
		fcc := &test.fakeContClient
		err := handleUploadErr(context.Background(), test.err, fcc)

		if test.wantTryCreate != fcc.called {
			t.Errorf("TestHandleUploadErr(%s): had test.wantTryCreate == %v, Create.called == %v", test.name, test.wantTryCreate, fcc.called)
			continue
		}
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestHandleUploadErr(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestHandleUploadErr(%s): got err != nil, want err == nil", test.name)
			continue
		}
	}

}
