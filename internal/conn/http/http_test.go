package http

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestSetup(t *testing.T) {
	tests := []struct {
		name            string
		endpoint        string
		body            []byte
		wantErr         bool
		wantBody        string
		wantContentType string
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
			name:            "good endpoint",
			endpoint:        "http://localhost:8080",
			body:            []byte("hello"),
			wantBody:        "hello",
			wantContentType: "application/json",
		},
	}

	c := &Client{}
	for _, test := range tests {
		c.endpoint = test.endpoint
		req, err := c.setup(context.Background(), bytes.NewReader(test.body))
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
			t.Fatalf("req.Body: got err == %v, want err == nil", err)
		}
		if string(gotBody) != "hello" {
			t.Fatalf("req.Body: got %s, want %s", gotBody, "hello")
		}
		if req.Raw().Header.Get("Content-Type") != "application/json" {
			t.Fatalf("req.Raw().Header.Get(Content-Type): got %s, want %s", req.Raw().Header.Get("Content-Type"), "application/json")
		}
	}
}
