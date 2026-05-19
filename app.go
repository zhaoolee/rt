package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const rtCronMarker = "# rt:"
const localMachineID = "local"

// App struct
type App struct {
	ctx context.Context
}

type SyncJob struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Source             string `json:"source"`
	Destination        string `json:"destination"`
	SourceMachine      string `json:"sourceMachine"`
	SourcePath         string `json:"sourcePath"`
	DestinationMachine string `json:"destinationMachine"`
	DestinationPath    string `json:"destinationPath"`
	Schedule           string `json:"schedule"`
	Options            string `json:"options"`
	Enabled            bool   `json:"enabled"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
	LastRunAt          string `json:"lastRunAt"`
}

type Machine struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
}

type DirectoryEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type LogEntry struct {
	JobID    string `json:"jobId"`
	JobName  string `json:"jobName"`
	LogPath  string `json:"logPath"`
	Content  string `json:"content"`
	Modified string `json:"modified"`
	Size     int64  `json:"size"`
}

type Status struct {
	CrontabAvailable bool   `json:"crontabAvailable"`
	RsyncAvailable   bool   `json:"rsyncAvailable"`
	StoreDir         string `json:"storeDir"`
	Message          string `json:"message"`
}

type SyncProgress struct {
	JobID   string `json:"jobId"`
	Percent int    `json:"percent"`
	Text    string `json:"text"`
	State   string `json:"state"`
}

// NewApp creates a new App application struct
func NewApp() *App { return &App{} }

// startup is called when the app starts. The context is saved.
func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) GetStatus() Status {
	_, cronErr := exec.LookPath("crontab")
	_, rsyncErr := findRsyncExecutable()
	message := "就绪"
	if runtime.GOOS == "windows" {
		cronErr = nil
		message = "Windows 版已内置 rsync；系统定时任务后续接入任务计划程序"
	}
	if runtime.GOOS != "windows" {
		if cronErr != nil && rsyncErr != nil {
			message = "未找到 crontab 和 rsync，请先安装"
		} else if cronErr != nil {
			message = "未找到 crontab，定时任务不可用"
		} else if rsyncErr != nil {
			message = "未找到 rsync，同步执行不可用"
		}
	} else if rsyncErr != nil {
		message = "未找到 rsync：请下载新版完整包，或把 rsync.exe 加入 PATH"
	}
	return Status{CrontabAvailable: cronErr == nil, RsyncAvailable: rsyncErr == nil, StoreDir: appDir(), Message: message}
}

func (a *App) ListJobs() ([]SyncJob, error) { return loadJobs() }

func (a *App) ListMachines() ([]Machine, error) { return listMachines() }

func (a *App) ListDirectories(machineID string, current string) ([]DirectoryEntry, error) {
	return listDirectories(machineID, current)
}

func (a *App) SelectPath(kind string, current string) (string, error) {
	if a.ctx == nil {
		return "", errors.New("应用尚未准备好")
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:                pathDialogTitle(kind),
		DefaultDirectory:     existingDirectoryOrHome(current),
		CanCreateDirectories: true,
		ShowHiddenFiles:      true,
	})
}

func (a *App) SaveJob(job SyncJob) (SyncJob, error) {
	jobs, err := loadJobs()
	if err != nil {
		return SyncJob{}, err
	}
	now := time.Now().Format(time.RFC3339)
	job, err = normalizeJob(job)
	if err != nil {
		return SyncJob{}, err
	}
	if job.ID == "" {
		job.ID = newID()
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	if job.Options == "" {
		job.Options = "-avh --delete"
	}

	found := false
	for i := range jobs {
		if jobs[i].ID == job.ID {
			if job.CreatedAt == "" {
				job.CreatedAt = jobs[i].CreatedAt
			}
			if job.LastRunAt == "" {
				job.LastRunAt = jobs[i].LastRunAt
			}
			jobs[i] = job
			found = true
			break
		}
	}
	if !found {
		jobs = append(jobs, job)
	}
	if err := saveJobs(jobs); err != nil {
		return SyncJob{}, err
	}
	return job, syncCrontab(jobs)
}

func (a *App) DeleteJob(id string) error {
	jobs, err := loadJobs()
	if err != nil {
		return err
	}
	filtered := make([]SyncJob, 0, len(jobs))
	for _, job := range jobs {
		if job.ID != id {
			filtered = append(filtered, job)
		}
	}
	if err := saveJobs(filtered); err != nil {
		return err
	}
	return syncCrontab(filtered)
}

func (a *App) RunJobNow(id string) error {
	jobs, err := loadJobs()
	if err != nil {
		return err
	}
	for i, job := range jobs {
		if job.ID == id {
			if _, err := normalizeJob(job); err != nil {
				return err
			}
			a.emitSyncProgress(job.ID, 0, "准备同步", "running")
			if err := runJobToLogWithProgress(job, logPath(job.ID), a.emitSyncProgress); err != nil {
				a.emitSyncProgress(job.ID, 0, err.Error(), "error")
				return err
			}
			a.emitSyncProgress(job.ID, 100, "同步完成", "done")
			jobs[i].LastRunAt = time.Now().Format(time.RFC3339)
			_ = saveJobs(jobs)
			return nil
		}
	}
	return fmt.Errorf("未找到任务: %s", id)
}

func (a *App) GetLogs(jobID string) ([]LogEntry, error) {
	jobs, err := loadJobs()
	if err != nil {
		return nil, err
	}
	nameByID := map[string]string{}
	for _, job := range jobs {
		nameByID[job.ID] = job.Name
	}
	entries := []LogEntry{}
	for _, job := range jobs {
		if jobID != "" && job.ID != jobID {
			continue
		}
		entry := readLog(job.ID, nameByID[job.ID])
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Modified > entries[j].Modified })
	return entries, nil
}

func normalizeJob(job SyncJob) (SyncJob, error) {
	job.Name = strings.TrimSpace(job.Name)
	job.Source = strings.TrimSpace(job.Source)
	job.Destination = strings.TrimSpace(job.Destination)
	job.SourceMachine = normalizeMachineID(job.SourceMachine)
	job.SourcePath = strings.TrimSpace(job.SourcePath)
	job.DestinationMachine = normalizeMachineID(job.DestinationMachine)
	job.DestinationPath = strings.TrimSpace(job.DestinationPath)
	job.Schedule = strings.TrimSpace(job.Schedule)
	job.Options = strings.TrimSpace(job.Options)
	if job.SourcePath != "" {
		job.Source = endpointFor(job.SourceMachine, job.SourcePath)
	}
	if job.DestinationPath != "" {
		job.Destination = endpointFor(job.DestinationMachine, job.DestinationPath)
	}
	if job.Name == "" {
		return job, errors.New("请填写任务名称")
	}
	if job.Source == "" {
		return job, errors.New("请选择来源机器和路径")
	}
	if job.Destination == "" {
		return job, errors.New("请选择目标机器和路径")
	}
	if job.Schedule == "" {
		return job, errors.New("请填写同步周期")
	}
	if !validSchedule(job.Schedule) {
		return job, errors.New("定时表达式无效：支持 @hourly/@daily/@weekly/@monthly 或 5 段 crontab")
	}
	return job, nil
}

func normalizeMachineID(machineID string) string {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" || machineID == "current" || machineID == "localhost" {
		return localMachineID
	}
	return machineID
}

func endpointFor(machineID, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if normalizeMachineID(machineID) == localMachineID {
		return path
	}
	return normalizeMachineID(machineID) + ":" + path
}

func validSchedule(schedule string) bool {
	presets := map[string]bool{"@reboot": true, "@hourly": true, "@daily": true, "@weekly": true, "@monthly": true, "@yearly": true, "@annually": true}
	if presets[schedule] {
		return true
	}
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return false
	}
	fieldRe := regexp.MustCompile(`^[\w*/?,#.-]+$`)
	for _, f := range fields {
		if !fieldRe.MatchString(f) {
			return false
		}
	}
	return true
}

