import { useEffect, useMemo, useRef, useState } from 'react';
import './App.css';
import { DeleteJob, GetLogs, GetStatus, ListDirectories, ListJobs, ListMachines, RunJobNow, SaveJob } from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';
import { EventsOn, WindowSetTitle } from '../wailsjs/runtime/runtime';

type Job = main.SyncJob;
type LogEntry = main.LogEntry;
type Machine = main.Machine;
type DirectoryEntry = main.DirectoryEntry;
type EndpointKind = 'source' | 'destination';

type CronField = {
  values: Set<number>;
  restricted: boolean;
};

type SyncProgress = {
  jobId: string;
  percent: number;
  text: string;
  state: 'running' | 'done' | 'error';
};

type PickerState = {
  kind: EndpointKind;
  machineId: string;
  currentPath: string;
  entries: DirectoryEntry[];
  loading: boolean;
};

const LOCAL_MACHINE_ID = 'local';
const APP_VERSION = import.meta.env.VITE_APP_VERSION || 'dev';

const emptyJob: Job = {
  id: '',
  name: '',
  source: '',
  destination: '',
  sourceMachine: LOCAL_MACHINE_ID,
  sourcePath: '',
  destinationMachine: LOCAL_MACHINE_ID,
  destinationPath: '',
  schedule: '@hourly',
  options: '-avh --delete',
  enabled: true,
  createdAt: '',
  updatedAt: '',
  lastRunAt: '',
};

const previewMachines: Machine[] = [
  { id: LOCAL_MACHINE_ID, name: '当前机器', kind: 'local', address: 'localhost' } as Machine,
  { id: 'nas', name: 'nas', kind: 'ssh', address: '~/.ssh/config' } as Machine,
  { id: 'pi', name: 'pi', kind: 'ssh', address: '~/.ssh/config' } as Machine,
];

function startOfNextMinute(now: Date) {
  const next = new Date(now);
  next.setSeconds(0, 0);
  next.setMinutes(next.getMinutes() + 1);
  return next;
}

function parseCronField(raw: string, min: number, max: number, normalize?: (value: number) => number): CronField | null {
  const values = new Set<number>();
  const parts = raw.split(',').map((part) => part.trim()).filter(Boolean);
  if (parts.length === 0) return null;

  for (const part of parts) {
    const [rangePart, stepPart] = part.split('/');
    const step = stepPart ? Number(stepPart) : 1;
    if (!Number.isInteger(step) || step <= 0) return null;

    let start = min;
    let end = max;
    if (rangePart !== '*') {
      if (rangePart.includes('-')) {
        const [from, to] = rangePart.split('-').map(Number);
        if (!Number.isInteger(from) || !Number.isInteger(to)) return null;
        start = from;
        end = to;
      } else {
        const value = Number(rangePart);
        if (!Number.isInteger(value)) return null;
        start = value;
        end = value;
      }
    }

    for (let value = start; value <= end; value += step) {
      const normalized = normalize ? normalize(value) : value;
      if (normalized >= min && normalized <= max) values.add(normalized);
    }
  }

  return values.size ? { values, restricted: raw.trim() !== '*' } : null;
}

function nextPresetRun(schedule: string, now: Date) {
  const next = new Date(now);
  next.setMilliseconds(0);
  switch (schedule) {
    case '@hourly':
      next.setMinutes(0, 0, 0);
      next.setHours(next.getHours() + 1);
      return next;
    case '@daily':
      next.setHours(0, 0, 0, 0);
      next.setDate(next.getDate() + 1);
      return next;
    case '@weekly':
      next.setHours(0, 0, 0, 0);
      next.setDate(next.getDate() + ((7 - next.getDay()) || 7));
      return next;
    case '@monthly':
      next.setHours(0, 0, 0, 0);
      next.setMonth(next.getMonth() + 1, 1);
      return next;
    case '@yearly':
    case '@annually':
      next.setHours(0, 0, 0, 0);
      next.setFullYear(next.getFullYear() + 1, 0, 1);
      return next;
    default:
      return null;
  }
}

