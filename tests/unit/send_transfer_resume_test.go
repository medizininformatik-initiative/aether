package unit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/send"
)

func sendTransfer(t *testing.T, req send.Request) (send.Result, error) {
	t.Helper()
	return send.Send(context.Background(), req, lib.NewLogger(lib.LogLevelError))
}

// sendTransferWarnings sends the request and returns the warnings it logged.
func sendTransferWarnings(t *testing.T, req send.Request) (send.Result, string, error) {
	t.Helper()
	var warnings bytes.Buffer
	result, err := send.Send(context.Background(), req, lib.NewLoggerWithWriter(lib.LogLevelWarn, &warnings))
	return result, warnings.String(), err
}

func TestSend_TransferLoad_ResumeUploadsOnlyMissingBinariesWithTheirFirstIDs(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(2, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstRun := server.binaryIDs()
	require.Len(t, firstRun, 2, "the run stops at the failed Binary")
	assert.Empty(t, server.documentReferencePaths())

	server.startRun(0, false)
	result, err := sendTransfer(t, req)
	require.NoError(t, err)
	assert.Equal(t, 3, result.FilesProcessed)

	secondRun := server.binaryIDs()
	require.Len(t, secondRun, 2)
	assert.Equal(t, firstRun[1], secondRun[0], "the failed file keeps its id")
	assert.NotEqual(t, firstRun[0], secondRun[0])
	assert.NotEqual(t, firstRun[0], secondRun[1])

	assert.Equal(t, []string{
		"Binary/" + firstRun[0],
		"Binary/" + firstRun[1],
		"Binary/" + secondRun[1],
	}, server.documentReferenceAttachmentURLs(t))
}

func TestSend_TransferLoad_ResumeProgressCountsOnlyTheUploadedFiles(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)

	server.startRun(0, false)
	stop := captureStdoutForTest(t)
	_, err = sendTransfer(t, req)
	out := stop()
	require.NoError(t, err)

	assert.Contains(t, out, "  ✓ c.csv (application/zip)\n  Uploading Binary 1/1...\n",
		"the file line comes before its upload line")
	assert.NotContains(t, out, "Uploading Binary 3/3...")
	assert.Contains(t, out, "Transfer complete (1 file sent, 2 already on the server)")
}

func TestSend_TransferLoad_ResumeUploadsAgainAFileWhoseContentChanged(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstRun := server.binaryIDs()

	writeSendFile(t, req.Files[0].Path, "id\nchanged\n")

	server.startRun(0, false)
	_, err = sendTransfer(t, req)
	require.NoError(t, err)

	assert.Equal(t, []string{firstRun[0], firstRun[2]}, server.binaryIDs(),
		"a.csv changed and goes again with its id, b.csv is unchanged and is skipped")
}

func TestSend_TransferLoad_UnreadableFileStopsTheSendBeforeTheFirstUpload(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")
	require.NoError(t, os.Remove(req.Files[2].Path))

	_, err := sendTransfer(t, req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process c.csv")
	assert.Empty(t, server.requestKinds())
}

func TestSend_TransferLoad_ResumeToAnotherServerUploadsAllFiles(t *testing.T) {
	first := newTransferServer(t)
	req := transferRequest(t, first.URL, "a.csv", "b.csv", "c.csv")

	first.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)

	second := newTransferServer(t)
	req.Config = transferSendConfig(second.URL)
	_, warnings, err := sendTransferWarnings(t, req)

	require.NoError(t, err)
	assert.Len(t, second.binaryIDs(), 3)
	assert.Contains(t, warnings, "Transfer manifest is for another job or target")
}

func TestSend_TransferLoad_ResumeToAnotherProjectUploadsAllFiles(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)

	server.startRun(0, false)
	req.Config.Transfer.ProjectIdentifier = "another-project"
	_, warnings, err := sendTransferWarnings(t, req)

	require.NoError(t, err)
	assert.Len(t, server.binaryIDs(), 3)
	assert.Contains(t, warnings, "Transfer manifest is for another job or target")
}

func TestSend_TransferLoad_ResumeOfAnotherJobUploadsAllFiles(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)

	server.startRun(0, false)
	req.JobID = "copied-job"
	_, warnings, err := sendTransferWarnings(t, req)

	require.NoError(t, err)
	assert.Len(t, server.binaryIDs(), 3)
	assert.Contains(t, warnings, "Transfer manifest is for another job or target")
}