func pathDialogTitle(kind string) string {
	switch kind {
	case "source":
		return "选择来源文件夹"
	case "destination":
		return "选择目标文件夹"
	default:
		return "选择文件夹"
	}
}

func existingDirectoryOrHome(path string) string {
	path = strings.TrimSpace(path)
	if path != "" {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return "/"
}

func listMachines() ([]Machine, error) {
	host, _ := os.Hostname()
	machines := []Machine{{ID: localMachineID, Name: "当前机器", Kind: "local", Address: host}}
	configPath := sshConfigPath()
	if configPath == "" {
		return machines, nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return machines, nil
		}
		return machines, err
	}
	for _, host := range parseSSHConfigHosts(string(data)) {
		machines = append(machines, Machine{ID: host, Name: host, Kind: "ssh", Address: host})
	}
	return machines, nil
}

func sshConfigPath() string {
	return sshConfigPathFromEnv(os.Getenv, func() (string, error) { return os.UserHomeDir() })
}

func sshConfigPathFromEnv(getenv func(string) string, userHomeDir func() (string, error)) string {
	home, err := userHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = strings.TrimSpace(getenv("HOME"))
	}
	if strings.TrimSpace(home) == "" {
		home = strings.TrimSpace(getenv("USERPROFILE"))
	}
	if strings.TrimSpace(home) == "" {
		drive := strings.TrimSpace(getenv("HOMEDRIVE"))
		path := strings.TrimSpace(getenv("HOMEPATH"))
		if drive != "" && path != "" {
			home = drive + path
		}
	}
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "config")
}

