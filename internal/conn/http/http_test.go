package http

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/kylelemons/godebug/pretty"
)

func TestSetup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		endpoint    string
		headers     []string
		body        []byte
		wantErr     bool
		wantBody    string
		wantHeaders map[string][]string
	}{
		{
			name:     "empty body",
			endpoint: "http://localhost:8080",
			body:     []byte(""),
			wantErr:  true,
		},
		{
			name:    "bad endpoint",
			body:    []byte("hello"),
			wantErr: true,
		},
		{
			name:     "bad headers",
			endpoint: "http://localhost:8080",
			headers:  []string{"onlyOneKeyAndNoValue"},
			wantErr:  true,
		},
		{
			name:     "good endpoint",
			endpoint: "http://localhost:8080",
			body:     []byte("hello"),
			wantBody: "hello",
		},
		{
			name:     "good endpoint with headers",
			endpoint: "http://localhost:8080",
			headers:  []string{"publisherinfo", "whatever"},
			body:     []byte("hello"),
			wantBody: "hello",
			wantHeaders: map[string][]string{
				"publisherinfo": []string{"whatever"},
			},
		},
	}

	c := &Client{}
	for _, test := range tests {
		c.endpoint = test.endpoint
		req, err := c.setup(context.Background(), bytes.NewReader(test.body), test.headers)
		switch {
		case test.wantErr && err == nil:
			t.Errorf("TestSetup(%s): got err == nil, want err != nil", test.name)
			continue
		case !test.wantErr && err != nil:
			t.Errorf("TestSetup(%s): got err == %v, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}

		gotBody, err := io.ReadAll(req.Body())
		if err != nil {
			t.Fatalf("TestSetup(%s): req.Body: got err == %v, want err == nil", test.name, err)
		}
		if string(gotBody) != "hello" {
			t.Fatalf("TestSetup(%s): req.Body: got %s, want %s", test.name, gotBody, "hello")
		}

		if test.wantHeaders == nil {
			test.wantHeaders = map[string][]string{}
		}
		test.wantHeaders["Accept"] = []string{"application/json"}
		test.wantHeaders["Content-Type"] = []string{"application/json"}
		test.wantHeaders["Content-Length"] = []string{strconv.Itoa(len(test.body))}
		if len(test.wantHeaders) != len(req.Raw().Header) {
			diff := pretty.Compare(test.wantHeaders, req.Raw().Header)
			t.Fatalf("TestSetup(%s): -want/+got:\n%s", test.name, diff)
			//t.Fatalf("TestSetup(%s): len(req.Raw().Header): got %d, want %d", test.name, len(req.Raw().Header), len(test.wantHeaders))
		}
		for k, v := range test.wantHeaders {
			if req.Raw().Header.Get(k) != v[0] {
				t.Fatalf("TestSetup(%s): req.Raw().Header.Get(%s): got %s, want %s", test.name, k, req.Raw().Header.Get(k), v)
			}
		}
	}
}

// TestEndpointSuffix pins how the ARN notify suffix is applied. The guard has been wrong twice, and
// both regressions doubled the suffix and 404ed every send: path.Dir returned the parent rather than
// the last segment, and path.Base over the raw endpoint returned that segment with the query still
// attached ("arnnotify?x=1"). The query rows are what hold the current fix in place.
func TestEndpointSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{
			name:     "Success: the suffix is appended when absent",
			endpoint: "https://host.example.com",
			want:     "https://host.example.com/arnnotify",
		},
		{
			name:     "Success: a trailing slash does not produce an empty segment",
			endpoint: "https://host.example.com/",
			want:     "https://host.example.com/arnnotify",
		},
		{
			name:     "Success: the suffix is not doubled when already present",
			endpoint: "https://host.example.com/arnnotify",
			want:     "https://host.example.com/arnnotify",
		},
		{
			name:     "Success: the suffix is not doubled when present alongside a query string",
			endpoint: "https://host.example.com/arnnotify?x=1",
			want:     "https://host.example.com/arnnotify?x=1",
		},
		{
			name:     "Success: a query string survives appending the suffix",
			endpoint: "https://host.example.com?x=1",
			want:     "https://host.example.com/arnnotify?x=1",
		},
		{
			name:     "Success: the suffix is appended under an existing base path",
			endpoint: "https://host.example.com/base",
			want:     "https://host.example.com/base/arnnotify",
		},
		{
			name:     "Error: the endpoint is not a parseable URL",
			endpoint: "https://host.example.com/%zz",
			wantErr:  true,
		},
	}

	for _, test := range tests {
		got, err := notifyEndpoint(test.endpoint)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestEndpointSuffix(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestEndpointSuffix(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
		if got != test.want {
			t.Errorf("TestEndpointSuffix(%s): got %s, want %s", test.name, got, test.want)
		}
	}
}

// TestNewEndpoint pins that New() surfaces a bad endpoint instead of building a client around it.
// notifyEndpoint() used to panic on an unparseable endpoint, so the error it now returns had no way
// of reaching the caller.
func TestNewEndpoint(t *testing.T) {
	t.Parallel()

	cred := struct{ azcore.TokenCredential }{}

	tests := []struct {
		name     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{
			name:     "Success: a good endpoint is normalized onto the client",
			endpoint: "https://host.example.com?x=1",
			want:     "https://host.example.com/arnnotify?x=1",
		},
		{
			name:     "Error: an unparseable endpoint is rejected",
			endpoint: "https://host.example.com/%zz",
			wantErr:  true,
		},
	}

	for _, test := range tests {
		c, err := New(test.endpoint, cred, nil)
		switch {
		case err == nil && test.wantErr:
			t.Errorf("TestNewEndpoint(%s): got err == nil, want err != nil", test.name)
			continue
		case err != nil && !test.wantErr:
			t.Errorf("TestNewEndpoint(%s): got err == %s, want err == nil", test.name, err)
			continue
		case err != nil:
			continue
		}
		if c.endpoint != test.want {
			t.Errorf("TestNewEndpoint(%s): got c.endpoint == %s, want %s", test.name, c.endpoint, test.want)
		}
	}
}