function nextCronRun(schedule: string, now: Date) {
  const trimmed = schedule.trim();
  const preset = nextPresetRun(trimmed, now);
  if (preset) return preset;

  const parts = trimmed.split(/\s+/);
  if (parts.length !== 5) return null;

  const [minuteRaw, hourRaw, dayRaw, monthRaw, weekdayRaw] = parts;
  const minutes = parseCronField(minuteRaw, 0, 59);
  const hours = parseCronField(hourRaw, 0, 23);
  const days = parseCronField(dayRaw, 1, 31);
  const months = parseCronField(monthRaw, 1, 12);
  const weekdays = parseCronField(weekdayRaw, 0, 6, (value) => value === 7 ? 0 : value);
  if (!minutes || !hours || !days || !months || !weekdays) return null;

  const candidate = startOfNextMinute(now);
  const maxMinutes = 60 * 24 * 366;
  for (let i = 0; i < maxMinutes; i += 1) {
    const dayMatch = days.values.has(candidate.getDate());
    const weekdayMatch = weekdays.values.has(candidate.getDay());
    const dateMatch = days.restricted && weekdays.restricted ? dayMatch || weekdayMatch : dayMatch && weekdayMatch;
    if (
      minutes.values.has(candidate.getMinutes()) &&
      hours.values.has(candidate.getHours()) &&
      months.values.has(candidate.getMonth() + 1) &&
      dateMatch
    ) {
      return new Date(candidate);
    }
    candidate.setMinutes(candidate.getMinutes() + 1);
  }
  return null;
}

function formatCountdown(ms: number) {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (days > 0) return `${days}天 ${hours}小时 ${minutes}分`;
  if (hours > 0) return `${hours}小时 ${minutes}分 ${seconds}秒`;
  if (minutes > 0) return `${minutes}分 ${seconds}秒`;
  return `${seconds}秒`;
}