func parseSSHConfigHosts(config string) []string {
	seen := map[string]bool{}
	hosts := []string{}
	scanner := bufio.NewScanner(strings.NewReader(config))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.ToLower(fields[0]) != "host" {
			continue
		}
		for _, host := range fields[1:] {
			if strings.ContainsAny(host, "*?") || seen[host] {
				continue
			}
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	sort.Strings(hosts)
	return hosts
}

func listDirectories(machineID string, current string) ([]DirectoryEntry, error) {
	machineID = normalizeMachineID(machineID)
	current = strings.TrimSpace(current)
	if current == "" {
		current = "/"
	}
	if machineID == localMachineID {
		if runtime.GOOS == "windows" && current == "/" {
			current = existingDirectoryOrHome("")
		}
		return listLocalDirectories(current)
	}
	return listRemoteDirectories(machineID, current)
}

func listLocalDirectories(current string) ([]DirectoryEntry, error) {
	entries, err := os.ReadDir(current)
	if err != nil {
		return nil, err
	}
	dirs := []DirectoryEntry{}
	if parent := filepath.Dir(current); parent != current {
		dirs = append(dirs, DirectoryEntry{Name: "..", Path: parent})
	}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, DirectoryEntry{Name: entry.Name(), Path: filepath.Join(current, entry.Name())})
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	return dirs, nil
}

func listRemoteDirectories(machineID string, current string) ([]DirectoryEntry, error) {
	remoteCommand := "find " + shellQuote(current) + " -mindepth 1 -maxdepth 1 -type d -print"
	cmd := newHiddenCommand("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", machineID, remoteCommand)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	dirs := []DirectoryEntry{}
	if parent := pathParent(current); parent != current {
		dirs = append(dirs, DirectoryEntry{Name: "..", Path: parent})
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		dirs = append(dirs, DirectoryEntry{Name: pathBase(line), Path: line})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	return dirs, nil
}

func pathParent(path string) string {
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "/"
	}
	return path[:idx]
}

func pathBase(path string) string {
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}
	return path[idx+1:]
}

func syncCrontab(jobs []SyncJob) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if _, err := exec.LookPath("crontab"); err != nil {
		return err
	}
	current := ""
	if out, err := newHiddenCommand("crontab", "-l").Output(); err == nil {
		current = string(out)
	}
	kept := []string{}
	scanner := bufio.NewScanner(strings.NewReader(current))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, rtCronMarker) {
			kept = append(kept, line)
		}
	}
	for _, job := range jobs {
		if job.Enabled {
			kept = append(kept, buildCronLine(job, logPath(job.ID)))
		}
	}
	content := strings.TrimSpace(strings.Join(kept, "\n"))
	if content != "" {
		content += "\n"
	}
	cmd := newHiddenCommand("crontab", "-")
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}

