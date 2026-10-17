export namespace aion2 {
	
	export class MemberStat {
	    id: number;
	    name: string;
	    class: string;
	    color: string;
	    damage: number;
	    dps: number;
	    hits: number;
	    crits: number;
	    self: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MemberStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.class = source["class"];
	        this.color = source["color"];
	        this.damage = source["damage"];
	        this.dps = source["dps"];
	        this.hits = source["hits"];
	        this.crits = source["crits"];
	        this.self = source["self"];
	    }
	}
	export class Snapshot {
	    active: boolean;
	    elapsed: number;
	    selfId: number;
	    self: string;
	    members: MemberStat[];
	    party: string[];
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.elapsed = source["elapsed"];
	        this.selfId = source["selfId"];
	        this.self = source["self"];
	        this.members = this.convertValues(source["members"], MemberStat);
	        this.party = source["party"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class CaptureState {
	    running: boolean;
	    error: string;
	    stats: sniffer.Stats;
	    logPath: string;
	
	    static createFrom(source: any = {}) {
	        return new CaptureState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.error = source["error"];
	        this.stats = this.convertValues(source["stats"], sniffer.Stats);
	        this.logPath = source["logPath"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sniffer {
	
	export class Stats {
	    devices: number;
	    packets: number;
	    bytes: number;
	    payloadBytes: number;
	    streams: number;
	    gaps: number;
	    packetsPerSec: number;
	    bytesPerSec: number;
	    trafficSeen: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.devices = source["devices"];
	        this.packets = source["packets"];
	        this.bytes = source["bytes"];
	        this.payloadBytes = source["payloadBytes"];
	        this.streams = source["streams"];
	        this.gaps = source["gaps"];
	        this.packetsPerSec = source["packetsPerSec"];
	        this.bytesPerSec = source["bytesPerSec"];
	        this.trafficSeen = source["trafficSeen"];
	    }
	}

}

