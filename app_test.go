package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateJobRejectsIncompleteInput(t *testing.T) {
	_, err := normalizeJob(SyncJob{Name: "备份", Source: "/tmp/a", Schedule: "0 2 * * *"})
	if err == nil {
		t.Fatalf("expected missing destination to be rejected")
	}
}

func TestValidateJobAcceptsPresetEveryHour(t *testing.T) {
	job, err := normalizeJob(SyncJob{Name: "备份", Source: "/tmp/a", Destination: "/tmp/b", Schedule: "@hourly", Options: "-av --delete"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Schedule != "@hourly" {
		t.Fatalf("schedule changed: %q", job.Schedule)
	}
	if job.Options != "-av --delete" {
		t.Fatalf("options changed: %q", job.Options)
	}
}

func TestNormalizeJobBuildsEndpointsFromMachineAndPath(t *testing.T) {
	job, err := normalizeJob(SyncJob{
		Name:               "nas backup",
		SourceMachine:      localMachineID,
		SourcePath:         "/home/me/docs",
		DestinationMachine: "nas",
		DestinationPath:    "/volume1/backup/docs",
		Schedule:           "@daily",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Source != "/home/me/docs" {
		t.Fatalf("local source endpoint = %q", job.Source)
	}
	if job.Destination != "nas:/volume1/backup/docs" {
		t.Fatalf("remote destination endpoint = %q", job.Destination)
	}
}

func TestParseSSHConfigHosts(t *testing.T) {
	config := `
Host nas pi pi-lan
  HostName 192.168.1.2
Host *
  ServerAliveInterval 60
Host github.com
  User git
`
	hosts := parseSSHConfigHosts(config)
	joined := strings.Join(hosts, ",")
	for _, want := range []string{"nas", "pi", "pi-lan", "github.com"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing host %q in %v", want, hosts)
		}
	}
	if strings.Contains(joined, "*") {
		t.Fatalf("wildcard host should be ignored: %v", hosts)
	}
}

func TestSSHConfigPathUsesUserHomeDir(t *testing.T) {
	got := sshConfigPathFromEnv(func(string) string { return "" }, func() (string, error) { return filepath.Join("C:", "Users", "lee"), nil })
	want := filepath.Join("C:", "Users", "lee", ".ssh", "config")
	if got != want {
		t.Fatalf("ssh config path = %q, want %q", got, want)
	}
}

func TestSSHConfigPathFallsBackToWindowsUserProfile(t *testing.T) {
	env := map[string]string{"USERPROFILE": filepath.Join("C:", "Users", "lee")}
	got := sshConfigPathFromEnv(func(key string) string { return env[key] }, func() (string, error) { return "", os.ErrNotExist })
	want := filepath.Join("C:", "Users", "lee", ".ssh", "config")
	if got != want {
		t.Fatalf("ssh config path = %q, want %q", got, want)
	}
}

func TestSSHConfigPathFallsBackToHomeDriveHomePath(t *testing.T) {
	env := map[string]string{"HOMEDRIVE": "C:", "HOMEPATH": `\Users\lee`}
	got := sshConfigPathFromEnv(func(key string) string { return env[key] }, func() (string, error) { return "", os.ErrNotExist })
	if !strings.Contains(got, "Users") || !strings.HasSuffix(got, filepath.Join(".ssh", "config")) {
		t.Fatalf("unexpected ssh config path: %q", got)
	}
}

func TestBuildCronLineContainsMarkerAndLogPath(t *testing.T) {
	job, err := normalizeJob(SyncJob{ID: "job-123", Name: "docs", Source: "/home/me/a", Destination: "/mnt/b", Schedule: "*/15 * * * *", Options: "-az"})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	line := buildCronLine(job, "/tmp/rt/logs/job-123.log")
	for _, want := range []string{"*/15 * * * *", "rsync -az", "/home/me/a", "/mnt/b", ">> '/tmp/rt/logs/job-123.log' 2>&1", rtCronMarker + "job-123"} {
		if !strings.Contains(line, want) {
			t.Fatalf("cron line missing %q in: %s", want, line)
		}
	}
}

func TestBackupDirectoryNameUsesNestedSourceHostTaskAndTimestamp(t *testing.T) {
	job := SyncJob{Name: "备份 banana", SourceMachine: "v2fy"}
	got := backupDirectoryName(job, "20260519_113000")
	want := "v2fy/备份_banana/20260519_113000"
	if got != want {
		t.Fatalf("backupDirectoryName() = %q, want %q", got, want)
	}
}

func TestBuildRunScriptWritesIntoTimestampedBackupDirectory(t *testing.T) {
	job, err := normalizeJob(SyncJob{Name: "备份 banana", SourceMachine: "v2fy", SourcePath: "/opt/banana", DestinationMachine: localMachineID, DestinationPath: "/backup", Schedule: "@hourly", Options: "-az"})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	script := buildRunScript(job, "/tmp/rt/logs/job-123.log")
	for _, want := range []string{"backup_dir='v2fy/备份_banana'/$(date '+%Y%m%d_%H%M%S')", "mkdir -p '/backup/v2fy/备份_banana'", "rsync -az --info=progress2 --outbuf=L 'v2fy:/opt/banana' '/backup'/$backup_dir"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q in: %s", want, script)
		}
	}
}

func TestEnsureRsyncProgressOptions(t *testing.T) {
	got := ensureRsyncProgressOptions("-az")
	for _, want := range []string{"-az", "--info=progress2", "--outbuf=L"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ensureRsyncProgressOptions missing %q in %q", want, got)
		}
	}
	got = ensureRsyncProgressOptions("-az --info=progress2 --outbuf=L")
	if strings.Count(got, "--info=progress2") != 1 || strings.Count(got, "--outbuf=L") != 1 {
		t.Fatalf("progress options duplicated: %q", got)
	}
}

func TestParseRsyncProgressPercent(t *testing.T) {
	line := "     12.34M  42%   10.20MB/s    0:00:01 (xfr#3, to-chk=4/10)"
	got, ok := parseRsyncProgressPercent(line)
	if !ok || got != 42 {
		t.Fatalf("parseRsyncProgressPercent() = %d, %v; want 42, true", got, ok)
	}
}

func TestWindowsLocalPathForRsyncConvertsDrivePath(t *testing.T) {
	got := rsyncEndpointForOS(`C:\Users\zhaoolee\Documents`, "windows")
	want := "/c/Users/zhaoolee/Documents"
	if got != want {
		t.Fatalf("windows rsync path = %q, want %q", got, want)
	}
}

func TestWindowsLocalPathForRsyncKeepsRemoteEndpoint(t *testing.T) {
	got := rsyncEndpointForOS("oracle:/home/ubuntu/clash-sub", "windows")
	want := "oracle:/home/ubuntu/clash-sub"
	if got != want {
		t.Fatalf("remote endpoint changed = %q, want %q", got, want)
	}
}

func TestPathWithToolDirPrependsPath(t *testing.T) {
	got := pathWithToolDir([]string{"Path=C:\\Windows", "OTHER=1"}, `C:\rt\rsync\bin`)
	if got[0] != `Path=C:\rt\rsync\bin`+string(os.PathListSeparator)+`C:\Windows` {
		t.Fatalf("PATH not prepended: %#v", got)
	}
}

func TestRunShellScriptToLogAppendsOutput(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "rt.log")
	err := runShellScriptToLog("echo hello-from-rt", logFile)
	if err != nil {
		t.Fatalf("runShellScriptToLog: %v", err)
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "hello-from-rt") {
		t.Fatalf("expected command output in log, got %q", string(data))
	}
}