func buildCronLine(job SyncJob, log string) string {
	return fmt.Sprintf("%s /bin/sh -lc %s >> %s 2>&1 %s%s", job.Schedule, shellQuote(buildRunScript(job, log)), shellQuote(log), rtCronMarker, job.ID)
}

func buildRunScript(job SyncJob, log string) string {
	dir := filepath.Dir(log)
	backupPrefix := backupDirectoryPrefix(job)
	destinationBase := strings.TrimRight(job.Destination, "/")
	destination := shellQuote(destinationBase) + "/$backup_dir"
	return fmt.Sprintf("mkdir -p %s; backup_dir=%s/$(date '+%%Y%%m%%d_%%H%%M%%S'); %s; echo '--- rt start '$(date '+%%F %%T')' %s -> '$backup_dir' ---'; rsync %s %s %s; code=$?; echo '--- rt end '$(date '+%%F %%T')' exit='$code' ---'; exit $code", shellQuote(dir), shellQuote(backupPrefix), buildPrepareDestinationParentScript(destinationBase, backupPrefix), shellSafe(job.Name), ensureRsyncProgressOptions(job.Options), shellQuote(job.Source), destination)
}

func ensureRsyncProgressOptions(options string) string {
	options = strings.TrimSpace(options)
	if options == "" {
		options = "-avh --delete"
	}
	if !strings.Contains(options, "--info=progress2") && !strings.Contains(options, "progress2") {
		options += " --info=progress2"
	}
	if !strings.Contains(options, "--outbuf=") {
		options += " --outbuf=L"
	}
	return strings.TrimSpace(options)
}

func buildPrepareDestinationParentScript(destinationBase string, backupPrefix string) string {
	if host, path, ok := splitRemoteEndpoint(destinationBase); ok {
		remoteParent := strings.TrimRight(path, "/") + "/" + backupPrefix
		return "ssh " + shellQuote(host) + " " + shellQuote("mkdir -p "+shellQuote(remoteParent))
	}
	return "mkdir -p " + shellQuote(strings.TrimRight(destinationBase, "/")+"/"+backupPrefix)
}

func findRsyncExecutable() (string, error) {
	return findExecutableWithBundled("rsync", []string{
		filepath.Join("rsync", "bin", executableName("rsync")),
		filepath.Join("rsync", executableName("rsync")),
	})
}

func findSSHExecutable() (string, error) {
	return findExecutableWithBundled("ssh", []string{
		filepath.Join("rsync", "bin", executableName("ssh")),
		filepath.Join("rsync", executableName("ssh")),
	})
}