function formatNextRun(next: Date) {
  return next.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

function nextRunLabel(job: Job, now: Date) {
  if (!job.enabled) return '已暂停';
  if (job.schedule.trim() === '@reboot') return '下次执行：系统启动后';
  const next = nextCronRun(job.schedule, now);
  if (!next) return '下次执行：无法计算';
  return `下次执行 ${formatCountdown(next.getTime() - now.getTime())} · ${formatNextRun(next)}`;
}

function App() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [machines, setMachines] = useState<Machine[]>(previewMachines);
  const [form, setForm] = useState<Job>(emptyJob);
  const [selectedJobId, setSelectedJobId] = useState('');
  const [formNotice, setFormNotice] = useState('');
  const [jobError, setJobError] = useState('');
  const [formBusy, setFormBusy] = useState(false);
  const [deletingJobId, setDeletingJobId] = useState('');
  const [runningJobId, setRunningJobId] = useState('');
  const [progressByJob, setProgressByJob] = useState<Record<string, SyncProgress>>({});
  const [now, setNow] = useState(() => new Date());
  const [picker, setPicker] = useState<PickerState | null>(null);
  const [statusMessage, setStatusMessage] = useState('');
  const logRef = useRef<HTMLPreElement | null>(null);

  const selectedLog = useMemo(() => logs.find((log) => log.jobId === selectedJobId) || logs[0], [logs, selectedJobId]);
  const wailsReady = Boolean((window as any).go?.main?.App);

  async function refresh() {
    if (!wailsReady) {
      setMachines(previewMachines);
      setJobs([]);
      setLogs([]);
      return;
    }
    const [nextMachines, nextJobs, nextLogs, nextStatus] = await Promise.all([ListMachines(), ListJobs(), GetLogs(''), GetStatus()]);
    setMachines(nextMachines?.length ? nextMachines : previewMachines);
    setJobs(nextJobs || []);
    setLogs(nextLogs || []);
    setStatusMessage(nextStatus?.message || '');
    if (!selectedJobId && nextJobs?.[0]?.id) setSelectedJobId(nextJobs[0].id);
  }

  useEffect(() => {
    refresh().catch((err) => setJobError(String(err)));
  }, []);

  useEffect(() => {
    const title = `RSYNC定时同步 ${APP_VERSION}`;
    document.title = title;
    if (wailsReady) WindowSetTitle(title);
  }, [wailsReady]);

  useEffect(() => {
    if (!wailsReady) return;
    const cancel = EventsOn('sync-progress', (progress: SyncProgress) => {
      if (!progress?.jobId) return;
      setProgressByJob((prev) => ({ ...prev, [progress.jobId]: progress }));
      if (progress.state === 'error') {
        setJobError(progress.text || '同步失败');
      }
    });
    return cancel;
  }, [wailsReady]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!wailsReady || !runningJobId) return;
    const timer = window.setInterval(() => {
      GetLogs('').then((nextLogs) => setLogs(nextLogs || [])).catch((err) => setJobError(String(err)));
    }, 800);
    return () => window.clearInterval(timer);
  }, [wailsReady, runningJobId]);

  useEffect(() => {
    const logElement = logRef.current;
    if (!logElement) return;
    logElement.scrollTop = logElement.scrollHeight;
  }, [selectedLog?.content, selectedLog?.jobId]);

  function setField(key: keyof Job, value: string | boolean) {
    setForm((prev) => ({ ...prev, [key]: value }));
  }

  function machineName(machineId: string) {
    return machines.find((machine) => machine.id === machineId)?.name || machineId || '当前机器';
  }

  function endpointFor(kind: EndpointKind) {
    const machineId = kind === 'source' ? form.sourceMachine : form.destinationMachine;
    const path = kind === 'source' ? form.sourcePath : form.destinationPath;
    if (!path) return kind === 'source' ? '选择来源路径' : '选择目标路径';
    return `${machineName(machineId)} · ${path}`;
  }

  function changeMachine(kind: EndpointKind, machineId: string) {
    if (kind === 'source') {
      setForm((prev) => ({ ...prev, sourceMachine: machineId, sourcePath: '', source: '' }));
    } else {
      setForm((prev) => ({ ...prev, destinationMachine: machineId, destinationPath: '', destination: '' }));
    }
  }

  async function save() {
    setFormBusy(true);
    setFormNotice('');
    try {
      const saved = await SaveJob(form);
      setFormNotice(`已保存：${saved.name}${statusMessage ? ` · ${statusMessage}` : ''}`);
      setForm(emptyJob);
      setSelectedJobId(saved.id);
      await refresh();
    } catch (err) {
      setFormNotice(String(err));
    } finally {
      setFormBusy(false);
    }
  }

  async function remove(id: string) {
    if (!confirm('删除这个同步任务？对应定时记录也会移除。')) return;
    setDeletingJobId(id);
    setJobError('');
    try {
      await DeleteJob(id);
      await refresh();
    } catch (err) {
      setJobError(String(err));
    } finally {
      setDeletingJobId('');
    }
  }

  async function runNow(id: string) {
    setRunningJobId(id);
    setSelectedJobId(id);
    setProgressByJob((prev) => ({ ...prev, [id]: { jobId: id, percent: 0, text: '准备同步', state: 'running' } }));
    setJobError('');
    try {
      await RunJobNow(id);
      await refresh();
    } catch (err) {
      setJobError(String(err));
      await refresh();
    } finally {
      setRunningJobId('');
      setProgressByJob((prev) => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
    }
  }

  async function openPicker(kind: EndpointKind) {
    const machineId = kind === 'source' ? form.sourceMachine || LOCAL_MACHINE_ID : form.destinationMachine || LOCAL_MACHINE_ID;
    const currentPath = kind === 'source' ? form.sourcePath || '/' : form.destinationPath || '/';
    setPicker({ kind, machineId, currentPath, entries: [], loading: true });
    await loadPickerEntries(kind, machineId, currentPath);
  }

  async function loadPickerEntries(kind: EndpointKind, machineId: string, path: string) {
    if (!wailsReady) {
      const sample = [
        { name: '..', path: '/' },
        { name: 'home', path: '/home' },
        { name: 'volume1', path: '/volume1' },
        { name: 'backup', path: '/volume1/backup' },
      ] as DirectoryEntry[];
      setPicker({ kind, machineId, currentPath: path, entries: sample, loading: false });
      return;
    }
    try {
      const entries = await ListDirectories(machineId, path);
      setPicker({ kind, machineId, currentPath: path, entries: entries || [], loading: false });
    } catch (err) {
      setFormNotice(`无法读取目录：${String(err)}`);
      setPicker({ kind, machineId, currentPath: path, entries: [], loading: false });
    }
  }

  function chooseCurrentPath() {
    if (!picker) return;
    if (picker.kind === 'source') {
      setForm((prev) => ({ ...prev, sourceMachine: picker.machineId, sourcePath: picker.currentPath, source: '' }));
    } else {
      setForm((prev) => ({ ...prev, destinationMachine: picker.machineId, destinationPath: picker.currentPath, destination: '' }));
    }
    setPicker(null);
  }

  function edit(job: Job) {
    setForm({
      ...job,
      sourceMachine: job.sourceMachine || LOCAL_MACHINE_ID,
      destinationMachine: job.destinationMachine || LOCAL_MACHINE_ID,
      sourcePath: job.sourcePath || job.source,
      destinationPath: job.destinationPath || job.destination,
    });
    setSelectedJobId(job.id);
  }

  return (
    <main className="shell">
      <header className="app-title">
        <div>
          <p className="eyebrow">RSYNC TASKER</p>
          <h1>RSYNC定时同步</h1>
        </div>
        <span className="version-badge">{APP_VERSION}</span>
      </header>

      <section className="grid">
        <div className="panel composer">
          <div className="panel-head">
            <div>
              <h2>{form.id ? '编辑同步任务' : '新建同步任务'}</h2>
            </div>
            {form.id && <button className="ghost" onClick={() => setForm(emptyJob)}>新建</button>}
          </div>

          <label>任务名称<input value={form.name} onChange={(e) => setField('name', e.target.value)} placeholder="例如：照片备份" /></label>
          <div className="path-selectors">
            {(['source', 'destination'] as EndpointKind[]).map((kind) => (
              <div className="path-field" key={kind}>
                <span className="field-title">{kind === 'source' ? '来源' : '目标'}</span>
                <div className="machine-path-row">
                  <select
                    value={kind === 'source' ? form.sourceMachine : form.destinationMachine}
                    onChange={(e) => changeMachine(kind, e.target.value)}
                    aria-label={kind === 'source' ? '来源机器' : '目标机器'}
                  >
                    {machines.map((machine) => (
                      <option key={machine.id} value={machine.id}>{machine.name}{machine.kind === 'ssh' ? ' · SSH' : ''}</option>
                    ))}
                  </select>
                  <button className={`path-picker ${(kind === 'source' ? form.sourcePath : form.destinationPath) ? 'selected' : ''}`} onClick={() => openPicker(kind)}>
                    <span>{endpointFor(kind)}</span>
                    <strong>选路径</strong>
                  </button>
                </div>
              </div>
            ))}
          </div>
          <div className="two-cols">
            <label>定时周期<input value={form.schedule} onChange={(e) => setField('schedule', e.target.value)} placeholder="@hourly 或 */30 * * * *" /></label>
            <label>rsync 参数<input value={form.options} onChange={(e) => setField('options', e.target.value)} placeholder="-avh --delete" /></label>
          </div>
          <div className="quick">
            {['@hourly', '@daily', '@weekly', '*/30 * * * *', '0 2 * * *'].map((value) => <button key={value} onClick={() => setField('schedule', value)}>{value}</button>)}
          </div>
          <label className="switch"><input type="checkbox" checked={form.enabled} onChange={(e) => setField('enabled', e.target.checked)} /> 启用定时</label>
          <button className="primary" disabled={formBusy} onClick={save}>{formBusy ? '保存中…' : '保存任务'}</button>
          {formNotice && <p className="notice">{formNotice}</p>}
        </div>

        <div className="panel jobs">
          <div className="panel-head">
            <div>
              <h2>同步任务</h2>
            </div>
            <button className="ghost" onClick={() => refresh()}>刷新</button>
          </div>
          {jobError && <p className="notice job-notice">{jobError}</p>}
          {jobs.length === 0 && <div className="empty">暂无任务，先在左侧创建一个同步计划。</div>}
          {jobs.map((job) => {
            const isRunning = runningJobId === job.id;
            const isDeleting = deletingJobId === job.id;
            const isLocked = isRunning || isDeleting;
            const progress = progressByJob[job.id];
            const progressPercent = progress?.percent ?? 0;
            const scheduleLabel = nextRunLabel(job, now);
            return (
            <article
              aria-busy={isRunning}
              className={`job ${selectedJobId === job.id ? 'active' : ''} ${isLocked ? 'locked' : ''}`}
              key={job.id}
              onClick={() => { if (!isLocked) setSelectedJobId(job.id); }}
            >
              <div>
                <strong>{job.name}</strong>
                <p>{job.source} → {job.destination}</p>
                <small>{isRunning ? `同步中 ${progressPercent}%` : job.enabled ? '已启用' : '已暂停'} · {job.schedule} · {job.options}</small>
                {job.enabled && <div className="next-run">{scheduleLabel}</div>}
              </div>
              <div className="actions">
                <button disabled={isLocked} onClick={(e) => { e.stopPropagation(); runNow(job.id); }}>{isRunning ? '同步中…' : '立即同步'}</button>
                <button disabled={isLocked} onClick={(e) => { e.stopPropagation(); edit(job); }}>编辑</button>
                <button disabled={isLocked} onClick={(e) => { e.stopPropagation(); remove(job.id); }}>{isDeleting ? '删除中…' : '删除'}</button>
              </div>
              {isRunning && <div className="job-progress-overlay"><span>同步中 {progressPercent}%</span><i><b style={{ width: `${progressPercent}%` }} /></i></div>}
            </article>
          );})}
        </div>
      </section>

      <section className="panel log-panel">
        <div className="panel-head">
          <div>
            <h2>同步日志</h2>
          </div>
          <small>{selectedLog?.logPath ? `${selectedLog.logPath} · 自动滚动` : '暂无日志路径'}</small>
        </div>
        <pre ref={logRef}>{selectedLog?.content || '暂无日志。手动执行或等待定时触发后，这里会显示 rsync 输出。'}</pre>
      </section>

      {picker && (
        <div className="modal-backdrop" onClick={() => setPicker(null)}>
          <section className="path-modal" onClick={(event) => event.stopPropagation()}>
            <div className="panel-head">
              <div>
                <p className="eyebrow">{machineName(picker.machineId)}</p>
                <h2>{picker.kind === 'source' ? '选择来源路径' : '选择目标路径'}</h2>
              </div>
              <button className="ghost" onClick={() => setPicker(null)}>关闭</button>
            </div>
            <div className="current-path">{picker.currentPath}</div>
            <div className="directory-list">
              {picker.loading && <div className="empty">读取目录中…</div>}
              {!picker.loading && picker.entries.length === 0 && <div className="empty">没有可进入的子目录，或 SSH 无法免密连接。</div>}
              {picker.entries.map((entry) => (
                <button key={entry.path} onClick={() => loadPickerEntries(picker.kind, picker.machineId, entry.path)}>
                  <span>{entry.name}</span>
                  <small>{entry.path}</small>
                </button>
              ))}
            </div>
            <button className="primary" onClick={chooseCurrentPath}>使用当前路径</button>
          </section>
        </div>
      )}
    </main>
  );
}

export default App;