func TestSend_TransferLoad_ResumeAfterFailedDocumentReferenceUploadsOnlyTheDocumentReference(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv")

	server.startRun(0, true)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstBinaries := server.binaryIDs()
	require.Len(t, firstBinaries, 2)
	firstDocRefPaths := server.documentReferencePaths()
	require.Len(t, firstDocRefPaths, 1)

	server.startRun(0, false)
	_, err = sendTransfer(t, req)
	require.NoError(t, err)

	assert.Empty(t, server.binaryIDs())
	assert.Equal(t, firstDocRefPaths, server.documentReferencePaths())
	assert.Equal(t, []string{
		"Binary/" + firstBinaries[0],
		"Binary/" + firstBinaries[1],
	}, server.documentReferenceAttachmentURLs(t))
}

func TestSend_TransferLoad_UnwritableManifestDoesNotStopTheSend(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv")
	req.State.TransferManifestPath = filepath.Join(t.TempDir(), "missing", "transfer_manifest.json")

	result, warnings, err := sendTransferWarnings(t, req)

	require.NoError(t, err)
	assert.Equal(t, 2, result.FilesProcessed)
	assert.Contains(t, warnings, "Failed to save transfer manifest")
	assert.Equal(t, []string{"Binary", "Binary", "DocumentReference"}, server.requestKinds())
}

func TestSend_TransferLoad_CorruptManifestUploadsAllFilesWithTheSameIDs(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstRun := server.binaryIDs()
	require.Len(t, firstRun, 3)

	require.NoError(t, os.WriteFile(req.State.TransferManifestPath, []byte("{not json"), 0o644))

	server.startRun(0, false)
	result, warnings, err := sendTransferWarnings(t, req)

	require.NoError(t, err)
	assert.Equal(t, 3, result.FilesProcessed)
	assert.Contains(t, warnings, "Failed to load transfer manifest")
	assert.Equal(t, firstRun, server.binaryIDs())
	assert.Equal(t, []string{
		"Binary/" + firstRun[0],
		"Binary/" + firstRun[1],
		"Binary/" + firstRun[2],
	}, server.documentReferenceAttachmentURLs(t))
}

func TestSend_TransferLoad_DeletedManifestKeepsTheIDsOfTheFailedRun(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv")

	server.startRun(0, true)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstBinaries := server.binaryIDs()
	firstDocRefPaths := server.documentReferencePaths()
	require.Len(t, firstBinaries, 2)
	require.Len(t, firstDocRefPaths, 1)

	require.NoError(t, os.Remove(req.State.TransferManifestPath))

	server.startRun(0, false)
	_, err = sendTransfer(t, req)

	require.NoError(t, err)
	assert.Equal(t, firstBinaries, server.binaryIDs())
	assert.Equal(t, firstDocRefPaths, server.documentReferencePaths())
}

func TestSend_TransferLoad_SendAfterCompletedSendUploadsAllFilesAgainWithTheSameIDs(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv")

	_, err := sendTransfer(t, req)
	require.NoError(t, err)
	firstRun := server.binaryIDs()
	require.Len(t, firstRun, 2)

	server.startRun(0, false)
	_, err = sendTransfer(t, req)

	require.NoError(t, err)
	assert.Equal(t, firstRun, server.binaryIDs())
}

func TestSend_TransferLoad_SameBaseNameInTwoDirectoriesGetsTwoBinaries(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, filepath.Join("a", "Patient.csv"), filepath.Join("b", "Patient.csv"))

	_, err := sendTransfer(t, req)

	require.NoError(t, err)
	ids := server.binaryIDs()
	require.Len(t, ids, 2)
	assert.NotEqual(t, ids[0], ids[1])
	assert.Equal(t, []string{"Binary/" + ids[0], "Binary/" + ids[1]}, server.documentReferenceAttachmentURLs(t))
}

func TestSend_TransferLoad_IDsDependOnJobIDAndFileName(t *testing.T) {
	idsOf := func(jobID string, names ...string) ([]string, []string) {
		server := newTransferServer(t)
		req := transferRequest(t, server.URL, names...)
		req.JobID = jobID
		_, err := sendTransfer(t, req)
		require.NoError(t, err)
		return server.binaryIDs(), server.documentReferencePaths()
	}

	firstBinaries, firstDocRefs := idsOf("job-1", "a.csv", "b.csv")
	againBinaries, againDocRefs := idsOf("job-1", "a.csv", "b.csv")
	otherBinaries, otherDocRefs := idsOf("job-2", "a.csv", "b.csv")

	assert.Equal(t, firstBinaries, againBinaries)
	assert.Equal(t, firstDocRefs, againDocRefs)
	assert.NotEqual(t, firstBinaries[0], firstBinaries[1], "files of one job differ in their ids")
	assert.NotEqual(t, firstBinaries[0], otherBinaries[0])
	assert.NotEqual(t, firstBinaries[1], otherBinaries[1])
	assert.NotEqual(t, firstDocRefs, otherDocRefs)
}

