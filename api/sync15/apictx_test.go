package sync15

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juruen/rmapi/archive"
	"github.com/juruen/rmapi/config"
	"github.com/juruen/rmapi/model"
	"github.com/juruen/rmapi/transport"
)

const testDocID = "1b6e2a52-0d1f-4a8b-9f53-4e7c2b7d9a10"

// recordingCloud stores uploaded blobs and accepts every root update.
type recordingCloud struct {
	mu    sync.Mutex
	blobs map[string][]byte
	names map[string]string
	roots int
}

func (c *recordingCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/files/"):
		body, _ := io.ReadAll(r.Body)
		hash := strings.TrimPrefix(r.URL.Path, "/files/")
		c.blobs[hash] = body
		c.names[r.Header.Get(transport.RmFileNameHeader)] = hash
	case r.Method == http.MethodPut && r.URL.Path == "/root":
		var req model.BlobRootStorageRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		c.roots++
		_ = json.NewEncoder(w).Encode(model.BlobRootStorageResponse{Hash: req.Hash, Generation: req.Generation + 1})
	default:
		http.NotFound(w, r)
	}
}

func (c *recordingCloud) blob(t *testing.T, name string) []byte {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	hash, ok := c.names[name]
	if !ok {
		t.Fatalf("%s was not uploaded", name)
	}
	return c.blobs[hash]
}

func newRecordingCtx(t *testing.T) (*ApiCtx, *recordingCloud, *BlobDoc) {
	t.Helper()
	cloud := &recordingCloud{blobs: map[string][]byte{}, names: map[string]string{}}
	server := httptest.NewServer(cloud)
	t.Cleanup(server.Close)
	oldBlobURL, oldRootPut := config.BlobUrl, config.RootPut
	config.BlobUrl, config.RootPut = server.URL+"/files/", server.URL+"/root"
	t.Cleanup(func() { config.BlobUrl, config.RootPut = oldBlobURL, oldRootPut })

	doc := &BlobDoc{
		Entry: Entry{DocumentID: testDocID, Type: DocType},
		Metadata: archive.MetadataFile{
			DocName:        "Report.pdf",
			CollectionType: model.DocumentType,
			LastModified:   "1700000000000",
			Version:        3,
		},
		Files: []*Entry{
			{DocumentID: testDocID + ".content", Hash: strings.Repeat("a", 64), Type: FileType, Size: 10},
			{DocumentID: testDocID + ".metadata", Hash: strings.Repeat("b", 64), Type: FileType, Size: 20},
			{DocumentID: testDocID + ".pdf", Hash: strings.Repeat("c", 64), Type: FileType, Size: 30},
		},
	}
	doc.updateSize()
	if err := doc.Rehash(); err != nil {
		t.Fatal(err)
	}
	tree := &HashTree{Docs: []*BlobDoc{doc}}
	if err := tree.Rehash(); err != nil {
		t.Fatal(err)
	}
	httpCtx := transport.CreateHttpClientCtx(model.AuthTokens{})
	ctx := &ApiCtx{Http: &httpCtx, blobStorage: NewBlobStorage(&httpCtx), hashTree: tree, cacheDir: t.TempDir()}
	return ctx, cloud, doc
}

func assertStampedMetadata(t *testing.T, cloud *recordingCloud, modified time.Time, version int) {
	t.Helper()
	var metadata archive.MetadataFile
	if err := json.Unmarshal(cloud.blob(t, testDocID+".metadata"), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Version != version || metadata.LastModified != strconv.FormatInt(modified.UnixMilli(), 10) || metadata.DocName != "Report.pdf" {
		t.Fatalf("uploaded metadata = %+v", metadata)
	}
}

func TestReplaceDocumentFileAtStampsMetadataInOneRootUpdate(t *testing.T) {
	ctx, cloud, doc := newRecordingCtx(t)
	source := filepath.Join(t.TempDir(), "Report.pdf.pdf")
	payload := []byte("%PDF-1.7 replaced")
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.UnixMilli(1764072245123)

	updated, err := ctx.ReplaceDocumentFileAt(testDocID, source, modified, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 4 || updated.ModifiedClient != "2025-11-25T12:04:05Z" {
		t.Fatalf("updated document = %+v", updated)
	}
	sum := sha256.Sum256(payload)
	if got := cloud.blob(t, testDocID+".pdf"); string(got) != string(payload) {
		t.Fatalf("uploaded payload = %q", got)
	}
	if hash := cloud.names[testDocID+".pdf"]; hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("payload hash = %q", hash)
	}
	assertStampedMetadata(t, cloud, modified, 4)
	if cloud.roots != 1 {
		t.Fatalf("root updates = %d, want 1", cloud.roots)
	}
	var total int64
	for _, f := range doc.Files {
		total += f.Size
	}
	if doc.Size != total {
		t.Fatalf("document size = %d, want %d", doc.Size, total)
	}
	if index := string(cloud.blob(t, testDocID+".docSchema")); !strings.Contains(index, hex.EncodeToString(sum[:])) {
		t.Fatalf("document index does not reference the new payload:\n%s", index)
	}
}

func TestSetLastModifiedLeavesFilesUntouched(t *testing.T) {
	ctx, cloud, doc := newRecordingCtx(t)
	modified := time.UnixMilli(1764072245123)

	updated, err := ctx.SetLastModified(testDocID, modified, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 4 {
		t.Fatalf("updated version = %d, want 4", updated.Version)
	}
	assertStampedMetadata(t, cloud, modified, 4)
	if _, uploaded := cloud.names[testDocID+".pdf"]; uploaded {
		t.Fatal("SetLastModified uploaded the document payload")
	}
	if pdf, _ := findFileEntry(doc, "pdf"); pdf.Hash != strings.Repeat("c", 64) {
		t.Fatalf("payload entry changed to %q", pdf.Hash)
	}
}

func TestReplaceDocumentFileAtRejectsMissingFormat(t *testing.T) {
	ctx, cloud, _ := newRecordingCtx(t)
	source := filepath.Join(t.TempDir(), "Report.epub")
	if err := os.WriteFile(source, []byte("epub"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ctx.ReplaceDocumentFileAt(testDocID, source, time.Now(), false); err == nil {
		t.Fatal("replacing a PDF document with an EPUB succeeded")
	}
	if len(cloud.blobs) != 0 || cloud.roots != 0 {
		t.Fatalf("rejected replacement uploaded %d blobs and %d roots", len(cloud.blobs), cloud.roots)
	}
}
