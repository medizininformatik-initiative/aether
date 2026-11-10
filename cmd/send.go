package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
	"github.com/medizininformatik-initiative/aether/internal/send"
	"github.com/medizininformatik-initiative/aether/internal/services"
)

// sourceFileName is the name of the source snapshot in the job directory.
const sourceFileName = "source.json"

// sendJobID is the value of the --job flag.
var sendJobID string

var sendCmd = &cobra.Command{
	Use:   "send <config> <dir>",
	Short: "Send the CSV files of a directory",
	Long: `Send the CSV files of a directory with the settings of services.send.

The command needs no CRTDL and no pipeline job. It reads the files in place
and makes no copy of them. It writes nothing into <dir>. It ignores the
pipeline section of the configuration.

Arguments:
  <config>  Path to aether.yaml (required, first positional)
  <dir>     Directory with the CSV files (required, second positional)

The command sends every file below <dir> whose name ends in .csv, in any letter
case. It does not send other files. The command follows symbolic links to
files and to directories. A link loop, a link to nothing, and a directory
without CSV files are errors.

Send modes:
  transfer_load   Each file goes into a zip archive. The entry name in the
                  archive is the path relative to <dir>.
  s3_upload       Each file goes to the S3 bucket. The key is the job ID and
                  the path relative to <dir>.
  direct_resource_load is not available for this command.

Each run creates a job directory under jobs_dir with the job ID, a
source.json file, a job.log file and the upload manifest. The source.json file
records the name, the size and the SHA-256 hash of each file. The command
prints the job ID.

Options:
  --job <job-id>  Resume an earlier run. The command compares the files of
                  <dir> with source.json and stops when they differ. It does
                  nothing when the run is complete. With s3_upload, the
                  upload manifest lets the run skip the files that are
                  already in the bucket.

Examples:
  aether send aether.yaml ./csv
  aether send aether.yaml ./csv --job 20250101_1200_3f2a9c1e-7b4d-4e8a-9c6f-1d2e3f4a5b6c`,
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("usage: aether send <config> <dir> [--job <job-id>] (got %d argument(s))", len(args))
		}
		return nil
	},
	RunE: runSend,
}

func init() {
	sendCmd.Flags().StringVar(&sendJobID, "job", "", "Resume the earlier run with this job ID")
	rootCmd.AddCommand(sendCmd)
}

// runSend sends the CSV files of a directory. It keeps its state in a job
// directory and never writes into the source directory.
func runSend(cmd *cobra.Command, args []string) error {
	cfgPath, dir := args[0], args[1]

	config, files, err := loadSendInput(cfgPath, dir)
	if err != nil {
		return err
	}

	job := sendJob{config: config, cfgPath: cfgPath, dir: dir, files: files}
	if sendJobID != "" {
		err = job.resume(sendJobID)
	} else {
		err = job.start(cmd)
	}
	if err != nil {
		return err
	}
	return job.run(cmd)
}

