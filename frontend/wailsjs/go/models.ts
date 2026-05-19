export namespace main {
	
	export class DirectoryEntry {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class LogEntry {
	    jobId: string;
	    jobName: string;
	    logPath: string;
	    content: string;
	    modified: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new LogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jobId = source["jobId"];
	        this.jobName = source["jobName"];
	        this.logPath = source["logPath"];
	        this.content = source["content"];
	        this.modified = source["modified"];
	        this.size = source["size"];
	    }
	}
	export class Machine {
	    id: string;
	    name: string;
	    kind: string;
	    address: string;
	
	    static createFrom(source: any = {}) {
	        return new Machine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.address = source["address"];
	    }
	}
	export class Status {
	    crontabAvailable: boolean;
	    rsyncAvailable: boolean;
	    storeDir: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.crontabAvailable = source["crontabAvailable"];
	        this.rsyncAvailable = source["rsyncAvailable"];
	        this.storeDir = source["storeDir"];
	        this.message = source["message"];
	    }
	}
	export class SyncJob {
	    id: string;
	    name: string;
	    source: string;
	    destination: string;
	    sourceMachine: string;
	    sourcePath: string;
	    destinationMachine: string;
	    destinationPath: string;
	    schedule: string;
	    options: string;
	    enabled: boolean;
	    createdAt: string;
	    updatedAt: string;
	    lastRunAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncJob(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.source = source["source"];
	        this.destination = source["destination"];
	        this.sourceMachine = source["sourceMachine"];
	        this.sourcePath = source["sourcePath"];
	        this.destinationMachine = source["destinationMachine"];
	        this.destinationPath = source["destinationPath"];
	        this.schedule = source["schedule"];
	        this.options = source["options"];
	        this.enabled = source["enabled"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.lastRunAt = source["lastRunAt"];
	    }
	}

}

