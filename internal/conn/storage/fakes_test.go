package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
)

type fakeEncoder struct {
	qs string
}

func (f fakeEncoder) Encode() string {
	return f.qs
}

type fakeUploader struct {
	err error
}

func (f *fakeUploader) UploadBuffer(ctx context.Context, buffer []byte, o *blockblob.UploadBufferOptions) (blockblob.UploadBufferResponse, error) {
	return blockblob.UploadBufferResponse{}, f.err
}

type fakeTrackingUploader struct {
	uploadCount int
	data        [][]byte
	err         error
	errOnFirst  bool // If true, only return error on first call
}

func (f *fakeTrackingUploader) UploadBuffer(ctx context.Context, buffer []byte, o *blockblob.UploadBufferOptions) (blockblob.UploadBufferResponse, error) {
	f.uploadCount++
	// Copy the buffer to track what was uploaded
	dataCopy := make([]byte, len(buffer))
	copy(dataCopy, buffer)
	f.data = append(f.data, dataCopy)

	// If errOnFirst is true, only return error on first call
	if f.errOnFirst && f.uploadCount == 1 {
		return blockblob.UploadBufferResponse{}, f.err
	}
	if !f.errOnFirst {
		return blockblob.UploadBufferResponse{}, f.err
	}
	return blockblob.UploadBufferResponse{}, nil
}

type fakeCreder struct {
	err error
}

func (f *fakeCreder) GetUserDelegationCredential(ctx context.Context, info service.KeyInfo, o *service.GetUserDelegationCredentialOptions) (*service.UserDelegationCredential, error) {
	expiry, err := time.Parse(sas.TimeFormat, *info.Expiry)
	if err != nil {
		panic(fmt.Sprintf("expiry time is not in the correct format: %s", *info.Expiry))
	}
	start, err := time.Parse(sas.TimeFormat, *info.Start)
	if err != nil {
		panic(fmt.Sprintf("start time is not in the correct format: %s", *info.Start))
	}
	if expiry.Sub(start) != 7*24*time.Hour {
		panic("expiry time is not 7 days")
	}
	if f.err != nil {
		return nil, f.err
	}
	return &service.UserDelegationCredential{}, nil
}

type fakeContClient struct {
	err    error
	called bool
}

func (f *fakeContClient) Create(ctx context.Context, options *container.CreateOptions) (container.CreateResponse, error) {
	f.called = true
	if f.err != nil {
		return container.CreateResponse{}, f.err
	}
	return container.CreateResponse{}, nil
}