// loadSendInput loads the configuration and lists the files.
func loadSendInput(cfgPath, dir string) (*models.ProjectConfig, []send.File, error) {
	config, err := services.LoadSendConfig(cfgPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	if err := checkDirectSendConfig(config); err != nil {
		return nil, nil, err
	}
	files, err := send.FindCSVFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	return config, files, nil
}

// sendJob is one run of the send command. source is nil for a resumed run
// until run holds the job lock.
type sendJob struct {
	config  *models.ProjectConfig
	cfgPath string
	dir     string
	files   []send.File
	id      string
	jobDir  string
	source  *send.Source
}

func (j *sendJob) sourcePath() string {
	return filepath.Join(j.jobDir, sourceFileName)
}

// start creates the job directory for a new run and saves the snapshot of the
// files.
func (j *sendJob) start(cmd *cobra.Command) error {
	current, err := send.SnapshotSource(j.dir, j.files)
	if err != nil {
		return err
	}
	j.id = models.GenerateJobID()
	j.jobDir = services.GetJobDir(j.config.JobsDir, j.id)
	if err := os.MkdirAll(j.jobDir, 0o755); err != nil {
		return fmt.Errorf("failed to create job directory: %w", err)
	}
	if err := send.SaveSource(j.sourcePath(), current); err != nil {
		return err
	}
	j.source = &current
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Job ID: %s\n", j.id)
	_, _ = fmt.Fprintf(out, "Resume with: %s\n\n", resumeHint(j.cfgPath, j.dir, j.id))
	return nil
}

// resume selects the earlier run with the given job ID. It checks that the
// job exists, so that the job lock does not create a new job directory.
func (j *sendJob) resume(jobID string) error {
	if err := models.ValidateJobID(jobID); err != nil {
		return fmt.Errorf("invalid job ID %q: %w", jobID, err)
	}
	j.id = jobID
	j.jobDir = services.GetJobDir(j.config.JobsDir, jobID)
	if _, err := os.Stat(j.sourcePath()); err != nil {
		return fmt.Errorf("cannot resume job %s: %w", jobID, err)
	}
	return nil
}

// run sends the files while it holds the job lock. For a resumed run, it reads
// the job state only after it has the lock, because another process can
// complete the job while this process waits.
func (j *sendJob) run(cmd *cobra.Command) error {
	logger := lib.NewLogger(commandLogLevel())
	defer func() { _ = logger.Close() }()
	if err := logger.AttachJobLogFile(services.GetJobLogFilePath(j.config.JobsDir, j.id)); err != nil {
		return fmt.Errorf("failed to attach job log file: %w", err)
	}

	lock, err := services.AcquireJobLock(j.config.JobsDir, j.id, logger)
	if err != nil {
		return fmt.Errorf("cannot send: %w\n\nAnother process may be working on this job", err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			logger.Error("Failed to release job lock", "error", err)
		}
	}()

	if j.source == nil {
		completed, err := j.loadResumedSource()
		if err != nil {
			return err
		}
		if completed {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Job %s is already completed. Nothing to send.\n", j.id)
			return nil
		}
	}

	if _, err := send.Send(context.Background(), send.Request{
		JobID:  j.id,
		Config: j.config.Services.Send,
		Retry:  j.config.Retry,
		TLS:    j.config.TLS,
		Files:  j.files,
		State: send.State{
			ManifestPath:      models.GetUploadManifestPath(j.jobDir),
			FailedRequestsDir: filepath.Join(j.jobDir, "send", "failed"),
		},
	}, logger); err != nil {
		return fmt.Errorf("send failed: %w\n\nResume with: %s", err, resumeHint(j.cfgPath, j.dir, j.id))
	}

	j.source.Completed = true
	if err := send.SaveSource(j.sourcePath(), *j.source); err != nil {
		return fmt.Errorf("send finished, but saving the job state failed: %w", err)
	}
	return nil
}

// loadResumedSource loads the snapshot of a resumed run. If the run is not
// complete, it checks the current files against the snapshot. It reads the
// files only for a run that is not complete.
func (j *sendJob) loadResumedSource() (completed bool, err error) {
	stored, err := send.LoadSource(j.sourcePath())
	if err != nil {
		return false, fmt.Errorf("cannot resume job %s: %w", j.id, err)
	}
	if stored.Completed {
		return true, nil
	}
	current, err := send.SnapshotSource(j.dir, j.files)
	if err != nil {
		return false, err
	}
	if err := stored.Compare(current); err != nil {
		return false, fmt.Errorf("source directory changed since job %s started: %w", j.id, err)
	}
	j.source = &current
	return false, nil
}

// checkDirectSendConfig checks the send settings for the send command. It
// accepts only the modes that send files as they are.
func checkDirectSendConfig(config *models.ProjectConfig) error {
	if err := config.Services.Send.Validate(); err != nil {
		return err
	}
	if config.Services.Send.SendAs == models.SendModeDirectResourceLoad {
		return fmt.Errorf("send_as %q is not available for 'aether send': use %q or %q",
			models.SendModeDirectResourceLoad, models.SendModeTransferLoad, models.SendModeS3Upload)
	}
	return nil
}

// resumeHint returns the command that resumes the job. The paths are absolute
// and quoted for a POSIX shell, so that the command works from every
// directory when a user copies it.
func resumeHint(cfgPath, dir, jobID string) string {
	return fmt.Sprintf("aether send %s %s --job %s", shellPath(cfgPath), shellPath(dir), jobID)
}

func shellPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}