func findExecutableWithBundled(name string, bundledRelative []string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	base := filepath.Dir(exe)
	for _, rel := range bundledRelative {
		candidate := filepath.Join(base, rel)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found", name)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func rsyncEndpointForRuntime(endpoint string) string {
	if runtime.GOOS != "windows" {
		return endpoint
	}
	return rsyncEndpointForOS(endpoint, "windows")
}

func rsyncEndpointForOS(endpoint string, goos string) string {
	endpoint = strings.TrimSpace(endpoint)
	if goos != "windows" || endpoint == "" {
		return endpoint
	}
	if _, _, ok := splitRemoteEndpoint(endpoint); ok {
		return endpoint
	}
	return windowsLocalPathForRsync(endpoint)
}

func windowsLocalPathForRsync(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if len(path) >= 2 && path[1] == ':' && isASCIIAlpha(path[0]) {
		drive := strings.ToLower(path[:1])
		rest := strings.TrimLeft(path[2:], "/")
		if rest == "" {
			return "/" + drive
		}
		return "/" + drive + "/" + rest
	}
	return path
}

func isASCIIAlpha(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func pathWithToolDir(env []string, toolDir string) []string {
	if toolDir == "" {
		return env
	}
	separator := string(os.PathListSeparator)
	updated := make([]string, 0, len(env)+1)
	found := false
	for _, item := range env {
		if strings.HasPrefix(strings.ToUpper(item), "PATH=") {
			updated = append(updated, item[:5]+toolDir+separator+item[5:])
			found = true
			continue
		}
		updated = append(updated, item)
	}
	if !found {
		updated = append(updated, "PATH="+toolDir)
	}
	return updated
}

func splitRemoteEndpoint(endpoint string) (host string, path string, ok bool) {
	idx := strings.Index(endpoint, ":")
	if idx <= 0 || strings.Contains(endpoint[:idx], "/") {
		return "", "", false
	}
	if idx == 1 && isASCIIAlpha(endpoint[0]) {
		if len(endpoint) == 2 || endpoint[2] == '/' || endpoint[2] == '\\' {
			return "", "", false
		}
	}
	return endpoint[:idx], endpoint[idx+1:], true
}

func backupDirectoryName(job SyncJob, timestamp string) string {
	return backupDirectoryPrefix(job) + "/" + sanitizePathName(timestamp)
}

func backupDirectoryPrefix(job SyncJob) string {
	return sanitizePathName(sourceHostName(job)) + "/" + sanitizePathName(job.Name)
}

func sourceHostName(job SyncJob) string {
	machine := normalizeMachineID(job.SourceMachine)
	if machine != localMachineID {
		return machine
	}
	if idx := strings.Index(job.Source, ":"); idx > 0 && !strings.Contains(job.Source[:idx], "/") {
		return job.Source[:idx]
	}
	return localMachineID
}

func sanitizePathName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "untitled"
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range name {
		allowed := r == '-' || r == '.' || r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r > 127
		if allowed {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	cleaned := strings.Trim(builder.String(), "_ .")
	if cleaned == "" {
		return "untitled"
	}
	return cleaned
}

func runShellScriptToLog(script string, log string) error {
	return runShellScriptToLogWithProgress(script, log, "", nil)
}

func runJobToLogWithProgress(job SyncJob, log string, emit progressEmitter) error {
	if runtime.GOOS == "windows" {
		return runRsyncDirectToLogWithProgress(job, log, job.ID, emit)
	}
	return runShellScriptToLogWithProgress(buildRunScript(job, log), log, job.ID, emit)
}

type progressEmitter func(jobID string, percent int, text string, state string)

func runShellScriptToLogWithProgress(script string, log string, jobID string, emit progressEmitter) error {
	if err := os.MkdirAll(filepath.Dir(log), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	cmd := newHiddenCommand("/bin/sh", "-lc", script)
	parser := newRsyncProgressParser(jobID, emit)
	writer := &progressLogWriter{file: file, parser: parser}
	cmd.Stdout = writer
	cmd.Stderr = writer
	return cmd.Run()
}

func runRsyncDirectToLogWithProgress(job SyncJob, log string, jobID string, emit progressEmitter) error {
	rsync, err := findRsyncExecutable()
	if err != nil {
		return fmt.Errorf("未找到 rsync：请下载新版完整包，或把 rsync.exe 加入 PATH: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(log), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	stamp := time.Now().Format("20060102_150405")
	backupDir := backupDirectoryName(job, stamp)
	destinationBase := strings.TrimRight(job.Destination, "/\\")
	destination := ""
	if host, remotePath, ok := splitRemoteEndpoint(destinationBase); ok {
		remoteParent := strings.TrimRight(remotePath, "/") + "/" + backupDirectoryPrefix(job)
		ssh, sshErr := findSSHExecutable()
		if sshErr != nil {
			return fmt.Errorf("未找到 ssh：远程目标创建目录不可用: %w", sshErr)
		}
		mkdir := newHiddenCommand(ssh, host, "mkdir -p "+shellQuote(remoteParent))
		mkdir.Stdout = file
		mkdir.Stderr = file
		if err := mkdir.Run(); err != nil {
			return err
		}
		destination = host + ":" + strings.TrimRight(remotePath, "/") + "/" + backupDir
	} else {
		parent := filepath.Join(destinationBase, filepath.FromSlash(backupDirectoryPrefix(job)))
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		destination = filepath.Join(destinationBase, filepath.FromSlash(backupDir))
	}

	args := strings.Fields(ensureRsyncProgressOptions(job.Options))
	args = append(args, rsyncEndpointForRuntime(job.Source), rsyncEndpointForRuntime(destination))
	cmd := newHiddenCommand(rsync, args...)
	cmd.Env = pathWithToolDir(os.Environ(), filepath.Dir(rsync))
	parser := newRsyncProgressParser(jobID, emit)
	writer := &progressLogWriter{file: file, parser: parser}
	cmd.Stdout = writer
	cmd.Stderr = writer
	return cmd.Run()
}

type progressLogWriter struct {
	mu     sync.Mutex
	file   *os.File
	parser *rsyncProgressParser
}

func (w *progressLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.parser != nil {
		w.parser.Write(p)
	}
	return w.file.Write(p)
}

type rsyncProgressParser struct {
	jobID   string
	emit    progressEmitter
	buffer  string
	lastPct int
}

func newRsyncProgressParser(jobID string, emit progressEmitter) *rsyncProgressParser {
	return &rsyncProgressParser{jobID: jobID, emit: emit, lastPct: -1}
}

func (p *rsyncProgressParser) Write(data []byte) {
	if p.emit == nil || p.jobID == "" {
		return
	}
	p.buffer += string(data)
	for {
		idx := strings.IndexAny(p.buffer, "\r\n")
		if idx < 0 {
			p.parseLine(p.buffer)
			if len(p.buffer) > 512 {
				p.buffer = p.buffer[len(p.buffer)-512:]
			}
			return
		}
		line := p.buffer[:idx]
		p.parseLine(line)
		p.buffer = strings.TrimLeft(p.buffer[idx+1:], "\r\n")
	}
}

var rsyncProgressRe = regexp.MustCompile(`(?m)([0-9]{1,3})%`)

func (p *rsyncProgressParser) parseLine(line string) {
	percent, ok := parseRsyncProgressPercent(line)
	if !ok || percent == p.lastPct {
		return
	}
	p.lastPct = percent
	p.emit(p.jobID, percent, strings.TrimSpace(line), "running")
}

func parseRsyncProgressPercent(line string) (int, bool) {
	match := rsyncProgressRe.FindStringSubmatch(line)
	if len(match) < 2 {
		return 0, false
	}
	percent, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return percent, true
}

func (a *App) emitSyncProgress(jobID string, percent int, text string, state string) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "sync-progress", SyncProgress{JobID: jobID, Percent: percent, Text: text, State: state})
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func shellSafe(s string) string { return strings.ReplaceAll(s, "'", "") }

func appDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "rt")
}
func jobsPath() string         { return filepath.Join(appDir(), "jobs.json") }
func logsDir() string          { return filepath.Join(appDir(), "logs") }
func logPath(id string) string { return filepath.Join(logsDir(), id+".log") }

func loadJobs() ([]SyncJob, error) {
	path := jobsPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []SyncJob{}, nil
	}
	if err != nil {
		return nil, err
	}
	var jobs []SyncJob
	if len(data) == 0 {
		return []SyncJob{}, nil
	}
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func saveJobs(jobs []SyncJob) error {
	if err := os.MkdirAll(appDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(jobsPath(), data, 0644)
}

func readLog(id, name string) LogEntry {
	path := logPath(id)
	entry := LogEntry{JobID: id, JobName: name, LogPath: path}
	info, err := os.Stat(path)
	if err != nil {
		entry.Content = "暂无日志"
		return entry
	}
	entry.Size = info.Size()
	entry.Modified = info.ModTime().Format(time.RFC3339)
	data, err := os.ReadFile(path)
	if err != nil {
		entry.Content = err.Error()
		return entry
	}
	content := string(data)
	if len(content) > 20000 {
		content = content[len(content)-20000:]
	}
	entry.Content = content
	return entry
}

func newID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
