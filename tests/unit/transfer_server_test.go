package unit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/send"
)

// transferServer is a transfer server that records every PUT. It answers 400
// for the Binary PUT with the number failBinaryPut (1-based, per run) and for
// every DocumentReference PUT when failDocumentRefs is set.
type transferServer struct {
	*httptest.Server

	mu                   sync.Mutex
	failBinaryPut        int
	failDocumentRefs     bool
	kinds                []string
	binaryPaths          []string
	documentReferencePut []string
	documentReferences   []map[string]any
	stored               map[string]bool
	searches             []url.Values
	failSearch           bool
	// searchBody, when set, is the raw body of every search answer.
	searchBody string
	// onBinaryPut, when set, runs before the server answers a Binary PUT.
	onBinaryPut func()
}

func newTransferServer(t *testing.T) *transferServer {
	t.Helper()
	ts := &transferServer{stored: map[string]bool{}}
	ts.Server = httptest.NewServer(http.HandlerFunc(ts.handle))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *transferServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		ts.search(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var res map[string]any
	_ = json.Unmarshal(body, &res)

	ts.mu.Lock()
	defer ts.mu.Unlock()

	kind, _ := res["resourceType"].(string)
	ts.kinds = append(ts.kinds, kind)
	switch kind {
	case "Binary":
		if ts.onBinaryPut != nil {
			ts.onBinaryPut()
		}
		ts.binaryPaths = append(ts.binaryPaths, r.URL.Path)
		if len(ts.binaryPaths) == ts.failBinaryPut {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ts.stored[strings.TrimPrefix(r.URL.Path, "/fhir/Binary/")] = true
	case "DocumentReference":
		ts.documentReferencePut = append(ts.documentReferencePut, r.URL.Path)
		if ts.failDocumentRefs {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ts.documentReferences = append(ts.documentReferences, res)
	}
	w.WriteHeader(http.StatusOK)
}

// search answers a Binary search by _id with a searchset of the stored
// Binaries. With failSearch set, it rejects the search as a server does that
// does not support it.
func (ts *transferServer) search(w http.ResponseWriter, r *http.Request) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	query := r.URL.Query()
	ts.searches = append(ts.searches, query)
	if ts.failSearch || r.URL.Path != "/fhir/Binary" {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"not-supported"}]}`))
		return
	}
	if ts.searchBody != "" {
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(ts.searchBody))
		return
	}
	entries := []map[string]any{}
	for _, id := range strings.Split(query.Get("_id"), ",") {
		if ts.stored[id] {
			entries = append(entries, map[string]any{
				"resource": map[string]any{"resourceType": "Binary", "id": id},
			})
		}
	}
	w.Header().Set("Content-Type", "application/fhir+json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"resourceType": "Bundle",
		"type":         "searchset",
		"entry":        entries,
	})
}

// deleteBinary removes a stored Binary, as a cleanup on the server does.
func (ts *transferServer) deleteBinary(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.stored, id)
}

// startRun clears the record of the last run and sets how the next run fails.
func (ts *transferServer) startRun(failBinaryPut int, failDocumentRefs bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.failBinaryPut = failBinaryPut
	ts.failDocumentRefs = failDocumentRefs
	ts.kinds = nil
	ts.binaryPaths = nil
	ts.documentReferencePut = nil
	ts.documentReferences = nil
}

func (ts *transferServer) requestKinds() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]string(nil), ts.kinds...)
}

func (ts *transferServer) binaryIDs() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ids := make([]string, len(ts.binaryPaths))
	for i, p := range ts.binaryPaths {
		ids[i] = strings.TrimPrefix(p, "/fhir/Binary/")
	}
	return ids
}

func (ts *transferServer) documentReferencePaths() []string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]string(nil), ts.documentReferencePut...)
}

func (ts *transferServer) documentReferenceAttachmentURLs(t *testing.T) []string {
	t.Helper()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	require.Len(t, ts.documentReferences, 1)
	var urls []string
	for _, c := range ts.documentReferences[0]["content"].([]any) {
		urls = append(urls, c.(map[string]any)["attachment"].(map[string]any)["url"].(string))
	}
	return urls
}

// transferState returns the state paths of a transfer_load send in dir.
func transferState(dir string) send.State {
	return send.State{
		TransferManifestPath: filepath.Join(dir, "transfer_manifest.json"),
		FailedRequestsDir:    filepath.Join(dir, "failed"),
	}
}

// transferRequest writes one file per name and returns a transfer_load
// request for them.
func transferRequest(t *testing.T, url string, names ...string) send.Request {
	t.Helper()
	dir := t.TempDir()
	files := make([]send.File, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		writeSendFile(t, path, "id\n"+name+"\n")
		files = append(files, send.File{Path: path, Name: name})
	}
	return send.Request{
		JobID:  "job-transfer",
		Config: transferSendConfig(url),
		Retry:  FastRetryConfig(),
		Files:  files,
		State:  transferState(dir),
	}
}

// searchQueries returns the query of each Binary search.
func (ts *transferServer) searchQueries() []url.Values {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]url.Values(nil), ts.searches...)
}

// searchedIDCounts returns the number of ids in each Binary search.
func (ts *transferServer) searchedIDCounts() []int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	counts := make([]int, len(ts.searches))
	for i, q := range ts.searches {
		counts[i] = len(strings.Split(q.Get("_id"), ","))
	}
	return counts
}
