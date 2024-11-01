package http

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"testing"

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
