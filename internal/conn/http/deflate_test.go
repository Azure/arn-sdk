package http

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/arn-sdk/internal/build"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/go-json-experiment/json"
	"github.com/google/uuid"
	"github.com/gostdlib/concurrency/goroutines/limited"
	"github.com/gostdlib/concurrency/prim/wait"
)

type flateData struct {
	Num int
	ID  string
}

type httpHandler struct {
	results        []flateData
	countA, countB atomic.Int32
}

// handleDeflateRequest is an HTTP handler that processes requests with deflate-encoded bodies.
func (h *httpHandler) handleDeflateRequest(w http.ResponseWriter, r *http.Request) {
	// Check if the Content-Encoding is deflate
	if r.Header.Get("Content-Encoding") == "deflate" {
		h.countA.Add(1)
		// Wrap the request body in a flate.Reader to decompress it
		deflateReader, err := zlib.NewReader(r.Body)
		if err != nil {
			panic(err)
		}
		defer deflateReader.Close()

		// Read the decompressed data
		decompressedBody, err := io.ReadAll(deflateReader)
		if err != nil {
			panic(err)
		}

		var f flateData
		if err := json.Unmarshal(decompressedBody, &f); err != nil {
			panic(err)
		}

		h.results[f.Num] = f

		// Send a response
		w.Write([]byte("Successfully processed deflated request"))
		return
	}
	h.countB.Add(1)
	// If not deflate-encoded, handle normally
	w.Write([]byte("Request is not deflate-encoded"))
}

func TestDeflate(t *testing.T) {
	t.Parallel()

	data := make([]flateData, 0, 1000)

	for i := 0; i < 1000; i++ {
		u, err := uuid.NewV7()
		if err != nil {
			panic(err)
		}

		d := flateData{
			Num: i,
			ID:  u.String(),
		}
		data = append(data, d)
	}
	data = append(data, flateData{})

	handler := &httpHandler{results: make([]flateData, len(data)-1)}
	// Set up the HTTP route
	mux := http.NewServeMux()
	mux.HandleFunc("/deflate", handler.handleDeflateRequest)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}

	go func() {
		if err := http.Serve(listener, mux); err != nil {
			fmt.Printf("Failed to start server: %v\n", err)
		}
	}()
	time.Sleep(1 * time.Second)

	endpoint := fmt.Sprintf("http://localhost:%v/deflate", listener.Addr().(*net.TCPAddr).Port)

	plOpts := runtime.PipelineOptions{
		PerRetry: []policy.Policy{
			newFlateTransport(),
		},
	}
	azclient, err := azcore.NewClient("arn.Client", build.Version, plOpts, &policy.ClientOptions{})
	if err != nil {
		panic(err)
	}

	pool, err := limited.New("test", 100)

	wg := wait.Group{
		Pool: pool,
	}

	var count int
	for _, d := range data {
		d := d
		count++
		wg.Go(
			context.Background(),
			func(ctx context.Context) error {
				var b []byte
				if d.ID != "" {
					var err error
					b, err = json.Marshal(d)
					if err != nil {
						return err
					}
				}

				req, err := runtime.NewRequest(context.Background(), http.MethodPost, endpoint)
				if err != nil {
					return err
				}
				req.Raw().Header["Accept"] = appJSON
				req.SetBody(rsc{bytes.NewReader(b)}, "application/json")

				// Send the event to the ARN service.
				resp, err := azclient.Pipeline().Do(req)
				if err != nil {
					return err
				}
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
				}
				return nil
			},
		)
	}

	if err := wg.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(handler.results) != len(data)-1 {
		t.Fatalf("TestDeflate: expected %d results, got %d", len(data)-1, len(handler.results))
	}

	for i, result := range handler.results {
		if result.ID != data[result.Num].ID {
			t.Fatalf("TestDeflate: for result(%d): expected ID %s, got %s", i, data[result.Num].ID, result.ID)
		}
	}
}

// TestDeflateRedirect pins that a 307 replay carries the compressed body. zlibTransport.Do() swaps in a
// compressed Body but used to leave GetBody pointing at azcore's original uncompressed stream, so
// net/http rebuilt the redirected request from the uncompressed JSON while the headers still advertised
// deflate and the compressed length. The transport then broke the connection with
// "ContentLength=N with Body length M" and the send failed after exhausting its retries.
func TestDeflateRedirect(t *testing.T) {
	got := make(chan flateData, 1)
	handlerErr := make(chan error, 4)

	mux := http.NewServeMux()
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		if enc := r.Header.Get("Content-Encoding"); enc != "deflate" {
			handlerErr <- fmt.Errorf("final: Content-Encoding == %q, want \"deflate\"", enc)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		zr, err := zlib.NewReader(r.Body)
		if err != nil {
			handlerErr <- fmt.Errorf("final: zlib.NewReader: %w", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer zr.Close()
		b, err := io.ReadAll(zr)
		if err != nil {
			handlerErr <- fmt.Errorf("final: read: %w", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var f flateData
		if err := json.Unmarshal(b, &f); err != nil {
			handlerErr <- fmt.Errorf("final: unmarshal: %w", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		got <- f
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	plOpts := runtime.PipelineOptions{PerRetry: []policy.Policy{newFlateTransport()}}
	azclient, err := azcore.NewClient("arn.Client", build.Version, plOpts, &policy.ClientOptions{})
	if err != nil {
		t.Fatalf("TestDeflateRedirect: NewClient(): got err == %s, want err == nil", err)
	}

	want := flateData{Num: 7, ID: "redirect-me"}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("TestDeflateRedirect: Marshal(): got err == %s, want err == nil", err)
	}

	req, err := runtime.NewRequest(t.Context(), http.MethodPost, srv.URL+"/redirect")
	if err != nil {
		t.Fatalf("TestDeflateRedirect: NewRequest(): got err == %s, want err == nil", err)
	}
	req.Raw().Header["Accept"] = appJSON
	if err := req.SetBody(rsc{bytes.NewReader(b)}, "application/json"); err != nil {
		t.Fatalf("TestDeflateRedirect: SetBody(): got err == %s, want err == nil", err)
	}

	resp, err := azclient.Pipeline().Do(req)
	if err != nil {
		t.Fatalf("TestDeflateRedirect: Do(): got err == %s, want err == nil", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("TestDeflateRedirect: got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	close(handlerErr)
	for e := range handlerErr {
		t.Errorf("TestDeflateRedirect: %s", e)
	}

	select {
	case f := <-got:
		if f != want {
			t.Errorf("TestDeflateRedirect: got %+v, want %+v", f, want)
		}
	default:
		t.Errorf("TestDeflateRedirect: the redirected request never reached /final intact")
	}
}
