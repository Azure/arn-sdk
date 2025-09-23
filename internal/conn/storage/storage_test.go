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

func TestUploadContainerBug(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	testData := []byte("test data for upload")

	tests := []struct {
		name            string
		uploaderErr     error
		errOnFirst      bool
		containerErr    error
		wantUploadCount int
		wantErr         bool
	}{
		{
			name:            "Success: should upload only once when successful",
			uploaderErr:     nil,
			containerErr:    nil,
			wantUploadCount: 1,
			wantErr:         false,
		},
		{
			name: "Success: container not found, create succeeds, should retry upload",
			uploaderErr: &azcore.ResponseError{
				ErrorCode: string(bloberror.ContainerNotFound),
			},
			errOnFirst:      true,
			containerErr:    nil,
			wantUploadCount: 2,
			wantErr:         false,
		},
		{
			name: "Success: container not found, already exists, should retry upload",
			uploaderErr: &azcore.ResponseError{
				ErrorCode: string(bloberror.ContainerNotFound),
			},
			errOnFirst: true,
			containerErr: &azcore.ResponseError{
				ErrorCode: string(bloberror.ContainerAlreadyExists),
			},
			wantUploadCount: 2,
			wantErr:         false,
		},
		{
			name: "Error: container not found, create fails with other error",
			uploaderErr: &azcore.ResponseError{
				ErrorCode: string(bloberror.ContainerNotFound),
			},
			containerErr:    errors.New("create failed"),
			wantUploadCount: 1,
			wantErr:         true,
		},
		{
			name:            "Error: upload fails with non-container error",
			uploaderErr:     errors.New("network error"),
			containerErr:    nil,
			wantUploadCount: 1,
			wantErr:         true,
		},
	}

	for _, test := range tests {
		uploader := &fakeTrackingUploader{
			err:        test.uploaderErr,
			errOnFirst: test.errOnFirst,
		}
		container := &fakeContClient{
			err: test.containerErr,
		}

		args := uploadArgs{
			b:      testData,
			upload: uploader,
			create: container,
		}

		err := upload(ctx, args)

		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestUploadContainerBug(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestUploadContainerBug(%s): got err == %s, want err == nil", test.name, err)
			continue
		}

		if uploader.uploadCount != test.wantUploadCount {
			t.Errorf("TestUploadContainerBug(%s): upload called %d times, want %d times", test.name, uploader.uploadCount, test.wantUploadCount)
		}

		// Verify that the same data was uploaded each time
		for i, data := range uploader.data {
			if string(data) != string(testData) {
				t.Errorf("TestUploadContainerBug(%s): upload %d had wrong data: got %q, want %q", test.name, i+1, string(data), string(testData))
			}
		}
	}
}

func TestCName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		now      func() time.Time
		contExt  string
		expected string
	}{
		{
			name: "Default container name without extension",
			now: func() time.Time {
				return time.Date(2023, 10, 1, 15, 0, 0, 0, time.UTC)
			},
			contExt:  "",
			expected: "arm-ext-nt-2023-10-01-15",
		},
		{
			name: "Container name with extension",
			now: func() time.Time {
				return time.Date(2023, 10, 1, 15, 0, 0, 0, time.UTC)
			},
			contExt:  "my-extension",
			expected: "arm-ext-nt-my-extension-2023-10-01-15",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &Client{
				now:     test.now,
				contExt: test.contExt,
			}

			got := client.cName()
			if got != test.expected {
				t.Errorf("TestCName(%s): got %s, want %s", test.name, got, test.expected)
			}
		})
	}
}