func TestRunShellScriptToLogWithProgressEmitsPercent(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "rt.log")
	percents := []int{}
	err := runShellScriptToLogWithProgress("printf '       10  10%% x\\r       90  90%% x\\n'", logFile, "job-1", func(jobID string, percent int, text string, state string) {
		if jobID != "job-1" {
			t.Fatalf("jobID = %q, want job-1", jobID)
		}
		percents = append(percents, percent)
	})
	if err != nil {
		t.Fatalf("runShellScriptToLogWithProgress: %v", err)
	}
	if len(percents) != 2 || percents[0] != 10 || percents[1] != 90 {
		t.Fatalf("percents = %v, want [10 90]", percents)
	}
}

func TestPathDialogTitleMatchesHyperBackupStyle(t *testing.T) {
	cases := map[string]string{
		"source":      "选择来源文件夹",
		"destination": "选择目标文件夹",
		"other":       "选择文件夹",
	}
	for kind, want := range cases {
		if got := pathDialogTitle(kind); got != want {
			t.Fatalf("pathDialogTitle(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestExistingDirectoryOrHomeFallsBackForMissingPath(t *testing.T) {
	missing := "/definitely/not/exist/rt"
	got := existingDirectoryOrHome(missing)
	if got == missing {
		t.Fatalf("expected missing directory to fall back, got %q", got)
	}
	if got == "" {
		t.Fatalf("expected non-empty fallback directory")
	}
}
