package unit

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
	assert.True(t, manifest.IsUploaded("a.csv"))
	assert.True(t, manifest.IsUploaded("b.csv"))
	assert.NotNil(t, manifest.CompletedAt)
}
