package http

import (
	"bytes"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
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
		deflateReader := flate.NewReader(r.Body)
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
	http.HandleFunc("/deflate", handler.handleDeflateRequest)

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}

	go func() {
		if err := http.Serve(listener, nil); err != nil {
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
