export namespace main {
	
	export class LauncherStatus {
	    state: string;
	    message: string;
	    account: string;
	    manifestUrl: string;
	    gameDirectory: string;
	    version: string;
	    progress: number;
	    requiredFile: number;
	    deleteFile: number;
	    downloadByte: number;
	    isAuthenticated: boolean;
	    isAutoPlayRequested: boolean;
	    isPatchEnabled: boolean;
	    isCinematicSkipped: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LauncherStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.message = source["message"];
	        this.account = source["account"];
	        this.manifestUrl = source["manifestUrl"];
	        this.gameDirectory = source["gameDirectory"];
	        this.version = source["version"];
	        this.progress = source["progress"];
	        this.requiredFile = source["requiredFile"];
	        this.deleteFile = source["deleteFile"];
	        this.downloadByte = source["downloadByte"];
	        this.isAuthenticated = source["isAuthenticated"];
	        this.isAutoPlayRequested = source["isAutoPlayRequested"];
	        this.isPatchEnabled = source["isPatchEnabled"];
	        this.isCinematicSkipped = source["isCinematicSkipped"];
	    }
	}

}