func TestSend_TransferLoad_ManifestRecordsUploadedFilesAndCompletion(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv")

	_, warnings, err := sendTransferWarnings(t, req)
	require.NoError(t, err)
	assert.NotContains(t, warnings, "transfer manifest")

	manifest, err := models.LoadTransferManifest(req.State.TransferManifestPath)
	require.NoError(t, err)
	require.NotNil(t, manifest)
	assert.Equal(t, "job-transfer", manifest.JobID)
	assert.True(t, manifest.IsUploaded("a.csv", fileSHA256Hex(t, req.Files[0].Path)))
	assert.True(t, manifest.IsUploaded("b.csv", fileSHA256Hex(t, req.Files[1].Path)))
	assert.NotNil(t, manifest.CompletedAt)
}

func fileSHA256Hex(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestSend_TransferLoad_ResumeUploadsAgainABinaryThatTheServerDeleted(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstRun := server.binaryIDs()
	server.deleteBinary(firstRun[0])

	server.startRun(0, false)
	_, err = sendTransfer(t, req)
	require.NoError(t, err)

	assert.Equal(t, []string{firstRun[0], firstRun[2]}, server.binaryIDs(),
		"the deleted Binary is uploaded again with its id")
}

func TestSend_TransferLoad_ResumeUploadsAllFilesWhenTheServerCannotSearch(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstRun := server.binaryIDs()

	server.startRun(0, false)
	server.failSearch = true
	_, warnings, err := sendTransferWarnings(t, req)
	require.NoError(t, err)

	assert.Equal(t, firstRun, server.binaryIDs(), "all files are uploaded again with their ids")
	assert.Contains(t, warnings, "Cannot check the Binaries on the server")
}

func TestSend_TransferLoad_ResumeSearchesAtMostFiftyBinariesPerRequest(t *testing.T) {
	server := newTransferServer(t)
	names := make([]string, 51)
	for i := range names {
		names[i] = fmt.Sprintf("f%02d.csv", i)
	}
	req := transferRequest(t, server.URL, names...)

	server.startRun(0, true)
	_, err := sendTransfer(t, req)
	require.Error(t, err)

	server.startRun(0, false)
	_, err = sendTransfer(t, req)
	require.NoError(t, err)

	assert.Empty(t, server.binaryIDs(), "every Binary is found on the server")
	assert.Equal(t, []int{50, 1}, server.searchedIDCounts())
	for _, q := range server.searchQueries() {
		assert.Equal(t, "true", q.Get("_summary"), "the search leaves out Binary.data")
		assert.Equal(t, strconv.Itoa(len(strings.Split(q.Get("_id"), ","))), q.Get("_count"))
	}
}

func TestSend_TransferLoad_ResumeUploadsAllFilesWhenTheSearchResultIsNotJSON(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstRun := server.binaryIDs()

	server.startRun(0, false)
	server.searchBody = "<html>proxy error</html>"
	_, warnings, err := sendTransferWarnings(t, req)
	require.NoError(t, err)

	assert.Equal(t, firstRun, server.binaryIDs(), "all files are uploaded again with their ids")
	assert.Contains(t, warnings, "failed to decode search result")
}

func TestSend_TransferLoad_ResumeUploadsAllFilesWhenTheSearchResultIsTooLarge(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(3, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstRun := server.binaryIDs()

	data := strings.Repeat("A", 600*1024)
	entries := make([]string, 0, len(firstRun))
	for _, id := range firstRun {
		entries = append(entries, fmt.Sprintf(
			`{"resource":{"resourceType":"Binary","id":%q,"contentType":"application/zip","data":%q}}`, id, data))
	}
	server.startRun(0, false)
	server.searchBody = `{"resourceType":"Bundle","type":"searchset","entry":[` + strings.Join(entries, ",") + `]}`
	_, warnings, err := sendTransferWarnings(t, req)
	require.NoError(t, err)

	assert.Equal(t, firstRun, server.binaryIDs(), "all files are uploaded again with their ids")
	assert.Contains(t, warnings, "larger than")
}

func TestSend_TransferLoad_ResumeAcceptsASearchResultOfExactlyTheMaximumSize(t *testing.T) {
	const maxBytes = 1 << 20
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv", "c.csv")

	server.startRun(0, true)
	_, err := sendTransfer(t, req)
	require.Error(t, err)

	entries := make([]string, 0, 3)
	for _, id := range server.binaryIDs() {
		entries = append(entries, fmt.Sprintf(`{"resource":{"resourceType":"Binary","id":%q}}`, id))
	}
	body := `{"resourceType":"Bundle","type":"searchset","entry":[` + strings.Join(entries, ",") + `]}`
	server.startRun(0, false)
	server.searchBody = body + strings.Repeat(" ", maxBytes-len(body))
	require.Len(t, server.searchBody, maxBytes)
	_, warnings, err := sendTransferWarnings(t, req)
	require.NoError(t, err)

	assert.Empty(t, server.binaryIDs(), "the search result is used, so no file is uploaded again")
	assert.NotContains(t, warnings, "larger than")
}

func TestSend_TransferLoad_FileThatVanishesDuringTheSendStopsItAndAResumeCompletesIt(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv")
	saved, err := os.ReadFile(req.Files[1].Path)
	require.NoError(t, err)

	server.onBinaryPut = func() { _ = os.Remove(req.Files[1].Path) }
	_, err = sendTransfer(t, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process b.csv")
	firstRun := server.binaryIDs()
	require.Len(t, firstRun, 1)
	assert.Empty(t, server.documentReferencePaths())

	server.onBinaryPut = nil
	require.NoError(t, os.WriteFile(req.Files[1].Path, saved, 0o644))
	server.startRun(0, false)
	_, err = sendTransfer(t, req)
	require.NoError(t, err)

	secondRun := server.binaryIDs()
	require.Len(t, secondRun, 1, "a.csv is on the server and is skipped")
	assert.Equal(t, []string{"Binary/" + firstRun[0], "Binary/" + secondRun[0]},
		server.documentReferenceAttachmentURLs(t))
}

func TestSend_TransferLoad_ResumeOfFilesWithoutANameUsesTheirPath(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv")
	for i := range req.Files {
		req.Files[i].Name = ""
	}

	server.startRun(2, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)
	firstRun := server.binaryIDs()
	require.Len(t, firstRun, 2)
	require.NotEqual(t, firstRun[0], firstRun[1])

	server.startRun(0, false)
	_, err = sendTransfer(t, req)
	require.NoError(t, err)

	assert.Equal(t, []string{firstRun[1]}, server.binaryIDs(), "a.csv is skipped, b.csv goes again with its id")
}

func TestSend_TransferLoad_ResumeStopsBeforeTheFirstUploadWhenARecordedFileIsCorrupt(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "b.csv")
	path := filepath.Join(filepath.Dir(req.Files[0].Path), "a.csv.zst")
	writeZstdFile(t, path, "id\n1\n")
	req.Files = append([]send.File{{Path: path, Name: "a.csv.zst"}}, req.Files...)

	server.startRun(2, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)

	require.NoError(t, os.WriteFile(path, []byte("not zstd"), 0o644))
	server.startRun(0, false)
	_, err = sendTransfer(t, req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process a.csv.zst")
	assert.Empty(t, server.requestKinds())
}

func TestSend_TransferLoad_ResumeStopsBeforeTheFirstUploadWhenARecordedFileIsGone(t *testing.T) {
	server := newTransferServer(t)
	req := transferRequest(t, server.URL, "a.csv", "b.csv")

	server.startRun(2, false)
	_, err := sendTransfer(t, req)
	require.Error(t, err)

	require.NoError(t, os.Remove(req.Files[0].Path))
	server.startRun(0, false)
	_, err = sendTransfer(t, req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process a.csv")
	assert.Empty(t, server.requestKinds())
}

func TestSend_TransferLoad_ResumeComparesTheDecompressedContentOfACompressedFile(t *testing.T) {
	resume := func(t *testing.T, change bool) (firstRun, secondRun []string) {
		server := newTransferServer(t)
		req := transferRequest(t, server.URL, "b.csv")
		path := filepath.Join(filepath.Dir(req.Files[0].Path), "a.csv.zst")
		writeZstdFile(t, path, "id\n1\n")
		req.Files = append([]send.File{{Path: path, Name: "a.csv.zst"}}, req.Files...)

		server.startRun(2, false)
		_, err := sendTransfer(t, req)
		require.Error(t, err)
		firstRun = server.binaryIDs()

		if change {
			writeZstdFile(t, path, "id\n2\n")
		}
		server.startRun(0, false)
		_, err = sendTransfer(t, req)
		require.NoError(t, err)
		return firstRun, server.binaryIDs()
	}

	firstRun, secondRun := resume(t, false)
	assert.Equal(t, []string{firstRun[1]}, secondRun, "the unchanged a.csv.zst is skipped")

	firstRun, secondRun = resume(t, true)
	assert.Equal(t, firstRun, secondRun, "the changed a.csv.zst goes again with its id")
}

func writeZstdFile(t *testing.T, path, content string) {
	t.Helper()
	w, err := lib.CreateCompressedFileWriter(path, "fastest")
	require.NoError(t, err)
	_, err = w.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, w.Close())
}
