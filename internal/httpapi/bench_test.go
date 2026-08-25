// This file benchmarks the HTTP layer itself — product spec §11's
// indexed-detail and first-page-list targets are stated for *server*
// latency, which internal/store's and internal/service's own
// benchmarks don't measure (they call Go functions directly, skipping
// routing, auth resolution, and JSON encoding). Never invoked by a
// plain `go test` run — only `-bench` triggers the lazy full-scale
// fixture below, the same pattern internal/store/bench_test.go and
// internal/service/bench_test.go use.
package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ArloB/tickets/internal/blobstore"
	"github.com/ArloB/tickets/internal/domain"
	"github.com/ArloB/tickets/internal/fixtures"
	"github.com/ArloB/tickets/internal/service"
	"github.com/ArloB/tickets/internal/store"
)

var (
	benchServerOnce      sync.Once
	benchServerURL       string
	benchServerSummary   fixtures.Summary
	benchServerAttachID  int64
	benchServerDir       string
	benchServerHTTP      *httptest.Server
	benchServerSetupErr  error
	benchAttachmentBytes = strings.Repeat("attachment benchmark payload line.\n", 30000) // ~1.1MB
)

// benchServer lazily builds product spec §11's reference dataset,
// wraps it in a real *service.Service and a real net/http server
// (anonymousRead: true, so GET benchmarks don't need session/CSRF
// plumbing — every route this file benchmarks is routeViewer), and
// creates one upload attachment for the streaming benchmark. Shared
// across every benchmark in this file via sync.Once, cleaned up by
// TestMain.
func benchServer(b *testing.B) (string, fixtures.Summary, int64) {
	b.Helper()
	benchServerOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tickets-http-bench-*")
		if err != nil {
			benchServerSetupErr = err
			return
		}
		benchServerDir = dir
		st, err := store.Open(dir)
		if err != nil {
			benchServerSetupErr = err
			return
		}
		sum, err := fixtures.Generate(context.Background(), st, 42, fixtures.Full)
		if err != nil {
			benchServerSetupErr = err
			return
		}
		blobs, err := blobstore.Open(dir)
		if err != nil {
			benchServerSetupErr = err
			return
		}
		svc := service.New(st, blobs)

		ref, err := domain.Parse(sum.SampleTicketRef)
		if err != nil {
			benchServerSetupErr = err
			return
		}
		actor := domain.ActorRef{Kind: domain.ActorSystem, Name: "system"}
		att, err := svc.CreateAttachment(context.Background(), service.CreateAttachmentRequest{
			Ref: ref, Title: "Benchmark attachment", Kind: domain.AttachmentKindUpload,
			Content: strings.NewReader(benchAttachmentBytes), FileName: "bench.txt", MediaType: "text/plain",
		}, actor, "bench-attachment-setup")
		if err != nil {
			benchServerSetupErr = err
			return
		}

		ts := httptest.NewServer(NewHandler(svc, true))
		benchServerHTTP = ts
		benchServerURL = ts.URL
		benchServerSummary = sum
		benchServerAttachID = att.ID
	})
	if benchServerSetupErr != nil {
		b.Fatalf("build HTTP bench server: %v", benchServerSetupErr)
	}
	return benchServerURL, benchServerSummary, benchServerAttachID
}

func TestMain(m *testing.M) {
	code := m.Run()
	if benchServerHTTP != nil {
		benchServerHTTP.Close()
	}
	if benchServerDir != "" {
		_ = os.RemoveAll(benchServerDir)
	}
	os.Exit(code)
}

// benchGet performs one GET and discards the body, failing the
// benchmark on a non-200 or a read error — every route benchmarked
// here is a plain read with no side effects to isolate.
func benchGet(b *testing.B, client *http.Client, url string) {
	b.Helper()
	resp, err := client.Get(url)
	if err != nil {
		b.Fatalf("GET %s: %v", url, err)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		b.Fatalf("read body: %v", err)
	}
	if closeErr != nil {
		b.Fatalf("close body: %v", closeErr)
	}
	if resp.StatusCode != http.StatusOK {
		b.Fatalf("GET %s: status %d, want 200", url, resp.StatusCode)
	}
}

// BenchmarkHTTPGetTicket is §11's "indexed detail fetch" target
// (p95 < 100ms), measured at the HTTP layer rather than the store
// layer — routing, auth resolution, and JSON encoding included.
func BenchmarkHTTPGetTicket(b *testing.B) {
	url, sum, _ := benchServer(b)
	client := &http.Client{}
	target := url + "/api/v1/tickets/" + sum.SampleTicketRef

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchGet(b, client, target)
	}
}

// BenchmarkHTTPListTicketsFirstPage is §11's "first-page list" target
// at the HTTP layer, against the sample project's priority queue
// (4,000 tickets).
func BenchmarkHTTPListTicketsFirstPage(b *testing.B) {
	url, sum, _ := benchServer(b)
	client := &http.Client{}
	target := url + "/api/v1/projects/" + sum.SampleProjectKey + "/tickets"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchGet(b, client, target)
	}
}

// BenchmarkHTTPAttachmentDownload is §11's requirement extended to
// attachment streaming (plan.md Phase 6 Step 6's explicit carry-in) —
// a ~1.1MB upload attachment, streamed back through the real download
// route and blobstore, not read from an in-memory buffer.
func BenchmarkHTTPAttachmentDownload(b *testing.B) {
	url, _, attachID := benchServer(b)
	client := &http.Client{}
	target := url + "/api/v1/attachments/" + strconv.FormatInt(attachID, 10) + "/download"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchGet(b, client, target)
	}
}
