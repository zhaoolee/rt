import { useEffect, useMemo, useState } from 'react';
import './App.css';
import { DeleteJob, GetLogs, GetStatus, ListDirectories, ListJobs, ListMachines, RunJobNow, SaveJob } from '../wailsjs/go/main/App';
import { main } from '../wailsjs/go/models';

type Job = main.SyncJob;
type LogEntry = main.LogEntry;
type Status = main.Status;
type Machine = main.Machine;
type DirectoryEntry = main.DirectoryEntry;
type EndpointKind = 'source' | 'destination';

type PickerState = {
  kind: EndpointKind;
  machineId: string;
  currentPath: string;
  entries: DirectoryEntry[];
  loading: boolean;
};

const LOCAL_MACHINE_ID = 'local';

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

function App() {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [machines, setMachines] = useState<Machine[]>(previewMachines);
  const [status, setStatus] = useState<Status | null>(null);
  const [form, setForm] = useState<Job>(emptyJob);
  const [selectedJobId, setSelectedJobId] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [picker, setPicker] = useState<PickerState | null>(null);

  const selectedLog = useMemo(() => logs.find((log) => log.jobId === selectedJobId) || logs[0], [logs, selectedJobId]);
  const wailsReady = Boolean((window as any).go?.main?.App);

  async function refresh() {
    if (!wailsReady) {
      setStatus({ crontabAvailable: true, rsyncAvailable: true, storeDir: '~/.config/rt', message: '设计预览模式' });
      setMachines(previewMachines);
      setJobs([]);
      setLogs([]);
      return;
    }
    const [nextStatus, nextMachines, nextJobs, nextLogs] = await Promise.all([GetStatus(), ListMachines(), ListJobs(), GetLogs('')]);
    setStatus(nextStatus);
    setMachines(nextMachines?.length ? nextMachines : previewMachines);
    setJobs(nextJobs || []);
    setLogs(nextLogs || []);
    if (!selectedJobId && nextJobs?.[0]?.id) setSelectedJobId(nextJobs[0].id);
  }

  useEffect(() => {
    refresh().catch((err) => setNotice(String(err)));
  }, []);

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
    setBusy(true);
    setNotice('');
    try {
      const saved = await SaveJob(form);
      setNotice(`已保存：${saved.name}，crontab 已同步`);
      setForm(emptyJob);
      setSelectedJobId(saved.id);
      await refresh();
    } catch (err) {
      setNotice(String(err));
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: string) {
    if (!confirm('删除这个同步任务？对应 crontab 记录也会移除。')) return;
    setBusy(true);
    try {
      await DeleteJob(id);
      setNotice('已删除任务');
      await refresh();
    } catch (err) {
      setNotice(String(err));
    } finally {
      setBusy(false);
    }
  }

  async function runNow(id: string) {
    setBusy(true);
    setNotice('正在执行 rsync，同步完成后会刷新日志…');
    try {
      await RunJobNow(id);
      setNotice('同步完成，日志已刷新');
      await refresh();
    } catch (err) {
      setNotice(String(err));
      await refresh();
    } finally {
      setBusy(false);
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
      setNotice(`无法读取目录：${String(err)}`);
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
      <section className="hero">
        <div>
          <p className="eyebrow">RT · rsync tasker</p>
          <h1>rsync定时同步</h1>
          <p className="subtle">像 Hyper Backup 一样先选机器，再选路径；机器来自 ~/.ssh/config，也可以选择当前机器。</p>
        </div>
        <div className="status-card">
          <span className="dot" />
          <strong>{status?.message || '检查环境中…'}</strong>
          <small>{status?.storeDir}</small>
        </div>
      </section>

      <section className="grid">
        <div className="panel composer">
          <div className="panel-head">
            <div>
              <p className="eyebrow">New schedule</p>
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
          <label className="switch"><input type="checkbox" checked={form.enabled} onChange={(e) => setField('enabled', e.target.checked)} /> 启用 crontab 定时</label>
          <button className="primary" disabled={busy} onClick={save}>{busy ? '处理中…' : '保存并同步 crontab'}</button>
          {notice && <p className="notice">{notice}</p>}
        </div>

        <div className="panel jobs">
          <div className="panel-head">
            <div>
              <p className="eyebrow">Jobs</p>
              <h2>同步任务</h2>
            </div>
            <button className="ghost" onClick={() => refresh()}>刷新</button>
          </div>
          {jobs.length === 0 && <div className="empty">暂无任务，先在左侧创建一个同步计划。</div>}
          {jobs.map((job) => (
            <article className={`job ${selectedJobId === job.id ? 'active' : ''}`} key={job.id} onClick={() => setSelectedJobId(job.id)}>
              <div>
                <strong>{job.name}</strong>
                <p>{job.source} → {job.destination}</p>
                <small>{job.enabled ? '已启用' : '已暂停'} · {job.schedule} · {job.options}</small>
              </div>
              <div className="actions">
                <button onClick={(e) => { e.stopPropagation(); runNow(job.id); }}>立即同步</button>
                <button onClick={(e) => { e.stopPropagation(); edit(job); }}>编辑</button>
                <button onClick={(e) => { e.stopPropagation(); remove(job.id); }}>删除</button>
              </div>
            </article>
          ))}
        </div>
      </section>

      <section className="panel log-panel">
        <div className="panel-head">
          <div>
            <p className="eyebrow">Logs</p>
            <h2>同步日志</h2>
          </div>
          <small>{selectedLog?.logPath || '暂无日志路径'}</small>
        </div>
        <pre>{selectedLog?.content || '暂无日志。手动执行或等待 crontab 触发后，这里会显示 rsync 输出。'}</pre>
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
